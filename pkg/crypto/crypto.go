package crypto

import (
	"crypto/rand"
	"fmt"
	"math/big"

	"github.com/google/uuid"
)

func GenerateOTP() (string, error) {
	max := big.NewInt(1_000_000)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%.06d", n.Int64()), nil
}

func NewUUIDv7Bytes() ([]byte, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	return id[:], nil
}

func ParseUUIDToBytes(s string) ([]byte, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return nil, err
	}
	return id[:], nil
}

func ParseBytesToUUID(b []byte) (string, error) {
	if len(b) != 16 {
		return "", fmt.Errorf("invalid binary uuid length: %d", len(b))
	}
	var id uuid.UUID
	copy(id[:], b)
	return id.String(), nil
}

func ParseHexToBytes(s string) ([]byte, error) {
	b, err := uuid.Parse(s)
	if err != nil {
		return nil, err
	}
	return b[:], nil
}
