package websocket

import (
	"encoding/hex"
	"sync"
)

type RoomManager struct {
	mu sync.RWMutex
	// viewing[userHex][convHex][connID] = active viewing session
	viewing map[string]map[string]map[string]struct{}
}

func NewRoomManager() *RoomManager {
	return &RoomManager{viewing: make(map[string]map[string]map[string]struct{})}
}

func (rm *RoomManager) IsViewing(userID, convID []byte) bool {
	uidHex := hex.EncodeToString(userID)
	cidHex := hex.EncodeToString(convID)

	rm.mu.RLock()
	defer rm.mu.RUnlock()
	convs, ok := rm.viewing[uidHex]
	if !ok {
		return false
	}
	sessions, viewing := convs[cidHex]
	return viewing && len(sessions) > 0
}

func (rm *RoomManager) SetViewing(convID, userID []byte, connID string) {
	cidHex := hex.EncodeToString(convID)
	uidHex := hex.EncodeToString(userID)

	rm.mu.Lock()
	defer rm.mu.Unlock()

	if _, ok := rm.viewing[uidHex]; !ok {
		rm.viewing[uidHex] = make(map[string]map[string]struct{})
	}
	if _, ok := rm.viewing[uidHex][cidHex]; !ok {
		rm.viewing[uidHex][cidHex] = make(map[string]struct{})
	}
	rm.viewing[uidHex][cidHex][connID] = struct{}{}
}

func (rm *RoomManager) ClearViewing(convID, userID []byte, connID string) {
	cidHex := hex.EncodeToString(convID)
	uidHex := hex.EncodeToString(userID)

	rm.mu.Lock()
	defer rm.mu.Unlock()

	if convs, ok := rm.viewing[uidHex]; ok {
		if sessions, ok2 := convs[cidHex]; ok2 {
			delete(sessions, connID)
			if len(sessions) == 0 {
				delete(convs, cidHex)
			}
		}
		if len(convs) == 0 {
			delete(rm.viewing, uidHex)
		}
	}
}

func (rm *RoomManager) ClearSession(userID []byte, connID string) {
	uidHex := hex.EncodeToString(userID)

	rm.mu.Lock()
	defer rm.mu.Unlock()

	convs, ok := rm.viewing[uidHex]
	if !ok {
		return
	}
	for cidHex, sessions := range convs {
		delete(sessions, connID)
		if len(sessions) == 0 {
			delete(convs, cidHex)
		}
	}
	if len(convs) == 0 {
		delete(rm.viewing, uidHex)
	}
}

func (rm *RoomManager) ClearAll(userID []byte) {
	uidHex := hex.EncodeToString(userID)
	rm.mu.Lock()
	defer rm.mu.Unlock()
	delete(rm.viewing, uidHex)
}
