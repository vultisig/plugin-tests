package testrunner

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
)

//go:embed fixture.json
var fixtureJSON []byte

type FixtureData struct {
	Vault struct {
		PublicKey string `json:"public_key"`
		Name      string `json:"name"`
		CreatedAt string `json:"created_at"`
		VaultB64  string `json:"vault_b64"`
	} `json:"vault"`
	Reshare struct {
		SessionID        string   `json:"session_id"`
		HexEncryptionKey string   `json:"hex_encryption_key"`
		HexChainCode     string   `json:"hex_chain_code"`
		LocalPartyID     string   `json:"local_party_id"`
		OldParties       []string `json:"old_parties"`
		OldResharePrefix string   `json:"old_reshare_prefix"`
		Email            string   `json:"email"`
	} `json:"reshare"`
}

type PluginConfig struct {
	ID             string
	Title          string
	Description    string
	ServerEndpoint string
	Category       string
	Audited        bool
}

func LoadFixture() (*FixtureData, error) {
	var fixture FixtureData
	err := json.Unmarshal(fixtureJSON, &fixture)
	if err != nil {
		return nil, fmt.Errorf("failed to parse embedded fixture JSON: %w", err)
	}
	return &fixture, nil
}

func GetTestPlugins() []PluginConfig {
	pluginEndpoint := os.Getenv("PLUGIN_ENDPOINT")
	if pluginEndpoint == "" {
		pluginEndpoint = "http://localhost:8082"
	}

	return []PluginConfig{
		{
			ID:             "vultisig-dca-0000",
			Title:          "DCA (Dollar Cost Averaging)",
			Description:    "Automated recurring swaps and transfers",
			ServerEndpoint: pluginEndpoint,
			Category:       "app",
		},
	}
}
