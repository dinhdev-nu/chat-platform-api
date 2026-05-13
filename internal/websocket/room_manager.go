package websocket

import (
	"encoding/hex"
	"sync"
)

type RoomManager struct {
	mu      sync.RWMutex
	viewing map[string]map[string]struct{}
}

func NewRoomManager() *RoomManager {
	return &RoomManager{viewing: make(map[string]map[string]struct{})}
}

func (rm *RoomManager) IsViewing(convID, userID []byte) bool {
	cidHex := hex.EncodeToString(convID)
	uidHex := hex.EncodeToString(userID)

	rm.mu.RLock()
	defer rm.mu.RUnlock()
	convs, ok := rm.viewing[uidHex]
	if !ok {
		return false
	}
	_, viewing := convs[cidHex]
	return viewing
}

func (rm *RoomManager) SetViewing(convID, userID []byte) {
	cidHex := hex.EncodeToString(convID)
	uidHex := hex.EncodeToString(userID)

	rm.mu.Lock()
	defer rm.mu.Unlock()

	if _, ok := rm.viewing[uidHex]; !ok {
		rm.viewing[uidHex] = make(map[string]struct{})
	}
	rm.viewing[uidHex][cidHex] = struct{}{}
}

func (rm *RoomManager) ClearViewing(convID, userID []byte) {
	cidHex := hex.EncodeToString(convID)
	uidHex := hex.EncodeToString(userID)

	rm.mu.Lock()
	defer rm.mu.Unlock()

	if convs, ok := rm.viewing[uidHex]; ok {
		delete(convs, cidHex)
		if len(convs) == 0 {
			delete(rm.viewing, uidHex)
		}
	}
}

func (rm *RoomManager) ClearAll(userID []byte) {
	uidHex := hex.EncodeToString(userID)
	rm.mu.Lock()
	defer rm.mu.Unlock()
	delete(rm.viewing, uidHex)
}
