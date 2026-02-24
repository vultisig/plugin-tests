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
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	keygenType "github.com/vultisig/commondata/go/vultisig/keygen/v1"
	vaultType "github.com/vultisig/commondata/go/vultisig/vault/v1"
	vsrelay "github.com/vultisig/vultiserver/relay"
	vgcommon "github.com/vultisig/vultisig-go/common"
	vgrelay "github.com/vultisig/vultisig-go/relay"
	"google.golang.org/protobuf/proto"

	"github.com/vultisig/plugin-tests/internal/testrunner/mpc"
)

const maxErrorResponseBytes = 4096

type InstallConfig struct {
	VerifierURL       string
	RelayURL          string
	JWTToken          string
	PluginID          string
	PluginAPIKey      string
	TestTargetAddress string
	Fixture           *FixtureData
	EncryptionSecret  string
}

type ReshareResult struct {
	ECDSAKeyshares map[string][]byte
	EdDSAKeyshares map[string][]byte
	ECDSAPubKey    string
	EdDSAPubKey    string
	HexChainCode   string
}

func RunInstall(cfg InstallConfig, logger *logrus.Logger) (*ReshareResult, error) {
	vault, err := parseVaultFromB64(cfg.Fixture.Vault.VaultB64, cfg.EncryptionSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to parse user vault: %w", err)
	}
	logger.WithFields(logrus.Fields{
		"local_party_id":   vault.LocalPartyId,
		"public_key_ecdsa": vault.PublicKeyEcdsa,
		"signers":          vault.Signers,
		"lib_type":         vault.LibType.String(),
	}).Info("parsed user vault")

	serverVault, err := parseVaultFromB64(cfg.Fixture.Vault.ServerVaultB64, cfg.EncryptionSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to parse server vault: %w", err)
	}
	logger.WithField("local_party_id", serverVault.LocalPartyId).Info("parsed server vault")

	if vault.LibType != keygenType.LibType_LIB_TYPE_DKLS {
		return nil, fmt.Errorf("vault lib_type is %s, expected DKLS", vault.LibType.String())
	}

	err = deleteExistingPlugin(context.Background(), cfg.VerifierURL, cfg.JWTToken, cfg.PluginID, logger)
	if err != nil {
		logger.WithError(err).Warn("failed to delete existing plugin (may not exist)")
	}

	sessionID := uuid.New().String()
	hexEncKey, err := generateHexEncryptionKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate encryption key: %w", err)
	}

	relayClient := vgrelay.NewRelayClient(cfg.RelayURL)

	err = relayClient.RegisterSessionWithRetry(sessionID, vault.LocalPartyId)
	if err != nil {
		return nil, fmt.Errorf("failed to register user party: %w", err)
	}
	err = relayClient.RegisterSessionWithRetry(sessionID, serverVault.LocalPartyId)
	if err != nil {
		return nil, fmt.Errorf("failed to register server party: %w", err)
	}
	logger.WithField("session_id", sessionID).Info("registered both old parties with relay")

	err = initiateReshare(cfg, vault, sessionID, hexEncKey)
	if err != nil {
		return nil, fmt.Errorf("failed to initiate reshare: %w", err)
	}
	logger.Info("reshare initiated on verifier")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	parties, err := waitForParties(ctx, relayClient, sessionID, 4, logger)
	if err != nil {
		return nil, fmt.Errorf("failed waiting for parties: %w", err)
	}
	logger.WithField("parties", parties).Info("all parties registered")

	err = relayClient.StartSession(sessionID, parties)
	if err != nil {
		return nil, fmt.Errorf("failed to start session: %w", err)
	}
	logger.Info("session started")

	allCommittee, oldIndices, newIndices := buildReshareCommittee(vault.Signers, parties)
	threshold := int(math.Ceil(float64(len(allCommittee)) * 2.0 / 3.0))

	logger.WithFields(logrus.Fields{
		"threshold":     threshold,
		"all_committee": allCommittee,
		"old_indices":   oldIndices,
		"new_indices":   newIndices,
	}).Info("starting MPC reshare")

	reshareResult, err := performReshare(vault, serverVault, sessionID, hexEncKey, allCommittee, threshold, oldIndices, newIndices, cfg.RelayURL, logger)
	if err != nil {
		return nil, fmt.Errorf("MPC reshare failed: %w", err)
	}

	logger.WithFields(logrus.Fields{
		"ecdsa_keyshares": len(reshareResult.ECDSAKeyshares),
		"eddsa_keyshares": len(reshareResult.EdDSAKeyshares),
	}).Info("reshare completed, keyshares saved")

	for _, partyID := range []string{vault.LocalPartyId, serverVault.LocalPartyId} {
		completeErr := relayClient.CompleteSession(sessionID, partyID)
		if completeErr != nil {
			logger.WithError(completeErr).WithField("party", partyID).Warn("failed to complete session")
		}
	}

	isCompleted, checkErr := relayClient.CheckCompletedParties(sessionID, parties)
	if checkErr != nil || !isCompleted {
		logger.WithFields(logrus.Fields{
			"is_completed": isCompleted,
			"error":        checkErr,
		}).Warn("not all parties completed")
	}

	logger.Info("waiting 30s for plugin-worker to upload vault to storage")
	time.Sleep(30 * time.Second)

	logger.Info("install completed: MPC reshare succeeded")
	return reshareResult, nil
}

