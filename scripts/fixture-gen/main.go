package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	vaultType "github.com/vultisig/commondata/go/vultisig/vault/v1"
	vgcommon "github.com/vultisig/vultisig-go/common"
	"google.golang.org/protobuf/proto"
)

func main() {
	vultFile := flag.String("vult", "", "path to .vult file")
	password := flag.String("password", "Saggy@Commotion@Occupier@Registry1", "vault encryption password")
	flag.Parse()

	if *vultFile == "" {
		fmt.Fprintln(os.Stderr, "usage: fixture-gen -vult <path-to-file.vult> [-password <encryption-password>]")
		os.Exit(1)
	}

	raw, err := os.ReadFile(*vultFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read file: %v\n", err)
		os.Exit(1)
	}

	vaultB64 := strings.TrimSpace(string(raw))

	containerBytes, err := base64.StdEncoding.DecodeString(vaultB64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to base64-decode .vult content: %v\n", err)
		os.Exit(1)
	}

	var container vaultType.VaultContainer
	err = proto.Unmarshal(containerBytes, &container)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to unmarshal VaultContainer: %v\n", err)
		os.Exit(1)
	}

	var vaultBytes []byte
	if container.IsEncrypted {
		if *password == "" {
			fmt.Fprintln(os.Stderr, "vault is encrypted — provide -password flag")
			os.Exit(1)
		}
		encBytes, decErr := base64.StdEncoding.DecodeString(container.Vault)
		if decErr != nil {
			fmt.Fprintf(os.Stderr, "failed to decode encrypted vault string: %v\n", decErr)
			os.Exit(1)
		}
		vaultBytes, err = vgcommon.DecryptVault(*password, encBytes)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to decrypt vault (wrong password?): %v\n", err)
			os.Exit(1)
		}
	} else {
		vaultBytes, err = base64.StdEncoding.DecodeString(container.Vault)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to decode vault string: %v\n", err)
			os.Exit(1)
		}
	}

	var vault vaultType.Vault
	err = proto.Unmarshal(vaultBytes, &vault)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to unmarshal Vault: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Vault info:\n")
	fmt.Fprintf(os.Stderr, "  Name:             %s\n", vault.Name)
	fmt.Fprintf(os.Stderr, "  LocalPartyId:     %s\n", vault.LocalPartyId)
	fmt.Fprintf(os.Stderr, "  PublicKeyEcdsa:   %s\n", vault.PublicKeyEcdsa)
	fmt.Fprintf(os.Stderr, "  PublicKeyEddsa:   %s\n", vault.PublicKeyEddsa)
	fmt.Fprintf(os.Stderr, "  Signers:          %v\n", vault.Signers)
	fmt.Fprintf(os.Stderr, "  HexChainCode:     %s\n", vault.HexChainCode)
	fmt.Fprintf(os.Stderr, "  KeyShares:        %d\n", len(vault.KeyShares))
	for _, ks := range vault.KeyShares {
		fmt.Fprintf(os.Stderr, "    - pubkey: %s (keyshare len: %d)\n", ks.PublicKey, len(ks.Keyshare))
	}

	placeholderParties := make([]string, len(vault.Signers))
	for i := range vault.Signers {
		placeholderParties[i] = fmt.Sprintf("party%d", i+1)
	}

	fixture := map[string]interface{}{
		"vault": map[string]interface{}{
			"public_key": vault.PublicKeyEcdsa,
			"name":       "integration-test-vault",
			"created_at": time.Now().UTC().Format(time.RFC3339),
			"vault_b64":  vaultB64,
		},
		"reshare": map[string]interface{}{
			"session_id":         "00000000-0000-0000-0000-000000000000",
			"hex_encryption_key": "0000000000000000000000000000000000000000000000000000000000000000",
			"hex_chain_code":     "0000000000000000000000000000000000000000000000000000000000000000",
			"local_party_id":     "integration-test-party",
			"old_parties":        placeholderParties,
			"old_reshare_prefix": "integration-test",
			"email":              "integration@test.example.com",
		},
	}

	out, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to marshal fixture JSON: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(out))
}
