package jwt

import (
	"encoding/hex"
	"fmt"
	"time"

	"github.com/dinhdev-nu/chat-platform-api/config"
	"github.com/dinhdev-nu/chat-platform-api/pkg/crypto"

	gojwt "github.com/golang-jwt/jwt/v5"
)

type JWTManager struct {
	cfg *config.JwtConfig
}

func NewJWTManager(cfg *config.JwtConfig) *JWTManager {
	return &JWTManager{cfg: cfg}
}

type Claims struct {
	gojwt.RegisteredClaims
	UserID   string `json:"uid"`
	DiviceID string `json:"did"`
}

type GenerateTokenParams struct {
	UserID   []byte
	DiviceID []byte
	JTI      []byte
}

type GenerateTokenResult struct {
	Token     string
	JTI       []byte
	ExpiresAT time.Time
}

func (m *JWTManager) GenerateToken(params GenerateTokenParams) (*GenerateTokenResult, error) {
	expiresAt := time.Now().Add(time.Duration(m.cfg.ExpireTime) * time.Second)

	jti, err := crypto.ParseBytesToUUID(params.JTI)
	if err != nil {
		return nil, err
	}

	claims := Claims{
		RegisteredClaims: gojwt.RegisteredClaims{
			ID:        jti,
			IssuedAt:  gojwt.NewNumericDate(time.Now()),
			ExpiresAt: gojwt.NewNumericDate(expiresAt),
		},
		UserID:   hex.EncodeToString(params.UserID),
		DiviceID: hex.EncodeToString(params.DiviceID),
	}

	token := gojwt.NewWithClaims(gojwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(m.cfg.Secret))
	if err != nil {
		return nil, err
	}

	return &GenerateTokenResult{
		Token:     signed,
		JTI:       params.JTI,
		ExpiresAT: expiresAt,
	}, nil
}

func (m *JWTManager) ParseToken(tokenStr string) (*Claims, error) {
	token, err := gojwt.ParseWithClaims(
		tokenStr, Claims{},
		func(token *gojwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*gojwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("Unexpected signing method: %v", token.Header["alg"])
			}
			return []byte(m.cfg.Secret), nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("Jwt parse %w:", err)
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return claims, fmt.Errorf("jwt invalid claims")
	}
	return claims, nil
}

func (c *Claims) JTIBytes() ([]byte, error) {
	return crypto.ParseUUIDToBytes(c.ID)
}

func (c *Claims) UserIDBytes() ([]byte, error) {
	return hex.DecodeString(c.UserID)
}