func deleteExistingPlugin(ctx context.Context, verifierURL, jwtToken, pluginID string, logger *logrus.Logger) error {
	url := verifierURL + "/plugin/" + pluginID
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwtToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound {
		logger.WithField("status", resp.StatusCode).Info("deleted existing plugin installation")
		return nil
	}

	var respBody bytes.Buffer
	io.Copy(&respBody, io.LimitReader(resp.Body, maxErrorResponseBytes))
	return fmt.Errorf("delete plugin returned %d: %s", resp.StatusCode, respBody.String())
}

func buildReshareCommittee(oldSigners, relayParties []string) (allCommittee []string, oldIndices, newIndices []int) {
	seen := make(map[string]bool)
	relaySet := make(map[string]bool, len(relayParties))
	for _, p := range relayParties {
		relaySet[p] = true
	}
	oldSet := make(map[string]bool, len(oldSigners))
	for _, s := range oldSigners {
		oldSet[s] = true
	}

	for _, s := range oldSigners {
		if !seen[s] {
			allCommittee = append(allCommittee, s)
			seen[s] = true
		}
	}
	for _, p := range relayParties {
		if !seen[p] {
			allCommittee = append(allCommittee, p)
			seen[p] = true
		}
	}

	for i, party := range allCommittee {
		if oldSet[party] {
			oldIndices = append(oldIndices, i)
		}
		if relaySet[party] {
			newIndices = append(newIndices, i)
		}
	}

	return allCommittee, oldIndices, newIndices
}

func performReshare(
	vault *vaultType.Vault,
	serverVault *vaultType.Vault,
	sessionID string,
	hexEncKey string,
	allCommittee []string,
	threshold int,
	oldIndices []int,
	newIndices []int,
	relayURL string,
	logger *logrus.Logger,
) (*ReshareResult, error) {
	result := &ReshareResult{
		ECDSAKeyshares: map[string][]byte{},
		EdDSAKeyshares: map[string][]byte{},
	}

	curves := []struct {
		label     string
		publicKey string
		isEdDSA   bool
	}{
		{"ECDSA", vault.PublicKeyEcdsa, false},
		{"EdDSA", vault.PublicKeyEddsa, true},
	}

	for _, curve := range curves {
		logger.WithField("curve", curve.label).Info("starting reshare for curve")

		keyshares, err := reshareWithRetry(
			vault, serverVault, sessionID, hexEncKey, allCommittee,
			threshold, oldIndices, newIndices,
			curve.publicKey, curve.isEdDSA, curve.label,
			relayURL, logger,
		)
		if err != nil {
			return nil, fmt.Errorf("%s reshare failed: %w", curve.label, err)
		}

		if curve.isEdDSA {
			result.EdDSAKeyshares = keyshares
			result.EdDSAPubKey = curve.publicKey
		} else {
			result.ECDSAKeyshares = keyshares
			result.ECDSAPubKey = curve.publicKey
		}

		logger.WithField("curve", curve.label).Info("reshare completed for curve")
	}

	result.HexChainCode = vault.HexChainCode
	return result, nil
}

