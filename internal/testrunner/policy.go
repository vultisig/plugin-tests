package testrunner

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	vaultType "github.com/vultisig/commondata/go/vultisig/vault/v1"
	recipetypes "github.com/vultisig/recipes/types"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/vultisig/plugin-tests/internal/testrunner/mpc"
)

const ethDerivePath = "m/44/60/0/0/0"

type SuggestResult struct {
	Recipe   string
	Resource string
}

func RunPolicyCRUD(cfg InstallConfig, reshareResult *ReshareResult, logger *logrus.Logger) error {
	vault, err := parseVaultFromB64(cfg.Fixture.Vault.VaultB64, cfg.EncryptionSecret)
	if err != nil {
		return fmt.Errorf("failed to parse user vault: %w", err)
	}
	serverVault, err := parseVaultFromB64(cfg.Fixture.Vault.ServerVaultB64, cfg.EncryptionSecret)
	if err != nil {
		return fmt.Errorf("failed to parse server vault: %w", err)
	}

	client := NewTestClient(cfg.VerifierURL)

	suggestResult, err := buildRecipeFromSuggest(client, cfg.PluginID, cfg.TestTargetAddress, logger)
	if err != nil {
		return fmt.Errorf("failed to build recipe from suggest: %w", err)
	}
	logger.WithField("resource", suggestResult.Resource).Info("built recipe from suggest endpoint")

	policyVersion := 1
	pluginVersion := "1.0.0"
	policyMsg := buildPolicyMessage(suggestResult.Recipe, vault.PublicKeyEcdsa, policyVersion, pluginVersion)

	signature, err := signPolicyMessage(vault, serverVault, policyMsg, logger)
	if err != nil {
		return fmt.Errorf("failed to sign policy message: %w", err)
	}
	logger.Info("policy message signed locally")

	policyID := uuid.New().String()

	err = createPolicy(client, cfg.JWTToken, policyID, vault.PublicKeyEcdsa, cfg.PluginID, pluginVersion, policyVersion, signature, suggestResult.Recipe)
	if err != nil {
		return fmt.Errorf("policy create failed: %w", err)
	}
	logger.WithField("policy_id", policyID).Info("policy created")

	err = getPolicy(client, cfg.JWTToken, policyID)
	if err != nil {
		return fmt.Errorf("policy read failed: %w", err)
	}
	logger.Info("policy read verified")

	deleteMsg := buildPolicyMessage(suggestResult.Recipe, vault.PublicKeyEcdsa, policyVersion, pluginVersion)
	deleteSig, err := signPolicyMessage(vault, serverVault, deleteMsg, logger)
	if err != nil {
		return fmt.Errorf("failed to sign delete message: %w", err)
	}

	err = deletePolicy(client, cfg.JWTToken, policyID, deleteSig)
	if err != nil {
		return fmt.Errorf("policy delete failed: %w", err)
	}
	logger.Info("policy deleted")

	return nil
}

