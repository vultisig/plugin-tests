package testrunner

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateEVMFixture_HappyPath(t *testing.T) {
	fixture, err := GenerateEVMFixture(1, "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb0", "", 21000, 0)
	require.NoError(t, err)

	assert.NotEmpty(t, fixture.TxB64)
	assert.NotEmpty(t, fixture.MsgB64)
	assert.NotEmpty(t, fixture.MsgSHA256B64)

	txBytes, err := base64.StdEncoding.DecodeString(fixture.TxB64)
	require.NoError(t, err)
	assert.Equal(t, byte(0x02), txBytes[0])

	msgBytes, err := base64.StdEncoding.DecodeString(fixture.MsgB64)
	require.NoError(t, err)
	assert.Len(t, msgBytes, 32)

	hashBytes, err := base64.StdEncoding.DecodeString(fixture.MsgSHA256B64)
	require.NoError(t, err)
	assert.Len(t, hashBytes, 32)
}

func TestGenerateEVMFixture_Deterministic(t *testing.T) {
	f1, err := GenerateEVMFixture(1, "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb0", "1000000", 21000, 5)
	require.NoError(t, err)

	f2, err := GenerateEVMFixture(1, "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb0", "1000000", 21000, 5)
	require.NoError(t, err)

	assert.Equal(t, f1.TxB64, f2.TxB64)
	assert.Equal(t, f1.MsgB64, f2.MsgB64)
	assert.Equal(t, f1.MsgSHA256B64, f2.MsgSHA256B64)
}

func TestGenerateEVMFixture_InvalidValue(t *testing.T) {
	_, err := GenerateEVMFixture(1, "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb0", "not-a-number", 21000, 0)
	assert.Error(t, err)
}
