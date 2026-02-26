package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"chatcode/internal/domain"
)

type Bot struct {
	token         string
	allowedUserID string
	httpClient    *http.Client
	offset        int64
}

type botCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

func New(token string, allowedUserID string) *Bot {
	return &Bot{token: token, allowedUserID: allowedUserID, httpClient: &http.Client{Timeout: 20 * time.Second}}
}

func (b *Bot) Name() string { return "telegram" }

func (b *Bot) Start(ctx context.Context, handler domain.MessageHandler) error {
	slog.Info("transport started", "transport", "telegram")
	if err := b.setMyCommands(ctx); err != nil {
		slog.Error("telegram setMyCommands failed", "error", err)
	} else {
		slog.Info("telegram commands registered")
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		updates, err := b.getUpdates(ctx)
		if err != nil {
			slog.Error("telegram getUpdates failed", "error", err)
			time.Sleep(2 * time.Second)
			continue
		}
		for _, u := range updates {
			// Always advance offset for every received update, including filtered ones.
			b.offset = u.UpdateID + 1
			if u.Message.From.ID.String() != b.allowedUserID {
				continue
			}
			msg := toDomainMessage(u)
			slog.Info("telegram inbound message",
				"chat_id", msg.SessionKey.ChatID,
				"thread_id", msg.SessionKey.ThreadID,
				"sender_id", msg.SenderID,
			)
			_ = handler(ctx, msg)
		}
	}
}

func (b *Bot) setMyCommands(ctx context.Context) error {
	commands := []botCommand{
		{Command: "new", Description: "Create and switch workdir: /new <project_dir>"},
		{Command: "cd", Description: "Set workdir: /cd [project_dir], empty uses project root"},
		{Command: "list", Description: "List projects under project root"},
		{Command: "codex", Description: "Use codex or run once: /codex <prompt>"},
		{Command: "claude", Description: "Use claude or run once: /claude <prompt>"},
		{Command: "mode", Description: "Set session permission mode: /mode <sandbox|full-access>"},
		{Command: "status", Description: "Show current session status"},
		{Command: "reset", Description: "Reset current session"},
		{Command: "stop", Description: "Stop running job: /stop <job_id>"},
	}
	payload := map[string]any{
		"commands": commands,
		"scope": map[string]string{
			"type": "default",
		},
	}
	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("https://api.telegram.org/bot%s/setMyCommands", b.token)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("telegram setMyCommands status=%d", resp.StatusCode)
	}
	return nil
}

func (b *Bot) Send(ctx context.Context, msg domain.OutboundMessage) error {
	payload := buildSendPayload(msg)
	if msg.Format == "html" {
		payload["parse_mode"] = "HTML"
	}
	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", b.token)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("telegram send status=%d", resp.StatusCode)
	}
	return nil
}

func (b *Bot) getUpdates(ctx context.Context) ([]update, error) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?timeout=20&offset=%s", b.token, strconv.FormatInt(b.offset, 10))
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("telegram getUpdates status=%d", resp.StatusCode)
	}
	var envelope struct {
		OK     bool     `json:"ok"`
		Result []update `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, err
	}
	return envelope.Result, nil
}

type update struct {
	UpdateID int64 `json:"update_id"`
	Message  struct {
		ID              int64  `json:"message_id"`
		Text            string `json:"text"`
		MessageThreadID int64  `json:"message_thread_id"`
		Chat            struct {
			ID telegramID `json:"id"`
		} `json:"chat"`
		From struct {
			ID telegramID `json:"id"`
		} `json:"from"`
	} `json:"message"`
}

type telegramID string

func (t *telegramID) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*t = ""
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*t = telegramID(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	*t = telegramID(n.String())
	return nil
}

func (t telegramID) String() string {
	return string(t)
}

func toDomainMessage(u update) domain.Message {
	key := domain.SessionKey{
		Platform: domain.PlatformTelegram,
		ChatID:   u.Message.Chat.ID.String(),
	}
	if u.Message.MessageThreadID != 0 {
		key.ThreadID = strconv.FormatInt(u.Message.MessageThreadID, 10)
	}
	return domain.Message{
		SessionKey: key,
		SenderID:   u.Message.From.ID.String(),
		Text:       u.Message.Text,
		Meta: domain.InboundMessageMeta{
			ReplyToMessageID: strconv.FormatInt(u.Message.ID, 10),
			Raw:              map[string]string{"telegram_message_id": strconv.FormatInt(u.Message.ID, 10)},
		},
		At: time.Now().UTC(),
	}
}

func buildSendPayload(msg domain.OutboundMessage) map[string]any {
	payload := map[string]any{
		"chat_id": msg.SessionKey.ChatID,
		"text":    msg.Text,
	}
	if msg.SessionKey.ThreadID != "" {
		if threadID, err := strconv.ParseInt(msg.SessionKey.ThreadID, 10, 64); err == nil {
			payload["message_thread_id"] = threadID
		}
	}
	return payload
}
