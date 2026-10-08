package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Telegram forwards a copy of each lead to one chat. It only ever sends:
// the bot does not read updates, so nobody can ask it for anything.
//
// The bot token is part of every request URL, and Go's HTTP errors quote
// the URL. No error from here wraps a transport error or prints a URL, so
// the token cannot reach a log line.
type Telegram struct {
	token  string
	chat   string
	base   string
	client *http.Client
}

func NewTelegram(token, chat string) *Telegram {
	return &Telegram{token: token, chat: chat, base: "https://api.telegram.org", client: &http.Client{Timeout: 15 * time.Second}}
}

func (t *Telegram) redact(s string) string {
	if t.token == "" {
		return s
	}
	s = strings.ReplaceAll(s, t.token, "<token>")
	return strings.ReplaceAll(s, url.PathEscape(t.token), "<token>")
}

func (t *Telegram) Send(ctx context.Context, text string) error {
	body, err := json.Marshal(map[string]any{
		"chat_id":                  t.chat,
		"text":                     text,
		"disable_web_page_preview": true,
	})
	if err != nil {
		return errors.New("telegram: cannot encode message")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.base+"/bot"+t.token+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return errors.New("telegram: cannot build request")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return errors.New("telegram: timed out")
		}
		return errors.New("telegram: network error")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode/100 == 2 {
		return nil
	}
	var answer struct {
		Description string `json:"description"`
	}
	_ = json.Unmarshal(raw, &answer)
	// Redact before trimming: a cut could leave half a token behind.
	desc := []rune(t.redact(answer.Description))
	if len(desc) > 200 {
		desc = desc[:200]
	}
	return fmt.Errorf("telegram: HTTP %d: %s", resp.StatusCode, string(desc))
}