func reshareWithRetry(
	vault *vaultType.Vault,
	serverVault *vaultType.Vault,
	sessionID string,
	hexEncKey string,
	allCommittee []string,
	threshold int,
	oldIndices []int,
	newIndices []int,
	publicKey string,
	isEdDSA bool,
	curveLabel string,
	relayURL string,
	logger *logrus.Logger,
) (map[string][]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		logger.WithFields(logrus.Fields{
			"curve":   curveLabel,
			"attempt": attempt,
		}).Info("reshare attempt")

		keyshares, err := reshare(
			vault, serverVault, sessionID, hexEncKey, allCommittee,
			threshold, oldIndices, newIndices,
			publicKey, isEdDSA, relayURL, logger,
		)
		if err != nil {
			lastErr = err
			logger.WithError(err).WithField("attempt", attempt).Error("reshare attempt failed")
			continue
		}
		return keyshares, nil
	}
	return nil, fmt.Errorf("reshare failed after 3 attempts: %w", lastErr)
}

func loadKeyshareHandle(vault *vaultType.Vault, publicKey string, wrapper *mpc.Wrapper) (mpc.Handle, error) {
	ks := findKeyshare(vault, publicKey)
	if ks == "" {
		return 0, fmt.Errorf("keyshare not found for public key %s in vault %s", publicKey, vault.LocalPartyId)
	}
	ksBytes, err := base64.StdEncoding.DecodeString(ks)
	if err != nil {
		return 0, fmt.Errorf("failed to decode keyshare: %w", err)
	}
	handle, err := wrapper.KeyshareFromBytes(ksBytes)
	if err != nil {
		return 0, fmt.Errorf("failed to load keyshare: %w", err)
	}
	return handle, nil
}

