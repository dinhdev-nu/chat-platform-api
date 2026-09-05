package presenter

import (
	"encoding/hex"
	"time"
)

func optionalHex(value []byte) *string {
	if len(value) == 0 {
		return nil
	}
	encoded := hex.EncodeToString(value)
	return &encoded
}

func optionalTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.Format(time.RFC3339)
	return &formatted
}
