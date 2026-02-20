package testrunner

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	vaultType "github.com/vultisig/commondata/go/vultisig/vault/v1"
	"github.com/vultisig/vultiserver/relay"
	vgcommon "github.com/vultisig/vultisig-go/common"
	vgrelay "github.com/vultisig/vultisig-go/relay"
	"google.golang.org/protobuf/proto"

	"github.com/vultisig/plugin-tests/internal/testrunner/mpc"
)

type InstallConfig struct {
	VerifierURL      string
	RelayURL         string
	JWTToken         string
	PluginID         string
	Fixture          *FixtureData
	EncryptionSecret string
}

func RunInstall(cfg InstallConfig, logger *logrus.Logger) error {
	vault, err := parseVaultFromFixture(cfg.Fixture, cfg.EncryptionSecret)
	if err != nil {
		return fmt.Errorf("failed to parse vault from fixture: %w", err)
	}

	logger.WithFields(logrus.Fields{
		"local_party_id":   vault.LocalPartyId,
		"public_key_ecdsa": vault.PublicKeyEcdsa,
		"public_key_eddsa": vault.PublicKeyEddsa,
		"signers":          vault.Signers,
	}).Info("parsed vault from fixture")

	sessionID := uuid.New().String()
	hexEncKey, err := generateHexEncryptionKey()
	if err != nil {
		return fmt.Errorf("failed to generate encryption key: %w", err)
	}

	relayClient := vgrelay.NewRelayClient(cfg.RelayURL)

	err = relayClient.RegisterSessionWithRetry(sessionID, vault.LocalPartyId)
	if err != nil {
		return fmt.Errorf("failed to register with relay: %w", err)
	}
	logger.WithField("session_id", sessionID).Info("registered with relay")

	err = initiateReshare(cfg, vault, sessionID, hexEncKey)
	if err != nil {
		return fmt.Errorf("failed to initiate reshare: %w", err)
	}
	logger.Info("reshare initiated on verifier")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	parties, err := waitForParties(ctx, relayClient, sessionID, 3, logger)
	if err != nil {
		return fmt.Errorf("failed waiting for parties: %w", err)
	}
	logger.WithField("parties", parties).Info("all parties registered")

	err = relayClient.StartSession(sessionID, parties)
	if err != nil {
		return fmt.Errorf("failed to start session: %w", err)
	}
	logger.Info("session started")

	localPartyID := vault.LocalPartyId
	ourIndex := slices.Index(parties, localPartyID)
	if ourIndex < 0 {
		return fmt.Errorf("our party %s not found in registered parties %v", localPartyID, parties)
	}
	oldParties := []int{ourIndex}
	var newParties []int
	for i := range parties {
		if i != ourIndex {
			newParties = append(newParties, i)
		}
	}
	threshold := int(math.Ceil(float64(len(parties))*2.0/3.0)) - 1

	logger.WithFields(logrus.Fields{
		"threshold":   threshold,
		"old_parties": oldParties,
		"new_parties": newParties,
		"our_index":   ourIndex,
	}).Info("starting ECDSA reshare")

	ecdsaPubkey, chainCode, err := reshareWithRetry(
		vault, sessionID, hexEncKey, parties, vault.PublicKeyEcdsa,
		false, localPartyID, threshold, oldParties, newParties,
		cfg.RelayURL, logger,
	)
	if err != nil {
		return fmt.Errorf("ECDSA reshare failed: %w", err)
	}
	logger.WithField("ecdsa_pubkey", ecdsaPubkey).Info("ECDSA reshare completed")

	logger.Info("starting EdDSA reshare")
	eddsaPubkey, _, err := reshareWithRetry(
		vault, sessionID, hexEncKey, parties, vault.PublicKeyEddsa,
		true, localPartyID, threshold, oldParties, newParties,
		cfg.RelayURL, logger,
	)
	if err != nil {
		return fmt.Errorf("EdDSA reshare failed: %w", err)
	}
	logger.WithField("eddsa_pubkey", eddsaPubkey).Info("EdDSA reshare completed")

	err = relayClient.CompleteSession(sessionID, localPartyID)
	if err != nil {
		logger.WithError(err).Warn("failed to complete session (non-fatal)")
	}

	_ = chainCode
	logger.WithFields(logrus.Fields{
		"ecdsa_pubkey": ecdsaPubkey,
		"eddsa_pubkey": eddsaPubkey,
	}).Info("install completed successfully")

	return nil
}

