//go:build dev

package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/jessepcc/victoria/internal/channel"
	"github.com/jessepcc/victoria/internal/domain"
)

// registerDevEndpoints mounts demo/test-only routes under /admin/dev/*.
// Compiled in only with the `dev` build tag — production binaries have the
// stub from dev_endpoints_stub.go and contain none of this code. Routes here
// can impersonate any operator (IsFromMe=true), so they MUST stay out of
// production builds entirely.
func registerDevEndpoints(s *Server, r chi.Router) {
	r.Post("/admin/dev/whatsapp/inbound", s.devWhatsAppInbound)
	r.Post("/admin/dev/whatsapp/session-status", s.devSetSessionStatus)
}

// devWhatsAppInbound simulates a whatsmeow message landing on a paired
// tenant's session. Honours the same App.HandleWhatsAppInbound code path as
// the real adapter, including the IsFromMe operator-impersonation flag.
func (s *Server) devWhatsAppInbound(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID          string `json:"tenant_id"`
		SenderJID         string `json:"sender_jid"`
		SenderIdentifier  string `json:"sender_identifier"`
		ProviderMessageID string `json:"provider_message_id"`
		FreeText          string `json:"free_text"`
		IsFromMe          bool   `json:"is_from_me"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.TenantID) == "" {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	if req.ProviderMessageID == "" {
		req.ProviderMessageID = "dev-" + strconv.FormatInt(int64(len(req.FreeText)), 10)
	}
	if req.SenderIdentifier == "" {
		req.SenderIdentifier = req.SenderJID
	}
	msg := channel.InboundMessage{
		SenderIdentifier:  req.SenderIdentifier,
		SenderJID:         req.SenderJID,
		ProviderMessageID: req.ProviderMessageID,
		Channel:           channel.ChannelWhatsApp,
		FreeText:          req.FreeText,
		IsFromMe:          req.IsFromMe,
	}
	if err := s.app.HandleWhatsAppInbound(r.Context(), req.TenantID, msg); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

// devSetSessionStatus lets a demo flow flip a tenant's WhatsApp session
// state without going through pairing. Required so the gateway will deliver
// outbound packets through the dev WA adapter.
func (s *Server) devSetSessionStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID string `json:"tenant_id"`
		Channel  string `json:"channel"`
		Status   string `json:"status"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.Status) == "" {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	ch := channel.Channel(req.Channel)
	if ch == "" {
		ch = channel.ChannelWhatsApp
	}
	s.app.NotifyChannelSession(r.Context(), req.TenantID, ch, domain.SessionStatus(req.Status))
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}
