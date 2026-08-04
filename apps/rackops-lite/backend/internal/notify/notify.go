// Package notify delivers events to Discord, Slack, Telegram, and generic
// webhooks. All sends are fire-and-forget with a short timeout.
package notify

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"time"
)

var client = &http.Client{Timeout: 8 * time.Second}

// Targets is the set of configured channels.
type Targets struct {
	DiscordWebhook string
	SlackWebhook   string
	TelegramToken  string
	TelegramChatID string
	GenericWebhook string
}

// Send fans a message out to every configured channel (async).
func Send(t Targets, title, message, level string) {
	go func() {
		if t.DiscordWebhook != "" {
			sendJSON(t.DiscordWebhook, map[string]any{
				"embeds": []map[string]any{{
					"title": title, "description": message,
					"color": colorFor(level),
				}},
			})
		}
		if t.SlackWebhook != "" {
			sendJSON(t.SlackWebhook, map[string]any{
				"text": "*" + title + "*\n" + message,
			})
		}
		if t.TelegramToken != "" && t.TelegramChatID != "" {
			u := "https://api.telegram.org/bot" + t.TelegramToken + "/sendMessage"
			sendJSON(u, map[string]any{
				"chat_id": t.TelegramChatID,
				"text":    title + "\n" + message,
			})
		}
		if t.GenericWebhook != "" {
			sendJSON(t.GenericWebhook, map[string]any{
				"source": "rackops", "title": title, "message": message,
				"level": level, "timestamp": time.Now().Format(time.RFC3339),
			})
		}
	}()
}

func sendJSON(endpoint string, payload any) {
	if _, err := url.Parse(endpoint); err != nil {
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	resp, err := client.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("notify: %v", err)
		return
	}
	_ = resp.Body.Close()
}

func colorFor(level string) int {
	switch level {
	case "critical", "failed":
		return 0xf87171
	case "warning":
		return 0xfbbf24
	default:
		return 0x34d399
	}
}