func parseVaultFromFixture(fixture *FixtureData, encryptionSecret string) (*vaultType.Vault, error) {
	containerBytes, err := base64.StdEncoding.DecodeString(fixture.Vault.VaultB64)
	if err != nil {
		return nil, fmt.Errorf("failed to decode vault_b64: %w", err)
	}

	var container vaultType.VaultContainer
	err = proto.Unmarshal(containerBytes, &container)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal vault container: %w", err)
	}

	var vaultBytes []byte
	if container.IsEncrypted {
		if encryptionSecret == "" {
			return nil, fmt.Errorf("vault is encrypted but no encryption secret provided")
		}
		encBytes, decErr := base64.StdEncoding.DecodeString(container.Vault)
		if decErr != nil {
			return nil, fmt.Errorf("failed to decode encrypted vault string: %w", decErr)
		}
		vaultBytes, err = vgcommon.DecryptVault(encryptionSecret, encBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt vault: %w", err)
		}
	} else {
		vaultBytes, err = base64.StdEncoding.DecodeString(container.Vault)
		if err != nil {
			return nil, fmt.Errorf("failed to decode vault string: %w", err)
		}
	}

	var vault vaultType.Vault
	err = proto.Unmarshal(vaultBytes, &vault)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal vault: %w", err)
	}

	if vault.LocalPartyId == "" {
		return nil, fmt.Errorf("vault has empty local_party_id")
	}
	if vault.PublicKeyEcdsa == "" {
		return nil, fmt.Errorf("vault has empty public_key_ecdsa")
	}

	return &vault, nil
}

func generateHexEncryptionKey() (string, error) {
	key := make([]byte, 32)
	_, err := rand.Read(key)
	if err != nil {
		return "", fmt.Errorf("failed to generate random key: %w", err)
	}
	return hex.EncodeToString(key), nil
}

type reshareRequest struct {
	Name             string   `json:"name"`
	PublicKey        string   `json:"public_key"`
	SessionID        string   `json:"session_id"`
	HexEncryptionKey string   `json:"hex_encryption_key"`
	HexChainCode     string   `json:"hex_chain_code"`
	LocalPartyId     string   `json:"local_party_id"`
	OldParties       []string `json:"old_parties"`
	Email            string   `json:"email"`
	PluginID         string   `json:"plugin_id"`
}

func initiateReshare(cfg InstallConfig, vault *vaultType.Vault, sessionID string, hexEncKey string) error {
	req := reshareRequest{
		Name:             vault.Name,
		PublicKey:        vault.PublicKeyEcdsa,
		SessionID:        sessionID,
		HexEncryptionKey: hexEncKey,
		HexChainCode:     vault.HexChainCode,
		LocalPartyId:     vault.LocalPartyId,
		OldParties:       vault.Signers,
		Email:            cfg.Fixture.Reshare.Email,
		PluginID:         cfg.PluginID,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal reshare request: %w", err)
	}

	url := cfg.VerifierURL + "/vault/reshare"
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+cfg.JWTToken)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("reshare request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var respBody bytes.Buffer
		io.Copy(&respBody, io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("reshare request returned %d: %s", resp.StatusCode, respBody.String())
	}

	return nil
}

func waitForParties(ctx context.Context, client *vgrelay.Client, sessionID string, expectedCount int, logger *logrus.Logger) ([]string, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timeout waiting for %d parties: %w", expectedCount, ctx.Err())
		default:
		}

		parties, err := client.GetSession(sessionID)
		if err != nil {
			logger.WithError(err).Debug("polling parties (will retry)")
			time.Sleep(time.Second)
			continue
		}

		if len(parties) >= expectedCount {
			return parties, nil
		}

		logger.WithField("registered", len(parties)).Debug("waiting for more parties")
		time.Sleep(time.Second)
	}
}

