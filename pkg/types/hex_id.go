package types

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
)

type HexID []byte

func (h *HexID) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if s == "" {
		return fmt.Errorf("hex id is required")
	}

	decoded, err := hex.DecodeString(s)
	if err != nil {
		return err
	}
	if len(decoded) != 16 {
		return fmt.Errorf("hex id must decode to 16 bytes")
	}

	*h = decoded
	return nil
}

func (h HexID) MarshalJSON() ([]byte, error) {
	return json.Marshal(hex.EncodeToString(h))
}
