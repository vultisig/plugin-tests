package testrunner

import (
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateJWT_HappyPath(t *testing.T) {
	secret := "test-secret"
	pubkey := "03abc123"
	tokenID := "token-1"

	tokenStr, err := GenerateJWT(secret, pubkey, tokenID, 24)
	require.NoError(t, err)
	assert.NotEmpty(t, tokenStr)

	claims := &Claims{}
	parsed, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	})
	require.NoError(t, err)
	assert.True(t, parsed.Valid)
	assert.Equal(t, pubkey, claims.PublicKey)
	assert.Equal(t, tokenID, claims.TokenID)
}

func TestGenerateJWT_EmptySecret(t *testing.T) {
	_, err := GenerateJWT("", "pubkey", "token", 24)
	assert.Error(t, err)
}

func TestGenerateJWT_EmptyPubkey(t *testing.T) {
	_, err := GenerateJWT("secret", "", "token", 24)
	assert.Error(t, err)
}
