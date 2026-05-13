package service

type RoomViewer interface {
	IsViewing(userID, convID []byte) bool
}
