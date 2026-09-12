// Package seed builds and manages synthetic local datasets. Generation is offline;
// database access is only performed by the explicit CLI apply/verify/sync/reset commands.
package seed

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/brianvoe/gofakeit/v7"
)

const Version = 1

var datasetName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)

func TestEmails() [2]string {
	return [2]string{"23t1020100@husc.edu.vn", "emailnhasai@gmail.com"}
}

type Options struct {
	Dataset       string    `json:"dataset"`
	Users         int       `json:"users"`
	Conversations int       `json:"conversations"`
	Messages      int       `json:"messages"`
	RandomSeed    uint64    `json:"random_seed"`
	At            time.Time `json:"at"`
}

func Defaults(now time.Time) Options {
	return Options{
		Dataset: "local-demo-v1", Users: 50, Conversations: 75, Messages: 25000,
		RandomSeed: 42, At: now.UTC().Truncate(time.Second).Add(-time.Minute),
	}
}

func (o Options) Validate() error {
	switch {
	case !datasetName.MatchString(o.Dataset):
		return fmt.Errorf("dataset must be 1-48 lowercase letters, digits or hyphens, starting with a letter")
	case o.Users != 50:
		return fmt.Errorf("this seed profile requires exactly 50 users (including the two test emails)")
	case o.Conversations < 50 || o.Conversations > 150:
		return fmt.Errorf("conversations must be between 50 and 150")
	case o.Messages < 25000 || o.Messages > 50000:
		return fmt.Errorf("messages must be between 25000 and 50000")
	case o.RandomSeed == 0:
		return fmt.Errorf("random-seed must be nonzero for reproducible generation")
	case o.At.IsZero() || o.At.Year() < 2000 || o.At.Year() > 2100 || o.At.Nanosecond() != 0:
		return fmt.Errorf("at must be a whole-second timestamp between years 2000 and 2100")
	}
	return nil
}

type User struct {
	ID        []byte
	Username  string
	Email     string
	Bio       string
	CreatedAt time.Time
	Owned     bool // False for an existing test account; never modify or delete it.
}

type Member struct {
	UserID   []byte
	Role     int
	Muted    bool
	JoinedAt time.Time
	ReadAt   *time.Time
	ReadSeq  uint64
}

type Message struct {
	ID        []byte
	SenderID  []byte
	ParentID  []byte
	Type      int
	Content   *string
	Seq       uint64
	Edited    bool
	DeletedAt *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
	Reactions []Reaction
}

type Reaction struct {
	UserID []byte
	Emoji  string
}

type Conversation struct {
	ID        []byte
	Type      int
	Name      *string
	CreatorID []byte
	CreatedAt time.Time
	Members   []Member
	Messages  []Message
}

type Dataset struct {
	Options       Options
	Users         []User
	Conversations []Conversation
}

type Summary struct {
	Options       Options   `json:"options"`
	TestEmails    [2]string `json:"test_emails"`
	Direct        int       `json:"direct_conversations"`
	Groups        int       `json:"group_conversations"`
	Empty         int       `json:"empty_conversations"`
	Members       int       `json:"memberships"`
	Messages      int       `json:"messages_including_system_and_deleted"`
	System        int       `json:"system_messages"`
	Replies       int       `json:"replies"`
	Edited        int       `json:"edited_messages"`
	Deleted       int       `json:"deleted_messages"`
	Reactions     int       `json:"reactions"`
	LargestRoom   int       `json:"largest_room_messages"`
	TestUserRooms [2]int    `json:"test_user_conversations"`
}