func reshare(
	vault *vaultType.Vault,
	serverVault *vaultType.Vault,
	sessionID string,
	hexEncKey string,
	allCommittee []string,
	threshold int,
	oldIndices []int,
	newIndices []int,
	publicKey string,
	isEdDSA bool,
	relayURL string,
	logger *logrus.Logger,
) (map[string][]byte, error) {
	userWrapper := mpc.NewWrapper(isEdDSA)
	serverWrapper := mpc.NewWrapper(isEdDSA)

	userKsHandle, err := loadKeyshareHandle(vault, publicKey, userWrapper)
	if err != nil {
		return nil, fmt.Errorf("user keyshare: %w", err)
	}
	defer userWrapper.KeyshareFree(userKsHandle)

	serverKsHandle, err := loadKeyshareHandle(serverVault, publicKey, serverWrapper)
	if err != nil {
		return nil, fmt.Errorf("server keyshare: %w", err)
	}
	defer serverWrapper.KeyshareFree(serverKsHandle)

	setupMsg, err := userWrapper.QcSetupMsgNew(userKsHandle, threshold, allCommittee, oldIndices, newIndices)
	if err != nil {
		return nil, fmt.Errorf("QcSetupMsgNew failed: %w", err)
	}

	encrypted, err := mpc.EncryptEncodeSetupMessage(setupMsg, hexEncKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt setup message: %w", err)
	}
	setupMsgID := ""
	if isEdDSA {
		setupMsgID = "eddsa"
	}
	relayClient := vgrelay.NewRelayClient(relayURL)
	err = relayClient.UploadSetupMessage(sessionID, setupMsgID, encrypted)
	if err != nil {
		return nil, fmt.Errorf("failed to upload setup message: %w", err)
	}
	logger.Info("QC setup message uploaded")

	userPartyID := vault.LocalPartyId
	serverPartyID := serverVault.LocalPartyId

	userHandle, err := userWrapper.QcSessionFromSetup(setupMsg, userPartyID, userKsHandle)
	if err != nil {
		return nil, fmt.Errorf("QcSessionFromSetup (user) failed: %w", err)
	}

	serverHandle, err := serverWrapper.QcSessionFromSetup(setupMsg, serverPartyID, serverKsHandle)
	if err != nil {
		return nil, fmt.Errorf("QcSessionFromSetup (server) failed: %w", err)
	}

	userLog := logger.WithField("role", "user")
	serverLog := logger.WithField("role", "server")

	var wg sync.WaitGroup
	var userErr, serverErr error
	var userKsBytes, serverKsBytes []byte

	wg.Add(2)
	go func() {
		defer wg.Done()
		outErr := processQcOutbound(userHandle, sessionID, hexEncKey, allCommittee, userPartyID, isEdDSA, relayURL, userWrapper, userLog)
		if outErr != nil {
			userLog.WithError(outErr).Error("initial outbound failed")
		}
		_, _, userKsBytes, userErr = processQcInbound(
			userHandle, sessionID, hexEncKey, isEdDSA, userPartyID,
			true, allCommittee, relayURL, userWrapper, userLog,
		)
	}()

	go func() {
		defer wg.Done()
		outErr := processQcOutbound(serverHandle, sessionID, hexEncKey, allCommittee, serverPartyID, isEdDSA, relayURL, serverWrapper, serverLog)
		if outErr != nil {
			serverLog.WithError(outErr).Error("initial outbound failed")
		}
		serverIsInNew := slices.Contains(allCommittee, serverPartyID)
		_, _, serverKsBytes, serverErr = processQcInbound(
			serverHandle, sessionID, hexEncKey, isEdDSA, serverPartyID,
			serverIsInNew, allCommittee, relayURL, serverWrapper, serverLog,
		)
	}()

	wg.Wait()

	if userErr != nil {
		return nil, fmt.Errorf("user party failed: %w", userErr)
	}
	if serverErr != nil {
		return nil, fmt.Errorf("server party failed: %w", serverErr)
	}

	keyshares := map[string][]byte{}
	if userKsBytes != nil {
		keyshares[userPartyID] = userKsBytes
	}
	if serverKsBytes != nil {
		keyshares[serverPartyID] = serverKsBytes
	}

	logger.Info("both old parties completed reshare")
	return keyshares, nil
}

func processQcOutbound(
	handle mpc.Handle,
	sessionID string,
	hexEncKey string,
	parties []string,
	localPartyID string,
	isEdDSA bool,
	relayURL string,
	wrapper *mpc.Wrapper,
	logger logrus.FieldLogger,
) error {
	messenger := vsrelay.NewMessenger(relayURL, sessionID, hexEncKey, true, "")
	for {
		outbound, err := wrapper.QcSessionOutputMessage(handle)
		if err != nil {
			logger.WithError(err).Error("failed to get output message")
		}
		if len(outbound) == 0 {
			return nil
		}
		encodedOutbound := base64.StdEncoding.EncodeToString(outbound)
		for i := 0; i < len(parties); i++ {
			receiver, recvErr := wrapper.QcSessionMessageReceiver(handle, outbound, i)
			if recvErr != nil {
				logger.WithError(recvErr).Error("failed to get receiver")
			}
			if len(receiver) == 0 {
				break
			}
			logger.WithField("receiver", receiver).Debug("sending message")
			sendErr := messenger.Send(localPartyID, receiver, encodedOutbound)
			if sendErr != nil {
				logger.WithError(sendErr).Errorf("failed to send message to %s", receiver)
			}
		}
	}
}

