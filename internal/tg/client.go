// Package tg implements a minimal Telegram Bot API client using long
// polling. It covers the subset of the Bot API the bot needs: sending
// messages, editing messages, and handling incoming updates.
package tg

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Client is a Telegram Bot API client.
type Client struct {
	token string
	http  *http.Client
	api   string

	mu       sync.Mutex
	offset   int64
	lastSeen int64
}

// NewClient creates a client for the given bot token.
func NewClient(token string) *Client {
	return &Client{
		token: token,
		http:  &http.Client{Timeout: 60 * time.Second},
		api:   "https://api.telegram.org/bot" + token,
	}
}

// Update is a Telegram update envelope.
type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message"`
	EditedMessage *Message       `json:"edited_message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

// Message is a Telegram message.
type Message struct {
	MessageID      int64    `json:"message_id"`
	Chat           *Chat    `json:"chat"`
	From           *User    `json:"from"`
	Text           string   `json:"text"`
	ReplyToMessage *Message `json:"reply_to_message"`
}

// Chat is a Telegram chat.
type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

// User is a Telegram user.
type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

// CallbackQuery is an inline button callback.
type CallbackQuery struct {
	ID      string   `json:"id"`
	From    *User    `json:"from"`
	Message *Message `json:"message"`
	Data    string   `json:"data"`
}

// SendOptions configures an outgoing message.
type SendOptions struct {
	ChatID        int64
	Text          string
	ReplyTo       int64
	DisableNotify bool
}

// Send sends a text message and returns the sent message ID.
func (c *Client) Send(ctx context.Context, opts SendOptions) (int64, error) {
	form := url.Values{}
	form.Set("chat_id", strconv.FormatInt(opts.ChatID, 10))
	form.Set("text", opts.Text)
	if opts.ReplyTo > 0 {
		form.Set("reply_to_message_id", strconv.FormatInt(opts.ReplyTo, 10))
	}
	if opts.DisableNotify {
		form.Set("disable_notification", "true")
	}
	var resp struct {
		OK       bool    `json:"ok"`
		Result   Message `json:"result"`
		Describe string  `json:"description"`
	}
	if err := c.call(ctx, "sendMessage", form, &resp); err != nil {
		return 0, err
	}
	return resp.Result.MessageID, nil
}

// Edit updates an existing message's text.
func (c *Client) Edit(ctx context.Context, chatID, messageID int64, text string) error {
	form := url.Values{}
	form.Set("chat_id", strconv.FormatInt(chatID, 10))
	form.Set("message_id", strconv.FormatInt(messageID, 10))
	form.Set("text", text)
	return c.call(ctx, "editMessageText", form, nil)
}

// AnswerCallback acknowledges an inline button callback.
func (c *Client) AnswerCallback(ctx context.Context, callbackID, text string, showAlert bool) error {
	form := url.Values{}
	form.Set("callback_query_id", callbackID)
	if text != "" {
		form.Set("text", text)
	}
	if showAlert {
		form.Set("show_alert", "true")
	}
	return c.call(ctx, "answerCallbackQuery", form, nil)
}

// Poll fetches a batch of updates, tracking the offset so no update is
// missed or duplicated.
func (c *Client) Poll(ctx context.Context, timeoutSeconds int) ([]Update, error) {
	c.mu.Lock()
	offset := c.offset
	c.mu.Unlock()

	form := url.Values{}
	form.Set("offset", strconv.FormatInt(offset, 10))
	form.Set("timeout", strconv.Itoa(timeoutSeconds))
	form.Set("allowed_updates", `["message","callback_query"]`)

	var resp struct {
		OK     bool     `json:"ok"`
		Result []Update `json:"result"`
	}
	if err := c.call(ctx, "getUpdates", form, &resp); err != nil {
		return nil, err
	}

	c.mu.Lock()
	for _, u := range resp.Result {
		if u.UpdateID >= c.lastSeen {
			c.lastSeen = u.UpdateID
		}
	}
	if len(resp.Result) > 0 {
		c.offset = resp.Result[len(resp.Result)-1].UpdateID + 1
	}
	c.mu.Unlock()

	return resp.Result, nil
}

func (c *Client) call(ctx context.Context, method string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.api+"/"+method, nil)
	if err != nil {
		return err
	}
	body := form.Encode()
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Body = io.NopCloser(strings.NewReader(body))

	httpResp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()

	data, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return err
	}
	if httpResp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram %s: HTTP %d: %s", method, httpResp.StatusCode, string(data))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("telegram %s: decode: %w", method, err)
	}
	var envelope struct {
		OK  bool   `json:"ok"`
		Err string `json:"description"`
	}
	_ = json.Unmarshal(data, &envelope)
	if !envelope.OK {
		return fmt.Errorf("telegram %s: %s", method, envelope.Err)
	}
	return nil
}
