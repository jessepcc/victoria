package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	qrcode "github.com/skip2/go-qrcode"

	"victoria/internal/app"
	"victoria/internal/channel"
	"victoria/internal/domain"
)

// WhatsAppManager is the subset of channel/whatsapp.Manager that the HTTP
// layer needs. Defined here to keep the http package free of a hard whatsmeow
// dependency in tests.
type WhatsAppManager interface {
	BeginPairing(ctx context.Context, tenantID string) (string, error)
	CurrentQR(tenantID string) string
	Status(tenantID string) domain.SessionStatus
	Logout(ctx context.Context, tenantID string) error
}

type Server struct {
	app      *app.App
	whatsapp WhatsAppManager
}

func New(application *app.App, wa WhatsAppManager) *Server {
	return &Server{app: application, whatsapp: wa}
}

func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Get("/healthz", s.health)
	r.Post("/admin/tenants", s.provisionTenant)
	r.Post("/admin/candidates/{tenant_id}/{candidate_id}/promote", s.promoteCandidate)
	r.Post("/admin/replays", s.replayCase)
	r.With(tenantAuth).Post("/cases", s.startCase)
	r.With(tenantAuth).Get("/skill-versions/active", s.activeSkillVersion)
	r.With(tenantAuth).Get("/candidates", s.listCandidates)
	r.Post("/gateway/inbound", s.gatewayInbound)
	r.Get("/mcp/tools", s.mcpTools)
	r.With(tenantAuth).Post("/mcp/write-final", s.mcpWriteFinal)
	r.With(tenantAuth).Post("/ingest/customer-message", s.ingestCustomerMessage)
	r.With(tenantAuth).Post("/channel-bindings/whatsapp/consent", s.whatsappConsent)
	r.With(tenantAuth).Post("/channel-bindings/whatsapp/customers", s.whatsappAddCustomer)
	r.With(tenantAuth).Delete("/channel-bindings/whatsapp/customers", s.whatsappRemoveCustomer)
	r.With(tenantAuth).Post("/channel-bindings/whatsapp/init", s.whatsappInit)
	r.With(tenantAuth).Get("/channel-bindings/whatsapp/qr", s.whatsappQR)
	r.With(tenantAuth).Get("/channel-bindings/whatsapp/qr.png", s.whatsappQRPNG)
	r.With(tenantAuth).Get("/channel-bindings/whatsapp/status", s.whatsappStatus)
	r.With(tenantAuth).Delete("/channel-bindings/whatsapp", s.whatsappLogout)
	return r
}

func (s *Server) whatsappInit(w http.ResponseWriter, r *http.Request) {
	if s.whatsapp == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "whatsapp_disabled"})
		return
	}
	tenantID := tenantFromContext(r.Context())
	binding, err := s.bindingForTenant(r.Context(), tenantID, channel.ChannelWhatsApp)
	if err != nil {
		writeError(w, err)
		return
	}
	if binding.ConsentAcknowledgedAt == nil {
		writeError(w, domain.ErrConsentRequired)
		return
	}
	code, err := s.whatsapp.BeginPairing(r.Context(), tenantID)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "pairing_failed", "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"qr":              code,
		"status":          string(s.whatsapp.Status(tenantID)),
		"provider_number": binding.ProviderNumber,
		"png_url":         "/channel-bindings/whatsapp/qr.png",
	})
}

func (s *Server) whatsappConsent(w http.ResponseWriter, r *http.Request) {
	var req app.AcknowledgeWhatsAppConsentInput
	if !decodeJSON(w, r, &req) {
		return
	}
	tenantID := tenantFromContext(r.Context())
	if err := s.app.AcknowledgeWhatsAppConsent(r.Context(), tenantID, req); err != nil {
		writeError(w, err)
		return
	}
	binding, err := s.bindingForTenant(r.Context(), tenantID, channel.ChannelWhatsApp)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"binding": binding})
}