func processQcInbound(
	handle mpc.Handle,
	sessionID string,
	hexEncKey string,
	isEdDSA bool,
	localPartyID string,
	isInNewCommittee bool,
	parties []string,
	relayURL string,
	wrapper *mpc.Wrapper,
	logger logrus.FieldLogger,
) (string, string, []byte, error) {
	var processedInitiatorMsg atomic.Bool
	processedInitiatorMsg.Store(false)
	var messageCache sync.Map
	relayClient := vgrelay.NewRelayClient(relayURL)
	start := time.Now()

	for {
		if time.Since(start) > 4*time.Minute {
			return "", "", nil, fmt.Errorf("reshare timed out after 4 minutes")
		}

		messages, err := relayClient.DownloadMessages(sessionID, localPartyID, "")
		if err != nil {
			logger.WithError(err).Debug("failed to download messages (will retry)")
			time.Sleep(100 * time.Millisecond)
			continue
		}

		for _, message := range messages {
			if message.From == localPartyID {
				continue
			}

			cacheKey := fmt.Sprintf("%s-%s-%s", sessionID, localPartyID, message.Hash)
			if _, found := messageCache.Load(cacheKey); found {
				continue
			}

			if localPartyID != parties[0] && !processedInitiatorMsg.Load() && message.From != parties[0] {
				logger.Debug("waiting for message from initiator party")
				continue
			}
			processedInitiatorMsg.Store(true)

			inboundBody, decErr := mpc.DecodeDecryptMessage(message.Body, hexEncKey)
			if decErr != nil {
				logger.WithError(decErr).Error("failed to decode message")
				continue
			}

			isFinished, inputErr := wrapper.QcSessionInputMessage(handle, inboundBody)
			if inputErr != nil {
				logger.WithError(inputErr).Error("failed to apply input message")
				continue
			}

			messageCache.Store(cacheKey, true)

			logger.WithFields(logrus.Fields{
				"hash": message.Hash,
				"from": message.From,
				"seq":  message.SequenceNo,
			}).Info("applied inbound message")

			delErr := relayClient.DeleteMessageFromServer(sessionID, localPartyID, message.Hash, "")
			if delErr != nil {
				logger.WithError(delErr).Error("failed to delete message")
			}

			time.Sleep(50 * time.Millisecond)

			outErr := processQcOutbound(handle, sessionID, hexEncKey, parties, localPartyID, isEdDSA, relayURL, wrapper, logger)
			if outErr != nil {
				logger.WithError(outErr).Error("failed to process outbound after inbound")
			}

			if isFinished {
				logger.Info("reshare finished")
				result, finishErr := wrapper.QcSessionFinish(handle)
				if finishErr != nil {
					return "", "", nil, fmt.Errorf("QcSessionFinish failed: %w", finishErr)
				}

				if !isInNewCommittee {
					logger.Info("reshare finished but not in new committee")
					return "", "", nil, nil
				}

				publicKeyBytes, pubErr := wrapper.KeysharePublicKey(result)
				if pubErr != nil {
					return "", "", nil, fmt.Errorf("failed to get public key: %w", pubErr)
				}
				encodedPublicKey := hex.EncodeToString(publicKeyBytes)

				chainCode := ""
				if !isEdDSA {
					chainCodeBytes, ccErr := wrapper.KeyshareChainCode(result)
					if ccErr != nil {
						return "", "", nil, fmt.Errorf("failed to get chain code: %w", ccErr)
					}
					chainCode = hex.EncodeToString(chainCodeBytes)
				}

				keyshareBytes, ksErr := wrapper.KeyshareToBytes(result)
				if ksErr != nil {
					return "", "", nil, fmt.Errorf("failed to serialize keyshare: %w", ksErr)
				}

				freeErr := wrapper.KeyshareFree(result)
				if freeErr != nil {
					logger.WithError(freeErr).Error("failed to free result keyshare")
				}

				return encodedPublicKey, chainCode, keyshareBytes, nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func parseVaultFromB64(vaultB64 string, encryptionSecret string) (*vaultType.Vault, error) {
	containerBytes, err := base64.StdEncoding.DecodeString(vaultB64)
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
		io.Copy(&respBody, io.LimitReader(resp.Body, maxErrorResponseBytes))
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

func findKeyshare(vault *vaultType.Vault, publicKey string) string {
	for _, ks := range vault.KeyShares {
		if ks.PublicKey == publicKey {
			return ks.Keyshare
		}
	}
	return ""
}
