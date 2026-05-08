package mailer

import "context"

type Mailer interface {
	Send(ctx context.Context, msg *Message) error
}

type Message struct {
	To      string
	Subject string
	HTML    string
}