func buildRecipeFromSuggest(client *TestClient, pluginID, targetAddr string, logger *logrus.Logger) (*SuggestResult, error) {
	resp, err := client.GET("/plugins/" + pluginID + "/recipe-specification")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch recipe spec: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("recipe spec returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read spec response: %w", err)
	}

	var specResp struct {
		Data struct {
			PluginID             string                   `json:"plugin_id"`
			ConfigurationExample []map[string]interface{} `json:"configuration_example"`
		} `json:"data"`
	}
	err = json.Unmarshal(body, &specResp)
	if err != nil {
		return nil, fmt.Errorf("failed to parse spec response: %w", err)
	}

	if len(specResp.Data.ConfigurationExample) == 0 {
		return nil, fmt.Errorf("plugin %s has no configuration examples", pluginID)
	}

	config := specResp.Data.ConfigurationExample[0]
	config = fillEmptyAddresses(config, targetAddr).(map[string]interface{})
	logger.WithField("config", config).Info("suggest config prepared")

	suggestBody := map[string]interface{}{
		"configuration": config,
	}
	suggestResp, err := client.POST("/plugins/"+pluginID+"/recipe-specification/suggest", suggestBody)
	if err != nil {
		return nil, fmt.Errorf("suggest request failed: %w", err)
	}
	defer suggestResp.Body.Close()

	if suggestResp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(suggestResp.Body)
		return nil, fmt.Errorf("suggest returned HTTP %d: %s", suggestResp.StatusCode, string(errBody))
	}

	suggestRaw, err := io.ReadAll(suggestResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read suggest response: %w", err)
	}
	logger.WithField("response", string(suggestRaw)).Info("suggest response received")

	var suggestEnvelope struct {
		Data json.RawMessage `json:"data"`
	}
	err = json.Unmarshal(suggestRaw, &suggestEnvelope)
	if err != nil {
		return nil, fmt.Errorf("failed to parse suggest envelope: %w", err)
	}

	var policySuggest recipetypes.PolicySuggest
	unmarshaler := protojson.UnmarshalOptions{DiscardUnknown: true}
	err = unmarshaler.Unmarshal(suggestEnvelope.Data, &policySuggest)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal PolicySuggest: %w", err)
	}

	for i, rule := range policySuggest.Rules {
		if rule.Id == "" {
			rule.Id = fmt.Sprintf("rule-%d", i+1)
		}
	}

	configStruct, err := structpb.NewStruct(config)
	if err != nil {
		return nil, fmt.Errorf("failed to build configuration struct: %w", err)
	}

	policy := &recipetypes.Policy{
		Id:              pluginID,
		Name:            "Integration Test Policy",
		Version:         1,
		Author:          "plugin-tests",
		Rules:           policySuggest.Rules,
		RateLimitWindow: policySuggest.RateLimitWindow,
		MaxTxsPerWindow: policySuggest.MaxTxsPerWindow,
		Configuration:   configStruct,
	}

	data, err := proto.Marshal(policy)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal policy: %w", err)
	}
	recipe := base64.StdEncoding.EncodeToString(data)

	result := &SuggestResult{
		Recipe: recipe,
	}

	if len(policySuggest.Rules) > 0 {
		result.Resource = policySuggest.Rules[0].Resource
	}

	return result, nil
}

func fillEmptyAddresses(v interface{}, addr string) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{}, len(val))
		for k, inner := range val {
			result[k] = fillEmptyAddresses(inner, addr)
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(val))
		for i, inner := range val {
			result[i] = fillEmptyAddresses(inner, addr)
		}
		return result
	case string:
		if val == "" {
			return addr
		}
		return val
	default:
		return val
	}
}

func buildPolicyMessage(recipe, publicKey string, policyVersion int, pluginVersion string) []byte {
	fields := []string{
		recipe,
		publicKey,
		strconv.Itoa(policyVersion),
		pluginVersion,
	}
	return []byte(strings.Join(fields, "*#*"))
}

func personalSignHash(message []byte) []byte {
	prefixed := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(message), message)
	return crypto.Keccak256([]byte(prefixed))
}

func signPolicyMessage(vault, serverVault *vaultType.Vault, message []byte, logger *logrus.Logger) (string, error) {
	wrapper := mpc.NewWrapper(false)

	userKsHandle, err := loadKeyshareHandle(vault, vault.PublicKeyEcdsa, wrapper)
	if err != nil {
		return "", fmt.Errorf("user keyshare: %w", err)
	}
	defer wrapper.KeyshareFree(userKsHandle)

	serverKsHandle, err := loadKeyshareHandle(serverVault, serverVault.PublicKeyEcdsa, wrapper)
	if err != nil {
		return "", fmt.Errorf("server keyshare: %w", err)
	}
	defer wrapper.KeyshareFree(serverKsHandle)

	hash := personalSignHash(message)

	sig, err := localSign(
		userKsHandle, serverKsHandle,
		vault.LocalPartyId, serverVault.LocalPartyId,
		hash, []byte(ethDerivePath),
		wrapper,
		logger,
	)
	if err != nil {
		return "", fmt.Errorf("local sign failed: %w", err)
	}

	if len(sig) == 65 && sig[64] < 27 {
		sig[64] += 27
	}

	logger.Info("policy signature produced")

	return "0x" + hex.EncodeToString(sig), nil
}

