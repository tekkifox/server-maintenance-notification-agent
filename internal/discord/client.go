package discord

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
)

type Messenger interface {
	Open() error
	Close() error
	SendMessage(channelID, message string) error
}

type Client struct {
	session *discordgo.Session
}

func NewClient(token string) (*Client, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("discord token is empty")
	}

	if !strings.HasPrefix(token, "Bot ") {
		token = "Bot " + token
	}

	session, err := discordgo.New(token)
	if err != nil {
		return nil, err
	}

	return &Client{session: session}, nil
}

func (c *Client) Open() error {
	return c.session.Open()
}

func (c *Client) Close() error {
	return c.session.Close()
}

func (c *Client) SendMessage(channelID, message string) error {
	_, err := c.session.ChannelMessageSend(channelID, message)
	return err
}
