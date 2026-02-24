package testrunner

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	PublicKey string `json:"public_key"`
	TokenID   string `json:"token_id"`
	TokenType string `json:"token_type"`
	jwt.RegisteredClaims
}

func GenerateJWT(secret, pubkey, tokenID string, expireHours int) (string, error) {
	if secret == "" || pubkey == "" {
		return "", fmt.Errorf("secret and pubkey are required")
	}

	expirationTime := time.Now().Add(time.Duration(expireHours) * time.Hour)
	claims := &Claims{
		PublicKey: pubkey,
		TokenID:   tokenID,
		TokenType: "access",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("failed to create token: %w", err)
	}

	return tokenString, nil
}
