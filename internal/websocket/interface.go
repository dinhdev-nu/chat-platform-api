package websocket

type RoomViewer interface {
	IsViewing(userID, convID []byte) bool
}
