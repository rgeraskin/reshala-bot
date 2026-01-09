package slack

import (
	"fmt"
	"log/slog"

	"github.com/rg/aiops/internal/messaging"
)

type Client struct {
	token string
}

func NewClient(token string) (*Client, error) {
	if token == "" {
		return nil, fmt.Errorf("slack token is required")
	}

	slog.Info("Slack client initialized (stub implementation)")

	return &Client{
		token: token,
	}, nil
}

func (c *Client) SendMessage(chatID string, text string) error {
	return fmt.Errorf("slack integration not yet implemented")
}

func (c *Client) SendTyping(chatID string) error {
	return nil
}

func (c *Client) GetChatType(chatID string) (messaging.ChatType, error) {
	return messaging.ChatTypeGroup, nil
}

func (c *Client) IsGroupOrChannel(chatID string) bool {
	return true
}

func (c *Client) Start(handler messaging.MessageHandler) error {
	return fmt.Errorf("slack integration not yet implemented")
}

func (c *Client) GetPlatformName() string {
	return "slack"
}
