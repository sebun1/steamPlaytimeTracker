package sptt

const (
	NotifUserListUpdateID = 1
)

type Notif struct {
	MessageType int
	Payload     interface{}
}

func NotifUserListUpdate() Notif {
	return Notif{MessageType: NotifUserListUpdateID, Payload: nil}
}

func (n Notif) IsUserListUpdate() bool {
	return n.MessageType == NotifUserListUpdateID
}