func (d *Dataset) Summary() Summary {
	s := Summary{Options: d.Options, TestEmails: TestEmails()}
	for _, c := range d.Conversations {
		if c.Type == 1 {
			s.Direct++
		} else {
			s.Groups++
		}
		if len(c.Messages) == 0 {
			s.Empty++
		}
		s.Members += len(c.Members)
		s.Messages += len(c.Messages)
		s.LargestRoom = max(s.LargestRoom, len(c.Messages))
		for _, m := range c.Members {
			for i := range 2 {
				if bytes.Equal(m.UserID, d.Users[i].ID) {
					s.TestUserRooms[i]++
				}
			}
		}
		for _, m := range c.Messages {
			if m.Type == 6 {
				s.System++
			}
			if m.ParentID != nil {
				s.Replies++
			}
			if m.Edited {
				s.Edited++
			}
			if m.DeletedAt != nil {
				s.Deleted++
			}
			s.Reactions += len(m.Reactions)
		}
	}
	return s
}

// IDs retain UUIDv7's time prefix, version and variant. Hash-derived random bits
// make them stable for a recorded dataset/options/time, independently of gofakeit.
func (o Options) id(kind string, ordinal int, at time.Time) []byte {
	h := sha256.Sum256(fmt.Appendf(nil, "stello-seed/%d/%s/%d/%s/%d", Version, o.Dataset, o.RandomSeed, kind, ordinal))
	id := append([]byte(nil), h[:16]...)
	ms := uint64(at.UnixMilli())
	for i := 5; i >= 0; i-- {
		id[i] = byte(ms)
		ms >>= 8
	}
	id[6] = (id[6] & 0x0f) | 0x70
	id[8] = (id[8] & 0x3f) | 0x80
	return id
}

func Generate(o Options) (*Dataset, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	o.At = o.At.UTC()
	fake := gofakeit.New(o.RandomSeed)
	start := o.At.AddDate(0, 0, -30)
	tag := sha256.Sum256([]byte(o.Dataset))
	prefix := hex.EncodeToString(tag[:4])
	d := &Dataset{Options: o}
	for i := range o.Users {
		email := fmt.Sprintf("seed-%s-%02d@example.test", prefix, i+1)
		if i < 2 {
			email = TestEmails()[i]
		}
		d.Users = append(d.Users, User{
			ID: o.id("user", i, start), Username: fmt.Sprintf("seed_%s_%02d", prefix, i+1),
			Email: email, Bio: "Local demo: " + fake.Name(), CreatedAt: start, Owned: true,
		})
	}
	counts := messageCounts(o.Conversations, o.Messages)
	// 24 DMs include both test accounts and different peers; all remaining rooms
	// are groups containing both test users. No duplicate unordered DM pairs.
	for i, count := range counts {
		created := start.Add(time.Duration(i+1) * time.Minute)
		c := Conversation{ID: o.id("conversation", i, created), Type: 1, CreatorID: d.Users[0].ID, CreatedAt: created}
		indices := []int{i % 2, i + 2}
		if i >= 24 {
			indices = []int{0, 1}
			c.Type = 2
			name := fmt.Sprintf("Stello demo %03d", i+1)
			c.Name = &name
			seen := map[int]bool{0: true, 1: true}
			// Rotating membership ensures all 50 users participate.
			for j := range 3 + i%10 {
				idx := 2 + ((i-24)*7+j)%48
				if !seen[idx] {
					indices = append(indices, idx)
					seen[idx] = true
				}
			}
		}
		c.CreatorID = d.Users[indices[0]].ID
		for j, idx := range indices {
			role := 3
			if c.Type == 2 && j == 0 {
				role = 2 // CreateGroup currently assigns admin to its creator.
			}
			c.Members = append(c.Members, Member{UserID: d.Users[idx].ID, Role: role, Muted: j%4 == 3, JoinedAt: created})
		}
		generateMessages(o, fake, &c, i, count)
		for j := range c.Members {
			if count == 0 || (i+j)%3 == 0 {
				continue // Never read.
			}
			read := count - 1
			if (i+j)%3 == 1 {
				read = count / 2 // Partially read.
			}
			m := c.Messages[read]
			c.Members[j].ReadAt, c.Members[j].ReadSeq = &m.CreatedAt, m.Seq
		}
		d.Conversations = append(d.Conversations, c)
	}
	return d, nil
}