func (s *Server) whatsappAddCustomer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Customer string `json:"customer"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	tenantID := tenantFromContext(r.Context())
	if err := s.app.AddWhatsAppCustomer(r.Context(), tenantID, req.Customer); err != nil {
		writeError(w, err)
		return
	}
	customers, err := s.app.ListWhatsAppCustomers(r.Context(), tenantID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"customers": customers})
}

func (s *Server) whatsappRemoveCustomer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Customer string `json:"customer"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	tenantID := tenantFromContext(r.Context())
	if err := s.app.RemoveWhatsAppCustomer(r.Context(), tenantID, req.Customer); err != nil {
		writeError(w, err)
		return
	}
	customers, err := s.app.ListWhatsAppCustomers(r.Context(), tenantID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"customers": customers})
}

func (s *Server) whatsappQR(w http.ResponseWriter, r *http.Request) {
	if s.whatsapp == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "whatsapp_disabled"})
		return
	}
	tenantID := tenantFromContext(r.Context())
	code := s.whatsapp.CurrentQR(tenantID)
	if code == "" {
		writeJSON(w, http.StatusGone, map[string]any{"error": "no_active_qr", "status": string(s.whatsapp.Status(tenantID))})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"qr": code, "status": string(s.whatsapp.Status(tenantID))})
}

func (s *Server) whatsappQRPNG(w http.ResponseWriter, r *http.Request) {
	if s.whatsapp == nil {
		http.Error(w, "whatsapp_disabled", http.StatusServiceUnavailable)
		return
	}
	tenantID := tenantFromContext(r.Context())
	code := s.whatsapp.CurrentQR(tenantID)
	if code == "" {
		http.Error(w, "no_active_qr", http.StatusGone)
		return
	}
	png, err := qrcode.Encode(code, qrcode.Medium, 320)
	if err != nil {
		http.Error(w, "qr_encode_failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}

func (s *Server) whatsappStatus(w http.ResponseWriter, r *http.Request) {
	tenantID := tenantFromContext(r.Context())
	binding, err := s.bindingForTenant(r.Context(), tenantID, channel.ChannelWhatsApp)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		writeError(w, err)
		return
	}
	status := domain.SessionUnknown
	if s.whatsapp != nil {
		status = s.whatsapp.Status(tenantID)
	}
	if status == domain.SessionUnknown && binding.SessionStatus != "" {
		status = binding.SessionStatus
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":          string(status),
		"provider_number": binding.ProviderNumber,
		"updated_at":      binding.SessionUpdated,
	})
}

func (s *Server) whatsappLogout(w http.ResponseWriter, r *http.Request) {
	if s.whatsapp == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "whatsapp_disabled"})
		return
	}
	tenantID := tenantFromContext(r.Context())
	if err := s.whatsapp.Logout(r.Context(), tenantID); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) bindingForTenant(ctx context.Context, tenantID string, ch channel.Channel) (domain.ChannelBinding, error) {
	return s.app.GetChannelBinding(ctx, tenantID, string(ch))
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) provisionTenant(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name           string `json:"name"`
		Vertical       string `json:"vertical"`
		ProviderNumber string `json:"provider_number"`
		OperatorID     string `json:"operator_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	tenant, manifest, err := s.app.ProvisionTenant(r.Context(), req.Name, req.Vertical, req.ProviderNumber, req.OperatorID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"tenant": tenant, "manifest": manifest})
}

func (s *Server) startCase(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WorkflowSlug string         `json:"workflow_slug"`
		Mode         string         `json:"mode"`
		Payload      map[string]any `json:"payload"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	run, packet, err := s.app.StartCase(r.Context(), app.StartCaseInput{
		TenantID:     tenantFromContext(r.Context()),
		WorkflowSlug: req.WorkflowSlug,
		Mode:         domain.Mode(req.Mode),
		Payload:      req.Payload,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"case_run": run, "review_packet": packet})
}

