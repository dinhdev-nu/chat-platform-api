package types

import (
	"encoding/hex"
	"encoding/json"
)

// 1. Định nghĩa kiểu dữ liệu Custom
type HexID []byte

// 2. Viết hàm UnmarshalJSON để tự động chuyển Hex String -> []byte
func (h *HexID) UnmarshalJSON(data []byte) error {
	var s string
	// Parse dữ liệu JSON (đang là chuỗi "019e...") vào biến s
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	// Decode chuỗi Hex đó thành mảng bytes
	decoded, err := hex.DecodeString(s)
	if err != nil {
		return err
	}
	*h = decoded
	return nil
}

// 3. (Tùy chọn) Viết thêm hàm MarshalJSON để khi trả về nó tự biến thành Hex
func (h HexID) MarshalJSON() ([]byte, error) {
	return json.Marshal(hex.EncodeToString(h))
}