func localSign(
	userKsHandle, serverKsHandle mpc.Handle,
	userPartyID, serverPartyID string,
	messageHash []byte,
	chainPath []byte,
	wrapper *mpc.Wrapper,
	logger *logrus.Logger,
) ([]byte, error) {
	keyID, err := wrapper.KeyshareKeyID(userKsHandle)
	if err != nil {
		return nil, fmt.Errorf("failed to get key ID: %w", err)
	}

	ids := []byte(userPartyID + "\x00" + serverPartyID)

	setupMsg, err := wrapper.SignSetupMsgNew(keyID, chainPath, messageHash, ids)
	if err != nil {
		return nil, fmt.Errorf("SignSetupMsgNew failed: %w", err)
	}

	userSession, err := wrapper.SignSessionFromSetup(setupMsg, []byte(userPartyID), userKsHandle)
	if err != nil {
		return nil, fmt.Errorf("user SignSessionFromSetup failed: %w", err)
	}
	defer wrapper.SignSessionFree(userSession)

	serverSession, err := wrapper.SignSessionFromSetup(setupMsg, []byte(serverPartyID), serverKsHandle)
	if err != nil {
		return nil, fmt.Errorf("server SignSessionFromSetup failed: %w", err)
	}
	defer wrapper.SignSessionFree(serverSession)

	type msgEnvelope struct {
		data     []byte
		receiver string
	}

	userToServer := make(chan msgEnvelope, 100)
	serverToUser := make(chan msgEnvelope, 100)

	sendOutbound := func(session mpc.Handle, localID string, outCh chan<- msgEnvelope) {
		for {
			out, outErr := wrapper.SignSessionOutputMessage(session)
			if outErr != nil || len(out) == 0 {
				return
			}
			for i := 0; i < 2; i++ {
				recv, recvErr := wrapper.SignSessionMessageReceiver(session, out, i)
				if recvErr != nil || recv == "" {
					break
				}
				if recv != localID {
					outCh <- msgEnvelope{data: append([]byte(nil), out...), receiver: recv}
				}
			}
		}
	}

	var userSig, serverSig []byte
	var userErr, serverErr error
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		sendOutbound(userSession, userPartyID, userToServer)

		for {
			select {
			case msg := <-serverToUser:
				finished, inputErr := wrapper.SignSessionInputMessage(userSession, msg.data)
				if inputErr != nil {
					userErr = fmt.Errorf("user input failed: %w", inputErr)
					return
				}
				sendOutbound(userSession, userPartyID, userToServer)
				if finished {
					userSig, userErr = wrapper.SignSessionFinish(userSession)
					return
				}
			case <-time.After(30 * time.Second):
				userErr = fmt.Errorf("user sign timed out")
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		sendOutbound(serverSession, serverPartyID, serverToUser)

		for {
			select {
			case msg := <-userToServer:
				finished, inputErr := wrapper.SignSessionInputMessage(serverSession, msg.data)
				if inputErr != nil {
					serverErr = fmt.Errorf("server input failed: %w", inputErr)
					return
				}
				sendOutbound(serverSession, serverPartyID, serverToUser)
				if finished {
					serverSig, serverErr = wrapper.SignSessionFinish(serverSession)
					return
				}
			case <-time.After(30 * time.Second):
				serverErr = fmt.Errorf("server sign timed out")
				return
			}
		}
	}()

	wg.Wait()

	if userErr != nil {
		return nil, fmt.Errorf("user: %w", userErr)
	}
	if serverErr != nil {
		return nil, fmt.Errorf("server: %w", serverErr)
	}

	if len(userSig) > 0 {
		return userSig, nil
	}
	return serverSig, nil
}

func createPolicy(client *TestClient, jwtToken, policyID, publicKey, pluginID, pluginVersion string, policyVersion int, signature, recipe string) error {
	body := map[string]interface{}{
		"id":             policyID,
		"public_key":     publicKey,
		"plugin_id":      pluginID,
		"plugin_version": pluginVersion,
		"policy_version": policyVersion,
		"signature":      signature,
		"recipe":         recipe,
		"active":         true,
		"billing":        []interface{}{},
	}

	resp, err := client.WithJWT(jwtToken).POST("/plugin/policy", body)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return readErrorResponse(resp)
	}
	return nil
}

func getPolicy(client *TestClient, jwtToken, policyID string) error {
	resp, err := client.WithJWT(jwtToken).GET("/plugin/policy/" + policyID)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return readErrorResponse(resp)
	}

	var apiResp struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	err = ReadJSONResponse(resp, &apiResp)
	if err != nil {
		return err
	}
	if apiResp.Data.ID != policyID {
		return fmt.Errorf("expected policy ID %s, got %s", policyID, apiResp.Data.ID)
	}
	return nil
}

func deletePolicy(client *TestClient, jwtToken, policyID, signature string) error {
	body := map[string]interface{}{
		"signature": signature,
	}

	resp, err := client.WithJWT(jwtToken).DELETE("/plugin/policy/"+policyID, body)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return readErrorResponse(resp)
	}
	return nil
}

func readErrorResponse(resp *http.Response) error {
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("HTTP %d (could not read body)", resp.StatusCode)
	}
	return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
}
