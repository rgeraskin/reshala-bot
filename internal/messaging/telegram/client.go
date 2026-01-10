package telegram

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/rg/aiops/internal/messaging"
)

type Client struct {
	bot *tgbotapi.BotAPI
}

// ReactionType represents a Telegram reaction for the setMessageReaction API call.
// This is needed because go-telegram-bot-api/v5.5.1 predates native reaction support.
type ReactionType struct {
	Type  string `json:"type"`
	Emoji string `json:"emoji"`
}

// parseChatID converts a string chat ID to int64 for Telegram API calls
func parseChatID(chatID string) (int64, error) {
	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid chat ID: %w", err)
	}
	return id, nil
}

func NewClient(token string) (*Client, error) {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("failed to create telegram bot: %w", err)
	}

	bot.Debug = false
	slog.Info("Authorized on Telegram account", "username", bot.Self.UserName)

	return &Client{
		bot: bot,
	}, nil
}

func (c *Client) SendMessage(outMsg *messaging.OutgoingMessage) (string, error) {
	chatIDInt, err := parseChatID(outMsg.ChatID)
	if err != nil {
		return "", err
	}

	msg := tgbotapi.NewMessage(chatIDInt, outMsg.Text)
	msg.ParseMode = "Markdown"

	// Add reply-to if specified
	if outMsg.ReplyToMessageID != "" {
		replyToID, err := strconv.Atoi(outMsg.ReplyToMessageID)
		if err != nil {
			slog.Warn("Invalid reply-to message ID, ignoring",
				"chat_id", outMsg.ChatID,
				"reply_to_message_id", outMsg.ReplyToMessageID,
				"error", err)
		} else {
			msg.ReplyToMessageID = replyToID
		}
	}

	// Send with markdown, fallback to plain text
	sentMsg, err := c.bot.Send(msg)
	if err != nil {
		msg.ParseMode = ""
		sentMsg, err = c.bot.Send(msg)
		if err != nil {
			return "", fmt.Errorf("failed to send message: %w", err)
		}
	}

	return strconv.Itoa(sentMsg.MessageID), nil
}

func (c *Client) AddReaction(chatID, messageID, emoji string) error {
	chatIDInt, err := parseChatID(chatID)
	if err != nil {
		return err
	}

	msgIDInt, err := strconv.Atoi(messageID)
	if err != nil {
		return fmt.Errorf("invalid message ID: %w", err)
	}

	// Build params for the setMessageReaction API call
	params := make(tgbotapi.Params)
	params.AddNonZero64("chat_id", chatIDInt)
	params.AddNonZero("message_id", msgIDInt)
	params.AddInterface("reaction", []ReactionType{
		{Type: "emoji", Emoji: emoji},
	})

	_, err = c.bot.MakeRequest("setMessageReaction", params)
	if err != nil {
		slog.Warn("Failed to add reaction",
			"chat_id", chatID,
			"message_id", messageID,
			"emoji", emoji,
			"error", err)
		return err
	}

	return nil
}

func (c *Client) SendTyping(chatID string) error {
	chatIDInt, err := parseChatID(chatID)
	if err != nil {
		return err
	}

	action := tgbotapi.NewChatAction(chatIDInt, tgbotapi.ChatTyping)
	if _, err := c.bot.Request(action); err != nil {
		return fmt.Errorf("failed to send typing: %w", err)
	}

	return nil
}

func (c *Client) GetChatType(chatID string) (messaging.ChatType, error) {
	chatIDInt, err := parseChatID(chatID)
	if err != nil {
		return "", err
	}

	chatConfig := tgbotapi.ChatInfoConfig{
		ChatConfig: tgbotapi.ChatConfig{
			ChatID: chatIDInt,
		},
	}

	chat, err := c.bot.GetChat(chatConfig)
	if err != nil {
		return "", fmt.Errorf("failed to get chat: %w", err)
	}

	return convertChatType(chat.Type), nil
}

func (c *Client) IsGroupOrChannel(chatID string) bool {
	chatType, err := c.GetChatType(chatID)
	if err != nil {
		slog.Warn("Failed to get chat type", "chat_id", chatID, "error", err)
		return false
	}
	return chatType.IsGroupOrChannel()
}

func (c *Client) Start(handler messaging.MessageHandler) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := c.bot.GetUpdatesChan(u)

	slog.Info("Telegram bot started, listening for messages")

	for update := range updates {
		if update.Message == nil {
			continue
		}

		msg := convertMessage(update.Message, c.bot.Self.UserName)
		if err := handler(msg); err != nil {
			slog.Error("Error handling message", "error", err)
		}
	}

	return nil
}

// Stop gracefully shuts down the Telegram client
func (c *Client) Stop() {
	slog.Info("Stopping Telegram bot")
	c.bot.StopReceivingUpdates()
}

