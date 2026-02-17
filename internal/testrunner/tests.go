package testrunner

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

type TestSuite struct {
	client     *TestClient
	fixture    *FixtureData
	plugins    []PluginConfig
	jwtToken   string
	evmFixture *EVMFixture
	logger     *logrus.Logger
	Passed     int
	Failed     int
	Total      int
	Errors     []string
}

func NewTestSuite(client *TestClient, fixture *FixtureData, plugins []PluginConfig, jwtToken string, evmFixture *EVMFixture, logger *logrus.Logger) *TestSuite {
	return &TestSuite{
		client:     client,
		fixture:    fixture,
		plugins:    plugins,
		jwtToken:   jwtToken,
		evmFixture: evmFixture,
		logger:     logger,
	}
}

func (s *TestSuite) run(name string, fn func() error) {
	s.Total++
	log := s.logger.WithFields(logrus.Fields{
		"test":  name,
		"index": s.Total,
	})
	log.Info("running")

	err := fn()
	if err != nil {
		s.Failed++
		errStr := fmt.Sprintf("%s: %s", name, err)
		s.Errors = append(s.Errors, errStr)
		log.WithError(err).Error("FAIL")
	} else {
		s.Passed++
		log.Info("PASS")
	}
}

func (s *TestSuite) RunAll() {
	sections := []struct {
		name string
		fn   func()
	}{
		{"plugin endpoints", s.testPluginEndpoints},
		{"vault endpoints", s.testVaultEndpoints},
		{"policy endpoints", s.testPolicyEndpoints},
		{"signer endpoints", s.testSignerEndpoints},
	}

	for _, section := range sections {
		beforePassed := s.Passed
		beforeFailed := s.Failed

		s.logger.WithField("section", section.name).Info("starting section")
		section.fn()

		sectionPassed := s.Passed - beforePassed
		sectionFailed := s.Failed - beforeFailed
		s.logger.WithFields(logrus.Fields{
			"section": section.name,
			"passed":  sectionPassed,
			"failed":  sectionFailed,
		}).Info("section completed")
	}

	s.logger.WithFields(logrus.Fields{
		"total":  s.Total,
		"passed": s.Passed,
		"failed": s.Failed,
	}).Info("all sections completed")
}

