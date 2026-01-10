package messaging

import "time"

type Platform interface {
	SendMessage(chatID string, text string) error
	SendTyping(chatID string) error
	GetChatType(chatID string) (ChatType, error)
	IsGroupOrChannel(chatID string) bool
	Start(handler MessageHandler) error
	Stop()
}

type MessageHandler func(msg *IncomingMessage) error

type IncomingMessage struct {
	ChatID    string
	MessageID string
	From      User
	Text      string
	Timestamp time.Time

	// Filtering metadata (platform-agnostic)
	ChatType         ChatType // Chat type: private, group, or channel
	IsMentioningBot  bool     // True if message @mentions the bot
	IsReplyToBot     bool     // True if message is a direct reply to a bot message
	ReplyToMessageID string   // ID of message being replied to (empty if not a reply)
}

type User struct {
	ID        string
	Username  string
	FirstName string
	LastName  string
}

type ChatType string

const (
	ChatTypePrivate ChatType = "private"
	ChatTypeGroup   ChatType = "group"
	ChatTypeChannel ChatType = "channel"
)

func (ct ChatType) String() string {
	return string(ct)
}

func (ct ChatType) IsGroupOrChannel() bool {
	return ct == ChatTypeGroup || ct == ChatTypeChannel
}