func convertMessage(tgMsg *tgbotapi.Message, botUsername string) *messaging.IncomingMessage {
	msg := &messaging.IncomingMessage{
		ChatID:    strconv.FormatInt(tgMsg.Chat.ID, 10),
		MessageID: strconv.Itoa(tgMsg.MessageID),
		Text:      tgMsg.Text,
		Timestamp: time.Unix(int64(tgMsg.Date), 0),

		// Filtering metadata
		ChatType:         convertChatType(tgMsg.Chat.Type),
		IsMentioningBot:  detectBotMention(tgMsg, botUsername),
		IsReplyToBot:     detectReplyToBot(tgMsg, botUsername),
		ReplyToMessageID: getReplyToMessageID(tgMsg),
	}

	// From can be nil for channel posts or forwarded messages without sender
	if tgMsg.From != nil {
		msg.From = messaging.User{
			ID:        strconv.FormatInt(tgMsg.From.ID, 10),
			Username:  tgMsg.From.UserName,
			FirstName: tgMsg.From.FirstName,
			LastName:  tgMsg.From.LastName,
		}
	}

	return msg
}

// detectBotMention checks if the message contains an @mention of the bot.
func detectBotMention(tgMsg *tgbotapi.Message, botUsername string) bool {
	if tgMsg.Entities == nil || botUsername == "" {
		slog.Debug("No entities or empty botUsername",
			"has_entities", tgMsg.Entities != nil,
			"bot_username", botUsername,
			"chat_id", tgMsg.Chat.ID)
		return false
	}

	slog.Debug("Checking for bot mention",
		"chat_id", tgMsg.Chat.ID,
		"text", tgMsg.Text,
		"bot_username", botUsername,
		"num_entities", len(tgMsg.Entities))

	for _, entity := range tgMsg.Entities {
		slog.Debug("Processing entity",
			"chat_id", tgMsg.Chat.ID,
			"type", entity.Type,
			"offset", entity.Offset,
			"length", entity.Length)

		if entity.Type == "mention" {
			mention := extractEntityText(tgMsg.Text, entity)
			slog.Debug("Found mention entity",
				"chat_id", tgMsg.Chat.ID,
				"mention", mention,
				"bot_username", botUsername,
				"expected", "@"+botUsername)

			// Compare case-insensitively (Telegram usernames are case-insensitive)
			if strings.EqualFold(mention, "@"+botUsername) {
				slog.Info("Bot mention detected", "chat_id", tgMsg.Chat.ID, "mention", mention)
				return true
			}
		}
	}

	slog.Debug("No bot mention found", "chat_id", tgMsg.Chat.ID)
	return false
}

// extractEntityText extracts the text for a Telegram entity, handling UTF-16 offset conversion.
// Telegram uses UTF-16 code units for offsets, while Go strings are UTF-8.
func extractEntityText(text string, entity tgbotapi.MessageEntity) string {
	// Convert UTF-16 offsets to byte offsets
	byteStart := utf16OffsetToByteOffset(text, entity.Offset)
	byteEnd := utf16OffsetToByteOffset(text, entity.Offset+entity.Length)

	if byteStart < 0 || byteEnd > len(text) || byteStart > byteEnd {
		return ""
	}

	return text[byteStart:byteEnd]
}

// utf16OffsetToByteOffset converts a UTF-16 code unit offset to a byte offset in a UTF-8 string.
// Telegram uses UTF-16 code units for entity offsets:
// - BMP characters (U+0000 to U+FFFF): 1 UTF-16 unit = 1-3 UTF-8 bytes
// - Non-BMP characters (emoji, etc.): 2 UTF-16 units = 4 UTF-8 bytes (surrogate pair)
func utf16OffsetToByteOffset(text string, utf16Offset int) int {
	utf16Pos := 0
	bytePos := 0

	for _, r := range text {
		if utf16Pos >= utf16Offset {
			break
		}

		// Characters outside BMP use surrogate pairs (2 UTF-16 units)
		if r > 0xFFFF {
			utf16Pos += 2
		} else {
			utf16Pos += 1
		}

		bytePos += len(string(r))
	}

	return bytePos
}

// detectReplyToBot checks if the message is a direct reply to a bot message.
func detectReplyToBot(tgMsg *tgbotapi.Message, botUsername string) bool {
	if tgMsg.ReplyToMessage == nil || botUsername == "" {
		return false
	}

	// Check if the original message was from the bot
	if tgMsg.ReplyToMessage.From != nil {
		return strings.EqualFold(tgMsg.ReplyToMessage.From.UserName, botUsername)
	}

	return false
}

// getReplyToMessageID returns the message ID being replied to, or empty string if not a reply.
func getReplyToMessageID(tgMsg *tgbotapi.Message) string {
	if tgMsg.ReplyToMessage != nil {
		return strconv.Itoa(tgMsg.ReplyToMessage.MessageID)
	}
	return ""
}

func convertChatType(tgType string) messaging.ChatType {
	switch tgType {
	case "private":
		return messaging.ChatTypePrivate
	case "group", "supergroup":
		return messaging.ChatTypeGroup
	case "channel":
		return messaging.ChatTypeChannel
	default:
		return messaging.ChatTypePrivate
	}
}
