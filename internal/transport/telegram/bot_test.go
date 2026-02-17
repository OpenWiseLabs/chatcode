package telegram

import (
	"testing"

	"chatcode/internal/domain"
)

func TestToDomainMessage_WithThreadID(t *testing.T) {
	u := update{}
	u.Message.Text = "hello"
	u.Message.ID = 123
	u.Message.MessageThreadID = 456
	u.Message.Chat.ID = telegramID("999")
	u.Message.From.ID = telegramID("777")

	msg := toDomainMessage(u)
	if msg.SessionKey.ChatID != "999" {
		t.Fatalf("expected chat id 999, got %q", msg.SessionKey.ChatID)
	}
	if msg.SessionKey.ThreadID != "456" {
		t.Fatalf("expected thread id 456, got %q", msg.SessionKey.ThreadID)
	}
	if msg.Meta.ReplyToMessageID != "123" {
		t.Fatalf("expected reply_to 123, got %q", msg.Meta.ReplyToMessageID)
	}
}

func TestBuildSendPayload_WithThreadID(t *testing.T) {
	msg := domain.OutboundMessage{
		SessionKey: domain.SessionKey{
			Platform: domain.PlatformTelegram,
			ChatID:   "999",
			ThreadID: "456",
		},
		Text: "ok",
	}
	payload := buildSendPayload(msg)
	gotThread, ok := payload["message_thread_id"].(int64)
	if !ok {
		t.Fatalf("expected int64 message_thread_id in payload")
	}
	if gotThread != 456 {
		t.Fatalf("expected thread 456, got %d", gotThread)
	}
}
