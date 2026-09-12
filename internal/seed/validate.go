package seed

import (
	"bytes"
	"fmt"
	"strings"
)

type graphValidator struct {
	ids   map[string]bool
	users map[string]bool
	pairs map[string]bool
}

// Validate checks the generated graph before any connection to MySQL or Redis.
func (d *Dataset) Validate() error {
	if err := d.Options.Validate(); err != nil {
		return err
	}
	if len(d.Users) != d.Options.Users || len(d.Conversations) != d.Options.Conversations {
		return fmt.Errorf("generated user/conversation counts do not match options")
	}
	v := graphValidator{ids: map[string]bool{}, users: map[string]bool{}, pairs: map[string]bool{}}
	if err := v.validateUsers(d.Users); err != nil {
		return err
	}
	total := 0
	for _, c := range d.Conversations {
		if err := v.validateConversation(c); err != nil {
			return err
		}
		total += len(c.Messages)
	}
	if total != d.Options.Messages {
		return fmt.Errorf("generated %d messages, expected %d", total, d.Options.Messages)
	}
	return nil
}

func (v *graphValidator) addID(id []byte) error {
	if len(id) != 16 || v.ids[string(id)] {
		return fmt.Errorf("invalid or duplicate ID %x", id)
	}
	v.ids[string(id)] = true
	return nil
}

func (v *graphValidator) validateUsers(users []User) error {
	emails, usernames := map[string]bool{}, map[string]bool{}
	for i, u := range users {
		if err := v.addID(u.ID); err != nil {
			return err
		}
		if u.Email == "" || emails[strings.ToLower(u.Email)] || usernames[u.Username] {
			return fmt.Errorf("duplicate or empty user identity")
		}
		if i < 2 && u.Email != TestEmails()[i] {
			return fmt.Errorf("required test email is missing")
		}
		emails[strings.ToLower(u.Email)], usernames[u.Username], v.users[string(u.ID)] = true, true, true
	}
	return nil
}

func (v *graphValidator) validateConversation(c Conversation) error {
	if err := v.addID(c.ID); err != nil {
		return err
	}
	members, err := v.validateMembers(c)
	if err != nil {
		return err
	}
	if err := v.validateRoomType(c, members); err != nil {
		return err
	}
	if err := v.validateMessages(c, members); err != nil {
		return err
	}
	return validateWatermarks(c)
}

func (v *graphValidator) validateMembers(c Conversation) (map[string]Member, error) {
	members := make(map[string]Member, len(c.Members))
	for _, m := range c.Members {
		if !v.users[string(m.UserID)] {
			return nil, fmt.Errorf("member references an unknown user")
		}
		if _, exists := members[string(m.UserID)]; exists {
			return nil, fmt.Errorf("duplicate member")
		}
		if m.JoinedAt.Before(c.CreatedAt) || m.Role < 1 || m.Role > 3 {
			return nil, fmt.Errorf("invalid membership")
		}
		members[string(m.UserID)] = m
	}
	if _, exists := members[string(c.CreatorID)]; !exists {
		return nil, fmt.Errorf("conversation creator is not a member")
	}
	return members, nil
}

func (v *graphValidator) validateRoomType(c Conversation, members map[string]Member) error {
	if c.Type != 1 {
		if c.Type != 2 || c.Name == nil || members[string(c.CreatorID)].Role != 2 {
			return fmt.Errorf("invalid group")
		}
		return nil
	}
	if len(c.Members) != 2 || c.Name != nil {
		return fmt.Errorf("invalid DM")
	}
	a, b := c.Members[0].UserID, c.Members[1].UserID
	if bytes.Compare(a, b) > 0 {
		a, b = b, a
	}
	pair := string(a) + string(b)
	if v.pairs[pair] {
		return fmt.Errorf("duplicate DM pair")
	}
	v.pairs[pair] = true
	return nil
}

func (v *graphValidator) validateMessages(c Conversation, members map[string]Member) error {
	previous := c.CreatedAt
	messageIDs := make(map[string]bool, len(c.Messages))
	for i, m := range c.Messages {
		if err := v.addID(m.ID); err != nil {
			return err
		}
		member, exists := members[string(m.SenderID)]
		if !exists || m.CreatedAt.Before(member.JoinedAt) {
			return fmt.Errorf("message sender is not a member at send time")
		}
		if m.Seq != uint64(i+1) || m.CreatedAt.Before(previous) || m.UpdatedAt.Before(m.CreatedAt) {
			return fmt.Errorf("invalid message ordering")
		}
		if m.ParentID != nil && !messageIDs[string(m.ParentID)] {
			return fmt.Errorf("reply does not reference an earlier message in this room")
		}
		if m.DeletedAt != nil && (m.Content != nil || m.DeletedAt.Before(m.CreatedAt)) {
			return fmt.Errorf("invalid soft-deleted message")
		}
		if err := validateReactions(m, members); err != nil {
			return err
		}
		previous, messageIDs[string(m.ID)] = m.CreatedAt, true
	}
	return nil
}

func validateReactions(m Message, members map[string]Member) error {
	reactions := map[string]bool{}
	for _, r := range m.Reactions {
		key := string(r.UserID) + r.Emoji
		if _, exists := members[string(r.UserID)]; !exists || reactions[key] || m.DeletedAt != nil {
			return fmt.Errorf("invalid reaction")
		}
		reactions[key] = true
	}
	return nil
}

func validateWatermarks(c Conversation) error {
	for _, m := range c.Members {
		if m.ReadAt == nil {
			if m.ReadSeq != 0 {
				return fmt.Errorf("unread member has a nonzero read sequence")
			}
		} else if m.ReadSeq == 0 || m.ReadSeq > uint64(len(c.Messages)) || !m.ReadAt.Equal(c.Messages[m.ReadSeq-1].CreatedAt) {
			return fmt.Errorf("read watermark does not identify a message")
		}
	}
	return nil
}

func (c Conversation) LastMessage() (id []byte, preview *string) {
	if len(c.Messages) == 0 {
		return nil, nil
	}
	m := c.Messages[len(c.Messages)-1]
	if m.DeletedAt != nil {
		text := "Message deleted"
		return m.ID, &text
	}
	if m.Content != nil {
		text := string([]rune(*m.Content)[:min(1000, len([]rune(*m.Content)))])
		return m.ID, &text
	}
	return m.ID, nil
}