func reshareWithRetry(
	vault *vaultType.Vault,
	sessionID string,
	hexEncKey string,
	parties []string,
	publicKey string,
	isEdDSA bool,
	localPartyID string,
	threshold int,
	oldParties []int,
	newParties []int,
	relayURL string,
	logger *logrus.Logger,
) (string, string, error) {
	for attempt := 0; attempt < 3; attempt++ {
		pubkey, chainCode, err := reshare(
			vault, sessionID, hexEncKey, parties, publicKey,
			isEdDSA, localPartyID, threshold, oldParties, newParties,
			relayURL, logger, attempt,
		)
		if err == nil {
			return pubkey, chainCode, nil
		}
		logger.WithError(err).WithField("attempt", attempt).Error("reshare attempt failed")
	}
	return "", "", fmt.Errorf("reshare failed after 3 attempts")
}

func reshare(
	vault *vaultType.Vault,
	sessionID string,
	hexEncKey string,
	parties []string,
	publicKey string,
	isEdDSA bool,
	localPartyID string,
	threshold int,
	oldParties []int,
	newParties []int,
	relayURL string,
	logger *logrus.Logger,
	attempt int,
) (string, string, error) {
	curveLabel := "ECDSA"
	if isEdDSA {
		curveLabel = "EdDSA"
	}
	logger.WithFields(logrus.Fields{
		"curve":   curveLabel,
		"attempt": attempt,
	}).Info("reshare attempt")

	wrapper := mpc.NewWrapper(isEdDSA)

	var keyshareHandle mpc.Handle
	if publicKey != "" {
		keyshare := findKeyshare(vault, publicKey)
		if keyshare == "" {
			return "", "", fmt.Errorf("keyshare not found for public key %s", publicKey)
		}
		keyshareBytes, err := base64.StdEncoding.DecodeString(keyshare)
		if err != nil {
			return "", "", fmt.Errorf("failed to decode keyshare: %w", err)
		}
		keyshareHandle, err = wrapper.KeyshareFromBytes(keyshareBytes)
		if err != nil {
			return "", "", fmt.Errorf("failed to load keyshare: %w", err)
		}
		defer func() {
			freeErr := wrapper.KeyshareFree(keyshareHandle)
			if freeErr != nil {
				logger.WithError(freeErr).Error("failed to free keyshare handle")
			}
		}()
	}

	setupMsg, err := wrapper.QcSetupMsgNew(keyshareHandle, threshold, parties, oldParties, newParties)
	if err != nil {
		return "", "", fmt.Errorf("failed to create QC setup message: %w", err)
	}

	encrypted, err := mpc.EncryptEncodeSetupMessage(setupMsg, hexEncKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to encrypt setup message: %w", err)
	}

	relayClient := vgrelay.NewRelayClient(relayURL)
	setupMsgID := ""
	if isEdDSA {
		setupMsgID = "eddsa"
	}
	err = relayClient.UploadSetupMessage(sessionID, setupMsgID, encrypted)
	if err != nil {
		return "", "", fmt.Errorf("failed to upload setup message: %w", err)
	}
	logger.Info("setup message uploaded")

	handle, err := wrapper.QcSessionFromSetup(setupMsg, localPartyID, keyshareHandle)
	if err != nil {
		return "", "", fmt.Errorf("failed to create QC session from setup: %w", err)
	}

	err = processQcOutbound(handle, sessionID, hexEncKey, parties, localPartyID, isEdDSA, relayURL, logger)
	if err != nil {
		logger.WithError(err).Error("initial outbound processing failed")
	}

	isInNewCommittee := slices.Contains(parties, localPartyID)

	newPubkey, chainCode, err := processQcInbound(
		handle, wrapper, sessionID, hexEncKey, isEdDSA,
		localPartyID, isInNewCommittee, parties, relayURL, logger,
	)
	return newPubkey, chainCode, err
}

func findKeyshare(vault *vaultType.Vault, publicKey string) string {
	for _, ks := range vault.KeyShares {
		if ks.PublicKey == publicKey {
			return ks.Keyshare
		}
	}
	return ""
}