func (s *Server) ingestCustomerMessage(w http.ResponseWriter, r *http.Request) {
	var req app.IngestionEvent
	if !decodeJSON(w, r, &req) {
		return
	}
	req.TenantID = tenantFromContext(r.Context())
	caseRunID, err := s.app.IngestCustomerMessage(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"case_run_id": caseRunID})
}

func (s *Server) gatewayInbound(w http.ResponseWriter, r *http.Request) {
	var req app.InboundReply
	if !decodeJSON(w, r, &req) {
		return
	}
	envelope, err := s.app.ReceiveOperatorReply(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"signal": envelope, "signal_name": envelope.ActionButton.SignalName()})
}

func (s *Server) listCandidates(w http.ResponseWriter, r *http.Request) {
	candidates, err := s.app.ListCandidates(r.Context(), tenantFromContext(r.Context()))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidates": candidates})
}

func (s *Server) promoteCandidate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ReviewerID string `json:"reviewer_id"`
		Rationale  string `json:"rationale"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	rule, sv, err := s.app.PromoteCandidate(r.Context(), chi.URLParam(r, "tenant_id"), chi.URLParam(r, "candidate_id"), req.ReviewerID, req.Rationale)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"validated_rule": rule, "skill_version": sv})
}

func (s *Server) activeSkillVersion(w http.ResponseWriter, r *http.Request) {
	workflowSlug := r.URL.Query().Get("workflow_slug")
	if workflowSlug == "" {
		workflowSlug = "quote_drafting"
	}
	sv, err := s.app.LoadSkillVersion(r.Context(), tenantFromContext(r.Context()), workflowSlug)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"skill_version": sv})
}

func (s *Server) replayCase(w http.ResponseWriter, r *http.Request) {
	var req app.ReplayInput
	if !decodeJSON(w, r, &req) {
		return
	}
	run, err := s.app.ReplayCase(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"case_run": run})
}

func (s *Server) mcpTools(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"tools": s.app.MCPListTools(domain.Mode(r.URL.Query().Get("mode")))})
}

func (s *Server) mcpWriteFinal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ServerMode string            `json:"server_mode"`
		Request    domain.MCPRequest `json:"request"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	boundTenantID := tenantFromContext(r.Context())
	result, err := s.app.CallMCPWriteFinal(r.Context(), boundTenantID, domain.Mode(req.ServerMode), req.Request)
	if err != nil {
		writeErrorWithPayload(w, err, result)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json", "message": err.Error()})
		return false
	}
	return true
}

func writeError(w http.ResponseWriter, err error) {
	writeErrorWithPayload(w, err, nil)
}

func writeErrorWithPayload(w http.ResponseWriter, err error, payload any) {
	status := http.StatusInternalServerError
	code := "internal_error"
	switch {
	case errors.Is(err, domain.ErrInvalidInput):
		status, code = http.StatusBadRequest, "invalid_input"
	case errors.Is(err, domain.ErrSandboxContamination):
		status, code = http.StatusBadRequest, "sandbox_contamination"
	case errors.Is(err, domain.ErrNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, domain.ErrTenantMismatch), errors.Is(err, domain.ErrSecurityViolation):
		status, code = http.StatusForbidden, "security_violation"
	case errors.Is(err, domain.ErrSandboxMode):
		status, code = http.StatusForbidden, "sandbox_mode"
	case errors.Is(err, domain.ErrApprovalRequired):
		status, code = http.StatusForbidden, "approval_required"
	case errors.Is(err, domain.ErrExpired):
		status, code = http.StatusGone, "expired"
	case errors.Is(err, domain.ErrConsentRequired):
		status, code = http.StatusForbidden, "consent_required"
	}
	body := map[string]any{"error": code, "message": err.Error()}
	if payload != nil {
		body["result"] = payload
	}
	writeJSON(w, status, body)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func tenantAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		const prefix = "Bearer tid:"
		if !strings.HasPrefix(auth, prefix) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		tenantID := strings.TrimSpace(strings.TrimPrefix(auth, prefix))
		if tenantID == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r.WithContext(contextWithTenant(r.Context(), tenantID)))
	})
}
