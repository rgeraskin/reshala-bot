package telegram

import (
	"fmt"
	"log/slog"
	"strconv"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/rg/aiops/internal/messaging"
)

type Client struct {
	bot *tgbotapi.BotAPI
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

func (c *Client) SendMessage(chatID string, text string) error {
	chatIDInt, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid chat ID: %w", err)
	}

	msg := tgbotapi.NewMessage(chatIDInt, text)
	msg.ParseMode = "Markdown"

	if _, err := c.bot.Send(msg); err != nil {
		msg.ParseMode = ""
		if _, err := c.bot.Send(msg); err != nil {
			return fmt.Errorf("failed to send message: %w", err)
		}
	}

	return nil
}

func (c *Client) SendTyping(chatID string) error {
	chatIDInt, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid chat ID: %w", err)
	}

	action := tgbotapi.NewChatAction(chatIDInt, tgbotapi.ChatTyping)
	if _, err := c.bot.Request(action); err != nil {
		return fmt.Errorf("failed to send typing: %w", err)
	}

	return nil
}

func (c *Client) GetChatType(chatID string) (messaging.ChatType, error) {
	chatIDInt, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid chat ID: %w", err)
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

		msg := convertMessage(update.Message)
		if err := handler(msg); err != nil {
			slog.Error("Error handling message", "error", err)
		}
	}

	return nil
}

func (c *Client) GetPlatformName() string {
	return "telegram"
}

func convertMessage(tgMsg *tgbotapi.Message) *messaging.IncomingMessage {
	return &messaging.IncomingMessage{
		ChatID:    strconv.FormatInt(tgMsg.Chat.ID, 10),
		MessageID: strconv.Itoa(tgMsg.MessageID),
		From: messaging.User{
			ID:        strconv.FormatInt(tgMsg.From.ID, 10),
			Username:  tgMsg.From.UserName,
			FirstName: tgMsg.From.FirstName,
			LastName:  tgMsg.From.LastName,
		},
		Text:      tgMsg.Text,
		Timestamp: time.Unix(int64(tgMsg.Date), 0),
	}
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
