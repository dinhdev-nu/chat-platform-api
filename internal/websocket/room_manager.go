package websocket

import (
	"encoding/hex"
	"sync"
)

type RoomManager struct {
	mu sync.RWMutex
	// viewing[userHex][convHex] = count of sessions viewing
	viewing map[string]map[string]int
}

func NewRoomManager() *RoomManager {
	return &RoomManager{viewing: make(map[string]map[string]int)}
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
	cnt, viewing := convs[cidHex]
	return viewing && cnt > 0
}

func (rm *RoomManager) SetViewing(convID, userID []byte) {
	cidHex := hex.EncodeToString(convID)
	uidHex := hex.EncodeToString(userID)

	rm.mu.Lock()
	defer rm.mu.Unlock()

	if _, ok := rm.viewing[uidHex]; !ok {
		rm.viewing[uidHex] = make(map[string]int)
	}
	rm.viewing[uidHex][cidHex]++
}

func (rm *RoomManager) ClearViewing(convID, userID []byte) {
	cidHex := hex.EncodeToString(convID)
	uidHex := hex.EncodeToString(userID)

	rm.mu.Lock()
	defer rm.mu.Unlock()

	if convs, ok := rm.viewing[uidHex]; ok {
		if cnt, ok2 := convs[cidHex]; ok2 {
			if cnt <= 1 {
				delete(convs, cidHex)
			} else {
				convs[cidHex] = cnt - 1
			}
		}
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
