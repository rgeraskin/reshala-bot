package telegram

import (
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestDetectBotMention(t *testing.T) {
	tests := []struct {
		name        string
		text        string
		entities    []tgbotapi.MessageEntity
		botUsername string
		want        bool
	}{
		{
			name:        "direct_mention",
			text:        "@mybot hello",
			entities:    []tgbotapi.MessageEntity{{Type: "mention", Offset: 0, Length: 6}},
			botUsername: "mybot",
			want:        true,
		},
		{
			name:        "mention_in_middle",
			text:        "hey @mybot what's up",
			entities:    []tgbotapi.MessageEntity{{Type: "mention", Offset: 4, Length: 6}},
			botUsername: "mybot",
			want:        true,
		},
		{
			name:        "no_mention",
			text:        "hello world",
			entities:    nil,
			botUsername: "mybot",
			want:        false,
		},
		{
			name:        "empty_entities",
			text:        "hello world",
			entities:    []tgbotapi.MessageEntity{},
			botUsername: "mybot",
			want:        false,
		},
		{
			name:        "wrong_bot",
			text:        "@otherbot hello",
			entities:    []tgbotapi.MessageEntity{{Type: "mention", Offset: 0, Length: 9}},
			botUsername: "mybot",
			want:        false,
		},
		{
			name:        "case_insensitive",
			text:        "@MyBot hello",
			entities:    []tgbotapi.MessageEntity{{Type: "mention", Offset: 0, Length: 6}},
			botUsername: "mybot",
			want:        true,
		},
		{
			name:        "empty_bot_username",
			text:        "@mybot hello",
			entities:    []tgbotapi.MessageEntity{{Type: "mention", Offset: 0, Length: 6}},
			botUsername: "",
			want:        false,
		},
		{
			name:        "hashtag_not_mention",
			text:        "#mybot hello",
			entities:    []tgbotapi.MessageEntity{{Type: "hashtag", Offset: 0, Length: 6}},
			botUsername: "mybot",
			want:        false,
		},
		{
			name:        "url_not_mention",
			text:        "https://example.com @mybot",
			entities:    []tgbotapi.MessageEntity{{Type: "url", Offset: 0, Length: 19}, {Type: "mention", Offset: 20, Length: 6}},
			botUsername: "mybot",
			want:        true,
		},
		{
			name:        "mention_after_emoji",
			text:        "🎉 @mybot hello",
			entities:    []tgbotapi.MessageEntity{{Type: "mention", Offset: 3, Length: 6}}, // UTF-16: emoji=2, space=1, @mybot starts at 3
			botUsername: "mybot",
			want:        true,
		},
		{
			name:        "mention_after_multiple_emoji",
			text:        "👍👎 @mybot test",
			entities:    []tgbotapi.MessageEntity{{Type: "mention", Offset: 5, Length: 6}}, // UTF-16: each emoji=2, space=1
			botUsername: "mybot",
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &tgbotapi.Message{
				Text:     tt.text,
				Entities: tt.entities,
			}
			got := detectBotMention(msg, tt.botUsername)
			if got != tt.want {
				t.Errorf("detectBotMention() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDetectReplyToBot(t *testing.T) {
	tests := []struct {
		name           string
		replyToMessage *tgbotapi.Message
		botUsername    string
		want           bool
	}{
		{
			name: "reply_to_bot",
			replyToMessage: &tgbotapi.Message{
				From: &tgbotapi.User{UserName: "mybot"},
			},
			botUsername: "mybot",
			want:        true,
		},
		{
			name: "reply_to_other_user",
			replyToMessage: &tgbotapi.Message{
				From: &tgbotapi.User{UserName: "otheruser"},
			},
			botUsername: "mybot",
			want:        false,
		},
		{
			name:           "no_reply",
			replyToMessage: nil,
			botUsername:    "mybot",
			want:           false,
		},
		{
			name: "reply_to_message_without_from",
			replyToMessage: &tgbotapi.Message{
				From: nil, // Can happen with channel messages
			},
			botUsername: "mybot",
			want:        false,
		},
		{
			name: "case_insensitive",
			replyToMessage: &tgbotapi.Message{
				From: &tgbotapi.User{UserName: "MyBot"},
			},
			botUsername: "mybot",
			want:        true,
		},
		{
			name: "empty_bot_username",
			replyToMessage: &tgbotapi.Message{
				From: &tgbotapi.User{UserName: "mybot"},
			},
			botUsername: "",
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &tgbotapi.Message{
				ReplyToMessage: tt.replyToMessage,
			}
			got := detectReplyToBot(msg, tt.botUsername)
			if got != tt.want {
				t.Errorf("detectReplyToBot() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetReplyToMessageID(t *testing.T) {
	tests := []struct {
		name           string
		replyToMessage *tgbotapi.Message
		want           string
	}{
		{
			name: "has_reply",
			replyToMessage: &tgbotapi.Message{
				MessageID: 12345,
			},
			want: "12345",
		},
		{
			name:           "no_reply",
			replyToMessage: nil,
			want:           "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &tgbotapi.Message{
				ReplyToMessage: tt.replyToMessage,
			}
			got := getReplyToMessageID(msg)
			if got != tt.want {
				t.Errorf("getReplyToMessageID() = %v, want %v", got, tt.want)
			}
		})
	}
}
