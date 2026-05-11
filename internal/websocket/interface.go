package websocket

type RoomViewer interface {
	IsViewing(userID, convID []byte) bool
}

type roomViewer struct {
}

func NewRoomViewer() RoomViewer {
	return &roomViewer{}
}

// IsViewing reports whether the given user is viewing the given conversation.
func (r *roomViewer) IsViewing(userID, convID []byte) bool {
	// TODO: implement real logic. Return false by default to satisfy interface.
	return false
}