func processQcOutbound(
	handle mpc.Handle,
	sessionID string,
	hexEncKey string,
	parties []string,
	localPartyID string,
	isEdDSA bool,
	relayURL string,
	logger *logrus.Logger,
) error {
	messenger := relay.NewMessenger(relayURL, sessionID, hexEncKey, true, "")
	wrapper := mpc.NewWrapper(isEdDSA)
	for {
		outbound, err := wrapper.QcSessionOutputMessage(handle)
		if err != nil {
			logger.WithError(err).Error("failed to get output message")
		}
		if len(outbound) == 0 {
			return nil
		}
		encodedOutbound := base64.StdEncoding.EncodeToString(outbound)
		for i := range parties {
			receiver, recvErr := wrapper.QcSessionMessageReceiver(handle, outbound, i)
			if recvErr != nil {
				logger.WithError(recvErr).Error("failed to get message receiver")
			}
			if receiver == "" {
				break
			}
			sendErr := messenger.Send(localPartyID, receiver, encodedOutbound)
			if sendErr != nil {
				logger.WithError(sendErr).Error("failed to send message")
			}
		}
	}
}

func processQcInbound(
	handle mpc.Handle,
	wrapper *mpc.Wrapper,
	sessionID string,
	hexEncKey string,
	isEdDSA bool,
	localPartyID string,
	isInNewCommittee bool,
	parties []string,
	relayURL string,
	logger *logrus.Logger,
) (string, string, error) {
	processedFirstMsg := false
	messageCache := make(map[string]bool)
	relayClient := vgrelay.NewRelayClient(relayURL)
	start := time.Now()

	for {
		if time.Since(start) > 4*time.Minute {
			return "", "", fmt.Errorf("reshare timed out after 4 minutes")
		}

		messages, err := relayClient.DownloadMessages(sessionID, localPartyID, "")
		if err != nil {
			logger.WithError(err).Error("failed to download messages")
			time.Sleep(100 * time.Millisecond)
			continue
		}

		for _, message := range messages {
			if message.From == localPartyID {
				continue
			}

			cacheKey := fmt.Sprintf("%s-%s-%s", sessionID, localPartyID, message.Hash)
			if messageCache[cacheKey] {
				continue
			}

			if !processedFirstMsg && message.From != parties[0] {
				continue
			}
			processedFirstMsg = true

			inboundBody, decErr := mpc.DecodeDecryptMessage(message.Body, hexEncKey)
			if decErr != nil {
				logger.WithError(decErr).Error("failed to decode inbound message")
				continue
			}

			isFinished, inputErr := wrapper.QcSessionInputMessage(handle, inboundBody)
			if inputErr != nil {
				logger.WithError(inputErr).Error("failed to apply input message")
				continue
			}
			messageCache[cacheKey] = true

			logger.WithFields(logrus.Fields{
				"hash": message.Hash,
				"from": message.From,
				"seq":  message.SequenceNo,
			}).Debug("applied inbound message")

			delErr := relayClient.DeleteMessageFromServer(sessionID, localPartyID, message.Hash, "")
			if delErr != nil {
				logger.WithError(delErr).Error("failed to delete message from server")
			}

			time.Sleep(50 * time.Millisecond)

			outErr := processQcOutbound(handle, sessionID, hexEncKey, parties, localPartyID, isEdDSA, relayURL, logger)
			if outErr != nil {
				logger.WithError(outErr).Error("failed to process outbound after input")
			}

			if isFinished {
				logger.Info("reshare finished")
				result, finErr := wrapper.QcSessionFinish(handle)
				if finErr != nil {
					return "", "", fmt.Errorf("failed to finish QC session: %w", finErr)
				}
				defer func() {
					freeErr := wrapper.KeyshareFree(result)
					if freeErr != nil {
						logger.WithError(freeErr).Error("failed to free result keyshare")
					}
				}()

				if !isInNewCommittee {
					logger.Info("reshare finished but not in new committee")
					return "", "", nil
				}

				pubkeyBytes, pubErr := wrapper.KeysharePublicKey(result)
				if pubErr != nil {
					return "", "", fmt.Errorf("failed to get public key: %w", pubErr)
				}
				encodedPubkey := hex.EncodeToString(pubkeyBytes)

				chainCode := ""
				if !isEdDSA {
					chainCodeBytes, ccErr := wrapper.KeyshareChainCode(result)
					if ccErr != nil {
						return "", "", fmt.Errorf("failed to get chain code: %w", ccErr)
					}
					chainCode = hex.EncodeToString(chainCodeBytes)
				}

				return encodedPubkey, chainCode, nil
			}
		}

		time.Sleep(100 * time.Millisecond)
	}
}