// Five empty DMs, five hot rooms, then medium and small rooms. Integer remainder
// goes to the last hot room so every accepted profile has the exact requested total.
func messageCounts(rooms, total int) []int {
	counts := make([]int, rooms)
	weights := make([]int, rooms)
	sum := 0
	for i := 5; i < rooms; i++ {
		w := 1
		if i >= rooms-5 {
			w = 25
		} else if i >= rooms-20 {
			w = 5
		}
		weights[i], sum = w, sum+w
	}
	remaining := total
	for i, w := range weights {
		counts[i] = total * w / sum
		remaining -= counts[i]
	}
	counts[rooms-1] += remaining
	return counts
}

var vietnameseMessages = []string{
	"Chào mọi người, hôm nay nhóm mình cập nhật tiến độ nhé 👋",
	"Mình đã kiểm tra phần đăng nhập và gửi tin nhắn.",
	"Bạn xem giúp mình thay đổi này được không?",
	"Chiều nay mình trao đổi thêm lúc 15 giờ nhé.",
	"Cảm ơn bạn, mình nhận được rồi 👍",
	"Mình đang thử lịch sử trò chuyện và thông báo chưa đọc.",
	"Nội dung có dấu: Nguyễn, Huế, Đà Nẵng; emoji: 🎉 🚀 ❤️",
}

func generateMessages(o Options, fake *gofakeit.Faker, c *Conversation, room, count int) {
	first := c.CreatedAt.Add(time.Hour)
	last := o.At.Add(-time.Duration(room%7) * time.Second)
	for j := range count {
		// Adjacent triples deliberately share a millisecond, exercising seq tie breaks.
		at := first.Add(time.Duration(last.Sub(first).Milliseconds()*int64(j/3)/int64(max(1, (count-1)/3))) * time.Millisecond)
		content := vietnameseMessages[fake.Number(0, len(vietnameseMessages)-1)]
		if j%4 == 0 {
			content += " " + fake.Sentence()
		}
		if j%101 == 0 {
			content = strings.Repeat(content+"\n", 12)
		}
		m := Message{
			ID: o.id(fmt.Sprintf("message-%d", room), j, at), SenderID: c.Members[fake.Number(0, len(c.Members)-1)].UserID,
			Type: 1, Content: &content, Seq: uint64(j + 1), CreatedAt: at, UpdatedAt: at,
		}
		if j == 0 && c.Type == 2 {
			content, m.Type, m.SenderID = "Group created", 6, c.CreatorID
		} else {
			if j > 1 && j%10 == 0 {
				m.ParentID = c.Messages[j-1].ID
			}
			if j%23 == 0 {
				m.Edited = true
				content += " (đã chỉnh sửa)"
				m.UpdatedAt = at.Add(10 * time.Second)
			}
			if j%37 == 0 {
				deleted := at.Add(20 * time.Second).Truncate(time.Second)
				m.DeletedAt, m.UpdatedAt, m.Content = &deleted, at.Add(20*time.Second), nil
			}
			if m.DeletedAt == nil && j%7 == 0 {
				m.Reactions = []Reaction{{UserID: c.Members[(j+1)%len(c.Members)].UserID, Emoji: "👍"}}
			}
		}
		c.Messages = append(c.Messages, m)
	}
}

// RebindUser replaces a synthetic test identity with an existing active account
// without changing its profile, credentials, sessions or account ownership.
func (d *Dataset) RebindUser(index int, id []byte) {
	old := d.Users[index].ID
	d.Users[index].ID, d.Users[index].Owned = id, false
	for i := range d.Conversations {
		c := &d.Conversations[i]
		if bytes.Equal(c.CreatorID, old) {
			c.CreatorID = id
		}
		for j := range c.Members {
			if bytes.Equal(c.Members[j].UserID, old) {
				c.Members[j].UserID = id
			}
		}
		for j := range c.Messages {
			m := &c.Messages[j]
			if bytes.Equal(m.SenderID, old) {
				m.SenderID = id
			}
			for k := range m.Reactions {
				if bytes.Equal(m.Reactions[k].UserID, old) {
					m.Reactions[k].UserID = id
				}
			}
		}
	}
}
