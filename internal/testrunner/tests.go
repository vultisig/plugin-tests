package testrunner

import (
	"fmt"
	"net/http"

	"github.com/sirupsen/logrus"
)

type TestSuite struct {
	client    *TestClient
	pluginCli *TestClient
	fixture   *FixtureData
	plugins   []PluginConfig
	jwtToken  string
	logger    *logrus.Logger
	Passed    int
	Failed    int
	Total     int
	Errors    []string
}

func NewTestSuite(client *TestClient, pluginCli *TestClient, fixture *FixtureData, plugins []PluginConfig, jwtToken string, logger *logrus.Logger) *TestSuite {
	return &TestSuite{
		client:    client,
		pluginCli: pluginCli,
		fixture:   fixture,
		plugins:   plugins,
		jwtToken:  jwtToken,
		logger:    logger,
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
	phases := []struct {
		name     string
		fn       func() bool
		required bool
	}{
		{"health checks", s.healthChecks, true},
		{"verifier-plugin integration", s.verifierPluginTests, false},
		{"plugin canonical endpoints", s.pluginEndpointTests, false},
	}

	for _, phase := range phases {
		beforePassed := s.Passed
		beforeFailed := s.Failed

		s.logger.WithField("phase", phase.name).Info("starting phase")
		allPassed := phase.fn()

		phasePassed := s.Passed - beforePassed
		phaseFailed := s.Failed - beforeFailed
		s.logger.WithFields(logrus.Fields{
			"phase":  phase.name,
			"passed": phasePassed,
			"failed": phaseFailed,
		}).Info("phase completed")

		if phase.required && !allPassed {
			s.logger.WithField("phase", phase.name).Error("required phase failed, skipping remaining phases")
			break
		}
	}

	s.logger.WithFields(logrus.Fields{
		"total":  s.Total,
		"passed": s.Passed,
		"failed": s.Failed,
	}).Info("all phases completed")
}

func (s *TestSuite) healthChecks() bool {
	beforeFailed := s.Failed

	s.run("VerifierHealth", func() error {
		resp, err := s.client.GET("/plugins")
		if err != nil {
			return fmt.Errorf("request failed: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("expected 2xx, got %d", resp.StatusCode)
		}
		return nil
	})

	s.run("PluginHealth", func() error {
		resp, err := s.pluginCli.GET("/healthz")
		if err != nil {
			return fmt.Errorf("plugin unreachable: %w", err)
		}
		defer resp.Body.Close()
		return nil
	})

	for _, plugin := range s.plugins {
		pluginID := plugin.ID
		s.run("PluginSeeded/"+pluginID, func() error {
			resp, err := s.client.GET("/plugins/" + pluginID)
			if err != nil {
				return fmt.Errorf("request failed: %w", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("expected 200, got %d", resp.StatusCode)
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
			if apiResp.Data.ID != pluginID {
				return fmt.Errorf("expected plugin ID %s, got %s", pluginID, apiResp.Data.ID)
			}
			return nil
		})
	}

	return s.Failed == beforeFailed
}

func (s *TestSuite) verifierPluginTests() bool {
	beforeFailed := s.Failed

	for _, plugin := range s.plugins {
		pluginID := plugin.ID

		s.run(pluginID+"/GetRecipeSpecification", func() error {
			resp, err := s.client.GET("/plugins/" + pluginID + "/recipe-specification")
			if err != nil {
				return fmt.Errorf("request failed: %w", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("expected 200, got %d", resp.StatusCode)
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
			if apiResp.Data.PluginID == "" {
				return fmt.Errorf("expected non-empty plugin_id")
			}
			if apiResp.Data.PluginName == "" {
				return fmt.Errorf("expected non-empty plugin_name")
			}
			return nil
		})

		s.run(pluginID+"/GetRecipeFunctions", func() error {
			resp, err := s.client.GET("/plugins/" + pluginID + "/recipe-functions")
			if err != nil {
				return fmt.Errorf("request failed: %w", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("expected 200, got %d", resp.StatusCode)
			}
			var apiResp struct {
				Data map[string]interface{} `json:"data"`
			}
			err = ReadJSONResponse(resp, &apiResp)
			if err != nil {
				return err
			}
			if len(apiResp.Data) == 0 {
				return fmt.Errorf("expected non-empty recipe functions")
			}
			return nil
		})

		s.run(pluginID+"/Reshare", func() error {
			reqBody := map[string]interface{}{
				"session_id":         s.fixture.Reshare.SessionID,
				"hex_encryption_key": s.fixture.Reshare.HexEncryptionKey,
				"hex_chain_code":     s.fixture.Reshare.HexChainCode,
				"local_party_id":     s.fixture.Reshare.LocalPartyID,
				"old_parties":        s.fixture.Reshare.OldParties,
				"old_reshare_prefix": s.fixture.Reshare.OldResharePrefix,
				"email":              s.fixture.Reshare.Email,
				"public_key":         s.fixture.Vault.PublicKey,
				"plugin_id":          pluginID,
			}
			resp, err := s.client.WithJWT(s.jwtToken).POST("/vault/reshare", reqBody)
			if err != nil {
				return fmt.Errorf("request failed (verifier->plugin connectivity): %w", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusUnauthorized {
				return fmt.Errorf("reshare returned 401 unauthorized (JWT token_type issue?)")
			}
			return nil
		})

	}

	return s.Failed == beforeFailed
}

func (s *TestSuite) pluginEndpointTests() bool {
	beforeFailed := s.Failed

	for _, plugin := range s.plugins {
		pluginID := plugin.ID
		pubkey := s.fixture.Vault.PublicKey

		s.run(pluginID+"/PluginVaultExist", func() error {
			resp, err := s.pluginCli.GET("/vault/exist/" + pluginID + "/" + pubkey)
			if err != nil {
				return fmt.Errorf("plugin unreachable: %w", err)
			}
			defer resp.Body.Close()
			return nil
		})

		s.run(pluginID+"/PluginVaultGet", func() error {
			resp, err := s.pluginCli.GET("/vault/get/" + pluginID + "/" + pubkey)
			if err != nil {
				return fmt.Errorf("plugin unreachable: %w", err)
			}
			defer resp.Body.Close()
			return nil
		})

		s.run(pluginID+"/PluginSignResponse", func() error {
			resp, err := s.pluginCli.GET("/vault/sign/response/nonexistent-task")
			if err != nil {
				return fmt.Errorf("plugin unreachable: %w", err)
			}
			defer resp.Body.Close()
			return nil
		})
	}

	return s.Failed == beforeFailed
}
