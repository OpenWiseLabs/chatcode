package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"chatcode/internal/domain"
)

type WebBridge struct {
	listenAddr      string
	allowedSenderID string
	outboundURL     string
	handler         domain.MessageHandler
	server          *http.Server
}

func NewWebBridge(listenAddr, allowedSenderID string) *WebBridge {
	return &WebBridge{listenAddr: listenAddr, allowedSenderID: allowedSenderID}
}

func (w *WebBridge) Name() string { return "whatsapp" }

func (w *WebBridge) Start(ctx context.Context, handler domain.MessageHandler) error {
	w.handler = handler
	mux := http.NewServeMux()
	mux.HandleFunc("/whatsapp/inbound", w.handleInbound)
	mux.HandleFunc("/whatsapp/config/outbound", w.handleConfig)
	w.server = &http.Server{Addr: w.listenAddr, Handler: mux}
	slog.Info("transport started", "transport", "whatsapp", "listen_addr", w.listenAddr)
	go func() {
		<-ctx.Done()
		_ = w.server.Shutdown(context.Background())
	}()
	if err := w.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (w *WebBridge) Send(ctx context.Context, msg domain.OutboundMessage) error {
	if w.outboundURL == "" {
		return fmt.Errorf("whatsapp outbound URL is not configured")
	}
	payload := map[string]string{"chat_id": msg.SessionKey.ChatID, "text": msg.Text}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, w.outboundURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("whatsapp outbound status=%d", resp.StatusCode)
	}
	return nil
}

func (w *WebBridge) handleInbound(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		rw.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		SenderID string `json:"sender_id"`
		ChatID   string `json:"chat_id"`
		Text     string `json:"text"`
	}
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		rw.WriteHeader(http.StatusBadRequest)
		return
	}
	if payload.SenderID != w.allowedSenderID {
		rw.WriteHeader(http.StatusForbidden)
		return
	}
	msg := domain.Message{
		SessionKey: domain.SessionKey{Platform: domain.PlatformWhatsApp, ChatID: payload.ChatID},
		SenderID:   payload.SenderID,
		Text:       payload.Text,
		At:         time.Now().UTC(),
	}
	slog.Info("whatsapp inbound message",
		"chat_id", msg.SessionKey.ChatID,
		"sender_id", msg.SenderID,
	)
	_ = w.handler(req.Context(), msg)
	rw.WriteHeader(http.StatusAccepted)
}

func (w *WebBridge) handleConfig(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		rw.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		OutboundURL string `json:"outbound_url"`
	}
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		rw.WriteHeader(http.StatusBadRequest)
		return
	}
	w.outboundURL = payload.OutboundURL
	rw.WriteHeader(http.StatusNoContent)
}