func (s *TestSuite) testPluginEndpoints() {
	for _, plugin := range s.plugins {
		pluginID := plugin.ID

		s.run(pluginID+"/GetPluginDetails", func() error {
			resp, err := s.client.GET("/plugins/" + pluginID)
			if err != nil {
				return fmt.Errorf("request failed: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("expected status 200, got %d", resp.StatusCode)
			}

			var apiResp struct {
				Data struct {
					ID    string `json:"id"`
					Title string `json:"title"`
				} `json:"data"`
			}
			err = ReadJSONResponse(resp, &apiResp)
			if err != nil {
				return err
			}
			if apiResp.Data.ID != pluginID {
				return fmt.Errorf("expected plugin ID %s, got %s", pluginID, apiResp.Data.ID)
			}
			if apiResp.Data.Title == "" {
				return fmt.Errorf("expected non-empty title")
			}
			return nil
		})

		s.run(pluginID+"/GetRecipeSpecification", func() error {
			resp, err := s.client.GET("/plugins/" + pluginID + "/recipe-specification")
			if err != nil {
				return fmt.Errorf("request failed: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("expected status 200, got %d", resp.StatusCode)
			}

			var apiResp struct {
				Data struct {
					PluginID   string `json:"plugin_id"`
					PluginName string `json:"plugin_name"`
				} `json:"data"`
			}
			err = ReadJSONResponse(resp, &apiResp)
			if err != nil {
				return err
			}
			if apiResp.Data.PluginID != pluginID {
				return fmt.Errorf("expected plugin_id %s, got %s", pluginID, apiResp.Data.PluginID)
			}
			if apiResp.Data.PluginName == "" {
				return fmt.Errorf("expected non-empty plugin_name")
			}
			return nil
		})
	}
}

func (s *TestSuite) testVaultEndpoints() {
	time.Sleep(2 * time.Second)

	for _, plugin := range s.plugins {
		pluginID := plugin.ID
		pubkey := s.fixture.Vault.PublicKey

		time.Sleep(500 * time.Millisecond)

		s.run(pluginID+"/VaultExists", func() error {
			resp, err := s.client.WithJWT(s.jwtToken).GET("/vault/exist/" + pluginID + "/" + pubkey)
			if err != nil {
				return fmt.Errorf("request failed: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("expected status 200, got %d", resp.StatusCode)
			}

			var apiResp struct {
				Data string `json:"data"`
			}
			err = ReadJSONResponse(resp, &apiResp)
			if err != nil {
				return err
			}
			if apiResp.Data != "ok" {
				return fmt.Errorf("expected data 'ok', got '%s'", apiResp.Data)
			}
			return nil
		})

		s.run(pluginID+"/GetVault_HappyPath", func() error {
			time.Sleep(500 * time.Millisecond)
			resp, err := s.client.WithJWT(s.jwtToken).GET("/vault/get/" + pluginID + "/" + pubkey)
			if err != nil {
				return fmt.Errorf("request failed: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("expected status 200, got %d", resp.StatusCode)
			}
			return nil
		})
	}
}

func (s *TestSuite) testPolicyEndpoints() {
	for i, plugin := range s.plugins {
		pluginID := plugin.ID
		policyID := fmt.Sprintf("00000000-0000-0000-0000-0000000000%02d", i+11)

		s.run(pluginID+"/GetPolicy_HappyPath", func() error {
			resp, err := s.client.WithJWT(s.jwtToken).GET("/plugin/policy/" + policyID)
			if err != nil {
				return fmt.Errorf("request failed: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("expected status 200, got %d", resp.StatusCode)
			}

			var apiResp struct {
				Data struct {
					ID       string `json:"id"`
					PluginID string `json:"plugin_id"`
					Active   bool   `json:"active"`
				} `json:"data"`
			}
			err = ReadJSONResponse(resp, &apiResp)
			if err != nil {
				return err
			}
			if apiResp.Data.ID != policyID {
				return fmt.Errorf("expected policy ID %s, got %s", policyID, apiResp.Data.ID)
			}
			if apiResp.Data.PluginID != pluginID {
				return fmt.Errorf("expected plugin ID %s, got %s", pluginID, apiResp.Data.PluginID)
			}
			if !apiResp.Data.Active {
				return fmt.Errorf("expected policy to be active")
			}
			return nil
		})

		s.run(pluginID+"/GetAllPolicies_HappyPath", func() error {
			resp, err := s.client.WithJWT(s.jwtToken).GET("/plugin/policies/" + pluginID)
			if err != nil {
				return fmt.Errorf("request failed: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("expected status 200, got %d", resp.StatusCode)
			}
			return nil
		})

		s.run(pluginID+"/CreatePolicy_InvalidSignature", func() error {
			reqBody := map[string]interface{}{
				"id":             "00000000-0000-0000-0000-000000000001",
				"public_key":     s.fixture.Vault.PublicKey,
				"plugin_id":      pluginID,
				"plugin_version": "1.0.0",
				"policy_version": 1,
				"signature":      "0x" + strings.Repeat("0", 130),
				"recipe":         "CgA=",
				"billing":        []interface{}{},
				"active":         true,
			}

			resp, err := s.client.WithJWT(s.jwtToken).POST("/plugin/policy", reqBody)
			if err != nil {
				return fmt.Errorf("request failed: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusBadRequest {
				return fmt.Errorf("expected status 400, got %d", resp.StatusCode)
			}
			return nil
		})

		s.run(pluginID+"/CreatePolicy_NoAuth", func() error {
			reqBody := map[string]interface{}{
				"id":             "00000000-0000-0000-0000-000000000001",
				"public_key":     s.fixture.Vault.PublicKey,
				"plugin_id":      pluginID,
				"plugin_version": "1.0.0",
				"policy_version": 1,
				"signature":      "0x" + strings.Repeat("0", 130),
				"recipe":         "CgA=",
				"billing":        []interface{}{},
				"active":         true,
			}

			resp, err := s.client.POST("/plugin/policy", reqBody)
			if err != nil {
				return fmt.Errorf("request failed: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusUnauthorized {
				return fmt.Errorf("expected status 401, got %d", resp.StatusCode)
			}
			return nil
		})

		s.run(pluginID+"/GetPolicy_NoAuth", func() error {
			resp, err := s.client.GET("/plugin/policy/test-id")
			if err != nil {
				return fmt.Errorf("request failed: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusUnauthorized {
				return fmt.Errorf("expected status 401, got %d", resp.StatusCode)
			}
			return nil
		})

		s.run(pluginID+"/GetPolicy_InvalidID", func() error {
			resp, err := s.client.WithJWT(s.jwtToken).GET("/plugin/policy/test-id")
			if err != nil {
				return fmt.Errorf("request failed: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusBadRequest {
				return fmt.Errorf("expected status 400, got %d", resp.StatusCode)
			}
			return nil
		})
	}
}

func (s *TestSuite) testSignerEndpoints() {
	for i, plugin := range s.plugins {
		pluginID := plugin.ID
		apiKey := fmt.Sprintf("integration-test-apikey-%s", pluginID)
		policyID := fmt.Sprintf("00000000-0000-0000-0000-0000000000%02d", i+11)

		if i > 0 {
			time.Sleep(2 * time.Second)
		}

		s.run(pluginID+"/Sign_NoAPIKey", func() error {
			reqBody := map[string]interface{}{
				"plugin_id":  pluginID,
				"public_key": s.fixture.Vault.PublicKey,
				"policy_id":  policyID,
				"messages":   []interface{}{},
			}

			resp, err := s.client.POST("/plugin-signer/sign", reqBody)
			if err != nil {
				return fmt.Errorf("request failed: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusUnauthorized {
				return fmt.Errorf("expected status 401, got %d", resp.StatusCode)
			}
			return nil
		})

		s.run(pluginID+"/Sign_InvalidAPIKey", func() error {
			reqBody := map[string]interface{}{
				"plugin_id":  pluginID,
				"public_key": s.fixture.Vault.PublicKey,
				"policy_id":  policyID,
				"messages":   []interface{}{},
			}

			resp, err := s.client.WithAPIKey("invalid-api-key").POST("/plugin-signer/sign", reqBody)
			if err != nil {
				return fmt.Errorf("request failed: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusUnauthorized {
				return fmt.Errorf("expected status 401, got %d", resp.StatusCode)
			}
			return nil
		})

		s.run(pluginID+"/Sign_EmptyMessages", func() error {
			reqBody := map[string]interface{}{
				"plugin_id":  pluginID,
				"public_key": s.fixture.Vault.PublicKey,
				"policy_id":  policyID,
				"messages":   []interface{}{},
			}

			resp, err := s.client.WithAPIKey(apiKey).POST("/plugin-signer/sign", reqBody)
			if err != nil {
				return fmt.Errorf("request failed: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusBadRequest {
				return fmt.Errorf("expected status 400, got %d", resp.StatusCode)
			}
			return nil
		})

		s.run(pluginID+"/Sign_ValidRequest", func() error {
			reqBody := map[string]interface{}{
				"plugin_id":        pluginID,
				"public_key":       s.fixture.Vault.PublicKey,
				"policy_id":        policyID,
				"transactions":     s.evmFixture.TxB64,
				"transaction_type": "evm",
				"messages": []map[string]interface{}{
					{
						"message":       s.evmFixture.MsgB64,
						"chain":         "Ethereum",
						"hash":          s.evmFixture.MsgSHA256B64,
						"hash_function": "SHA256",
					},
				},
			}

			resp, err := s.client.WithAPIKey(apiKey).POST("/plugin-signer/sign", reqBody)
			if err != nil {
				return fmt.Errorf("request failed: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("expected status 200, got %d", resp.StatusCode)
			}

			var apiResp struct {
				Data struct {
					TaskIDs []string `json:"task_ids"`
				} `json:"data"`
			}
			err = ReadJSONResponse(resp, &apiResp)
			if err != nil {
				return err
			}

			if len(apiResp.Data.TaskIDs) != 1 {
				return fmt.Errorf("expected 1 task_id, got %d", len(apiResp.Data.TaskIDs))
			}

			taskID := apiResp.Data.TaskIDs[0]
			return s.verifySignResponse(apiKey, taskID)
		})
	}
}

func (s *TestSuite) verifySignResponse(apiKey, taskID string) error {
	resp, err := s.client.GET("/plugin-signer/sign/response/" + taskID)
	if err != nil {
		return fmt.Errorf("GetSignResponse_NoAPIKey request failed: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		return fmt.Errorf("GetSignResponse_NoAPIKey: expected status 401, got %d", resp.StatusCode)
	}

	resp, err = s.client.WithAPIKey(apiKey).GET("/plugin-signer/sign/response/" + taskID)
	if err != nil {
		return fmt.Errorf("GetSignResponse_WithAPIKey request failed: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode < 200 {
		return fmt.Errorf("GetSignResponse_WithAPIKey: expected status >= 200, got %d", resp.StatusCode)
	}

	return nil
}
