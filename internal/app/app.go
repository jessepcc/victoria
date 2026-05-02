package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"victoria/internal/channel"
	"victoria/internal/domain"
)

type App struct {
	store Store
	ids   IDGenerator
	clock Clock

	gateway *Gateway
}

func New(store Store, opts ...Option) *App {
	a := &App{
		store: store,
		ids:   RandomIDs{},
		clock: SystemClock{},
	}
	for _, opt := range opts {
		opt(a)
	}
	a.gateway = NewGateway(store, a.ids, a.clock)
	return a
}

type Option func(*App)

func WithIDs(ids IDGenerator) Option {
	return func(a *App) { a.ids = ids }
}

func WithClock(clock Clock) Option {
	return func(a *App) { a.clock = clock }
}

func (a *App) ProvisionTenant(ctx context.Context, name, vertical, providerNumber, operatorID string) (domain.Tenant, domain.ProvisioningManifest, error) {
	now := a.clock.Now()
	tenant := domain.Tenant{
		ID:          a.ids.NewID("t"),
		Name:        strings.TrimSpace(name),
		Vertical:    strings.TrimSpace(vertical),
		Status:      "active",
		DefaultMode: domain.ModeSandbox,
		CreatedAt:   now,
	}
	if tenant.Name == "" || tenant.Vertical == "" || providerNumber == "" || operatorID == "" {
		return domain.Tenant{}, domain.ProvisioningManifest{}, domain.ErrInvalidInput
	}

	for _, tmpl := range defaultWorkflowTemplates(tenant.Vertical) {
		if err := a.store.UpsertWorkflowTemplate(ctx, tmpl); err != nil {
			return domain.Tenant{}, domain.ProvisioningManifest{}, err
		}
	}

	initialSV := domain.SkillVersion{
		ID:           a.ids.NewID("sv"),
		TenantID:     tenant.ID,
		WorkflowSlug: "quote_drafting",
		Version:      1,
		RuleManifest: nil,
		Status:       "active",
		CreatedAt:    now,
	}
	manifest := domain.ProvisioningManifest{
		TenantID:          tenant.ID,
		HermesVersion:     "0.3.4",
		Mode:              domain.ModeSandbox,
		WorkflowTemplates: []string{"enquiry_triage", "quote_drafting", "invoice_handling"},
		MCPEndpoints: map[string]string{
			"sandbox_email":   "http://localhost:3001",
			"sandbox_drive":   "http://localhost:3002",
			"sandbox_invoice": "http://localhost:3003",
		},
		SkillVersionEndpoint: "/internal/skill-versions/active",
		Vertical:             tenant.Vertical,
	}
	binding := domain.ChannelBinding{
		TenantID:       tenant.ID,
		Channel:        "telegram",
		ProviderNumber: providerNumber,
		OperatorID:     operatorID,
		SessionStatus:  domain.SessionActive,
		SessionUpdated: now,
	}
	if err := a.store.CreateTenant(ctx, tenant, manifest, binding, initialSV); err != nil {
		return domain.Tenant{}, domain.ProvisioningManifest{}, err
	}
	// Pre-create the WhatsApp binding so the operator can pair from day 1.
	// Status starts as qr_needed; pairing transitions it to connecting → active.
	waBinding := domain.ChannelBinding{
		TenantID:         tenant.ID,
		Channel:          "whatsapp",
		ProviderNumber:   providerNumber,
		OperatorID:       operatorID,
		SessionStatus:    domain.SessionQRNeeded,
		SessionUpdated:   now,
		InboundMode:      domain.InboundModeReadOnly,
		RetentionMinutes: 30,
		OperatorJID:      normalizeWhatsAppJID(providerNumber),
	}
	if err := a.store.UpsertChannelBinding(ctx, waBinding); err != nil {
		return domain.Tenant{}, domain.ProvisioningManifest{}, err
	}
	for _, workflowSlug := range []string{"enquiry_triage", "invoice_handling"} {
		if err := a.store.CreateSkillVersion(ctx, domain.SkillVersion{
			ID:           a.ids.NewID("sv"),
			TenantID:     tenant.ID,
			WorkflowSlug: workflowSlug,
			Version:      1,
			RuleManifest: nil,
			Status:       "active",
			CreatedAt:    now,
		}); err != nil {
			return domain.Tenant{}, domain.ProvisioningManifest{}, err
		}
	}
	_, err := a.store.CreateAuditEvent(ctx, auditEvent(a.ids, now, tenant.ID, "tenant_provisioned", "admin", "system", "tenant", tenant.ID, nil, map[string]any{
		"vertical": tenant.Vertical,
	}, "tenant-provisioned", tenant.ID))
	if err != nil {
		return domain.Tenant{}, domain.ProvisioningManifest{}, err
	}
	return tenant, manifest, nil
}

type StartCaseInput struct {
	TenantID     string         `json:"tenant_id"`
	WorkflowSlug string         `json:"workflow_slug"`
	Mode         domain.Mode    `json:"mode"`
	Payload      map[string]any `json:"payload"`
}

type IngestionEvent struct {
	TenantID           string         `json:"tenant_id,omitempty"`
	Channel            string         `json:"channel"`
	SourceMessageID    string         `json:"source_message_id"`
	CustomerIdentifier string         `json:"customer_identifier"`
	ReceivedAt         time.Time      `json:"received_at"`
	Subject            string         `json:"subject,omitempty"`
	BodyText           string         `json:"body_text"`
	Metadata           map[string]any `json:"metadata,omitempty"`
}

type AcknowledgeWhatsAppConsentInput struct {
	InboundMode      domain.InboundMode `json:"inbound_mode"`
	DraftDeliveryJID string             `json:"draft_delivery_jid,omitempty"`
	OperatorJID      string             `json:"operator_jid,omitempty"`
}

func (a *App) AcknowledgeWhatsAppConsent(ctx context.Context, tenantID string, input AcknowledgeWhatsAppConsentInput) error {
	binding, err := a.store.ChannelBindingByTenant(ctx, tenantID, string(channel.ChannelWhatsApp))
	if err != nil {
		return err
	}
	mode := input.InboundMode
	if mode == "" {
		mode = domain.InboundModeReadOnly
	}
	if !mode.Valid() {
		return domain.ErrInvalidInput
	}
	now := a.clock.Now().UTC()
	binding.InboundMode = mode
	binding.ConsentAcknowledgedAt = &now
	if binding.RetentionMinutes <= 0 {
		binding.RetentionMinutes = 30
	}
	if input.OperatorJID != "" {
		binding.OperatorJID = normalizeWhatsAppJID(input.OperatorJID)
	}
	if binding.OperatorJID == "" {
		binding.OperatorJID = normalizeWhatsAppJID(binding.ProviderNumber)
	}
	if input.DraftDeliveryJID != "" {
		binding.DraftDeliveryJID = normalizeWhatsAppJID(input.DraftDeliveryJID)
	}
	if binding.DraftDeliveryJID == "" && mode == domain.InboundModeReadOnly {
		binding.DraftDeliveryJID = binding.OperatorJID
	}
	binding.SessionUpdated = now
	if err := a.store.UpsertChannelBinding(ctx, binding); err != nil {
		return err
	}
	_, err = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, now, tenantID, "whatsapp_consent_acknowledged", "operator", binding.OperatorID, "channel_binding", string(channel.ChannelWhatsApp), nil, map[string]any{
		"inbound_mode": string(mode),
	}, "wa-consent", tenantID, string(mode), now.Format(time.RFC3339Nano)))
	return err
}

func (a *App) AddWhatsAppCustomer(ctx context.Context, tenantID, customer string) error {
	binding, err := a.store.ChannelBindingByTenant(ctx, tenantID, string(channel.ChannelWhatsApp))
	if err != nil {
		return err
	}
	jid := normalizeWhatsAppJID(customer)
	if jid == "" {
		return domain.ErrInvalidInput
	}
	binding.CustomerAllowlist = appendIfMissing(binding.CustomerAllowlist, jid)
	binding.SessionUpdated = a.clock.Now().UTC()
	return a.store.UpsertChannelBinding(ctx, binding)
}

func (a *App) RemoveWhatsAppCustomer(ctx context.Context, tenantID, customer string) error {
	binding, err := a.store.ChannelBindingByTenant(ctx, tenantID, string(channel.ChannelWhatsApp))
	if err != nil {
		return err
	}
	jid := normalizeWhatsAppJID(customer)
	if jid == "" {
		return domain.ErrInvalidInput
	}
	binding.CustomerAllowlist = removeString(binding.CustomerAllowlist, jid)
	binding.SessionUpdated = a.clock.Now().UTC()
	return a.store.UpsertChannelBinding(ctx, binding)
}

func (a *App) ListWhatsAppCustomers(ctx context.Context, tenantID string) ([]string, error) {
	binding, err := a.store.ChannelBindingByTenant(ctx, tenantID, string(channel.ChannelWhatsApp))
	if err != nil {
		return nil, err
	}
	out := append([]string(nil), binding.CustomerAllowlist...)
	sort.Strings(out)
	return out, nil
}

func (a *App) RegisterWhatsAppCommandIdentity(ctx context.Context, tenantID, jid string) error {
	binding, err := a.store.ChannelBindingByTenant(ctx, tenantID, string(channel.ChannelWhatsApp))
	if err != nil {
		return err
	}
	normalized := normalizeWhatsAppJID(jid)
	if normalized == "" {
		return domain.ErrInvalidInput
	}
	binding.CommandIdentities = appendIfMissing(binding.CommandIdentities, normalized)
	binding.SessionUpdated = a.clock.Now().UTC()
	return a.store.UpsertChannelBinding(ctx, binding)
}

func (a *App) RecordOutboundBlocked(ctx context.Context, tenantID, dstJID, bodyHash, callSite string) error {
	_, err := a.store.CreateAuditEvent(ctx, auditEvent(a.ids, a.clock.Now(), tenantID, "outbound_blocked_to_customer", "system", "whatsapp", "channel_binding", string(channel.ChannelWhatsApp), nil, map[string]any{
		"dst_jid":         dstJID,
		"body_hash":       bodyHash,
		"call_site_stack": callSite,
	}, "outbound-blocked-to-customer", tenantID, dstJID, bodyHash, a.clock.Now().Format(time.RFC3339Nano)))
	return err
}

func (a *App) IngestCustomerMessage(ctx context.Context, event IngestionEvent) (string, error) {
	event.TenantID = strings.TrimSpace(event.TenantID)
	event.Channel = strings.TrimSpace(event.Channel)
	event.SourceMessageID = strings.TrimSpace(event.SourceMessageID)
	event.CustomerIdentifier = strings.TrimSpace(event.CustomerIdentifier)
	event.BodyText = strings.TrimSpace(event.BodyText)
	if event.TenantID == "" || event.Channel == "" || event.SourceMessageID == "" || event.CustomerIdentifier == "" || event.BodyText == "" {
		return "", domain.ErrInvalidInput
	}
	if event.ReceivedAt.IsZero() {
		event.ReceivedAt = a.clock.Now()
	}
	if _, err := a.store.GetTenant(ctx, event.TenantID); err != nil {
		return "", err
	}
	msg := domain.CustomerMessage{
		ID:                 a.ids.NewID("cm"),
		TenantID:           event.TenantID,
		Channel:            event.Channel,
		SourceMessageID:    event.SourceMessageID,
		CustomerIdentifier: event.CustomerIdentifier,
		ReceivedAt:         event.ReceivedAt.UTC(),
		Subject:            event.Subject,
		BodyText:           event.BodyText,
		Metadata:           cloneMap(event.Metadata),
		Status:             "ingested",
	}
	created, stored, err := a.store.CreateCustomerMessage(ctx, msg)
	if err != nil {
		return "", err
	}
	if !created {
		return stored.CaseRunID, nil
	}
	run, _, err := a.StartCase(ctx, StartCaseInput{
		TenantID:     event.TenantID,
		WorkflowSlug: "enquiry_triage",
		Mode:         domain.ModeSandbox,
		Payload:      payloadFromIngestionEvent(event),
	})
	if err != nil {
		_ = a.store.UpdateCustomerMessageCase(ctx, event.TenantID, event.Channel, event.SourceMessageID, "", "failed")
		return "", err
	}
	if err := a.store.UpdateCustomerMessageCase(ctx, event.TenantID, event.Channel, event.SourceMessageID, run.ID, "ingested"); err != nil {
		return "", err
	}
	_, err = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, a.clock.Now(), event.TenantID, "customer_message_received", "customer", event.CustomerIdentifier, "customer_message", msg.ID, map[string]any{
		"case_run_id": run.ID,
	}, map[string]any{
		"channel":             event.Channel,
		"source_message_id":   event.SourceMessageID,
		"customer_identifier": event.CustomerIdentifier,
	}, "customer-message-received", event.TenantID, event.Channel, event.SourceMessageID))
	if err != nil {
		return "", err
	}
	return run.ID, nil
}

func (a *App) StartCase(ctx context.Context, input StartCaseInput) (domain.CaseRun, domain.ReviewPacket, error) {
	if input.TenantID == "" || input.WorkflowSlug == "" || !input.Mode.ValidForWorkflowInput() {
		return domain.CaseRun{}, domain.ReviewPacket{}, domain.ErrInvalidInput
	}
	if input.Mode == domain.ModeSandbox && !boolValue(input.Payload, "sandbox") {
		return domain.CaseRun{}, domain.ReviewPacket{}, domain.ErrSandboxContamination
	}
	if _, err := a.store.GetTenant(ctx, input.TenantID); err != nil {
		return domain.CaseRun{}, domain.ReviewPacket{}, err
	}
	if _, err := a.store.GetWorkflowTemplate(ctx, input.WorkflowSlug); err != nil {
		return domain.CaseRun{}, domain.ReviewPacket{}, err
	}

	now := a.clock.Now()
	inputHash, err := domain.JSONHash(input.Payload)
	if err != nil {
		return domain.CaseRun{}, domain.ReviewPacket{}, err
	}
	sv, err := a.store.ActiveSkillVersion(ctx, input.TenantID, input.WorkflowSlug)
	if err != nil {
		return domain.CaseRun{}, domain.ReviewPacket{}, err
	}
	caseRun := domain.CaseRun{
		ID:                a.ids.NewID("cr"),
		TenantID:          input.TenantID,
		WorkflowSlug:      input.WorkflowSlug,
		Mode:              input.Mode,
		InputPayload:      cloneMap(input.Payload),
		InputHash:         inputHash,
		SkillVersionID:    sv.ID,
		Status:            "waiting_for_approval",
		CreatedAt:         now,
		ReplayTemperature: 0.2,
	}
	decision := a.evaluateDecision(now, caseRun, input.Payload, sv)
	caseRun.DecisionPointID = decision.ID
	artifact := a.buildArtifact(now, caseRun, decision)
	packet := a.buildReviewPacket(now, caseRun, decision, artifact)

	if err := a.store.CreateCaseRun(ctx, caseRun); err != nil {
		return domain.CaseRun{}, domain.ReviewPacket{}, err
	}
	if err := a.store.CreateDecisionPoint(ctx, decision); err != nil {
		return domain.CaseRun{}, domain.ReviewPacket{}, err
	}
	if err := a.store.CreateArtifact(ctx, artifact); err != nil {
		return domain.CaseRun{}, domain.ReviewPacket{}, err
	}
	snapshot := artifact
	snapshot.ID = a.ids.NewID("art")
	snapshot.ArtifactType = "mcp_read_snapshot"
	snapshot.StoragePath = fmt.Sprintf("/%s/sandbox/%s/snapshots/input.json", caseRun.TenantID, caseRun.ID)
	snapshot.Content = cloneMap(input.Payload)
	if err := a.store.CreateArtifact(ctx, snapshot); err != nil {
		return domain.CaseRun{}, domain.ReviewPacket{}, err
	}
	if err := a.gateway.SendApprovalPacket(ctx, packet); err != nil {
		return domain.CaseRun{}, domain.ReviewPacket{}, err
	}
	return caseRun, packet, nil
}

type ReplayInput struct {
	TenantID       string `json:"tenant_id"`
	CaseRunID      string `json:"case_run_id"`
	SkillVersionID string `json:"skill_version_id,omitempty"`
	ReplayRunID    string `json:"replay_run_id,omitempty"`
	ForceCurrentSV bool   `json:"force_current_skill_version,omitempty"`
}

func (a *App) ReplayCase(ctx context.Context, input ReplayInput) (domain.CaseRun, error) {
	original, err := a.store.GetCaseRun(ctx, input.TenantID, input.CaseRunID)
	if err != nil {
		return domain.CaseRun{}, err
	}
	var sv domain.SkillVersion
	switch {
	case input.SkillVersionID != "":
		sv, err = a.store.GetSkillVersion(ctx, input.TenantID, input.SkillVersionID)
	case input.ForceCurrentSV:
		sv, err = a.store.ActiveSkillVersion(ctx, input.TenantID, original.WorkflowSlug)
	default:
		sv, err = a.store.GetSkillVersion(ctx, input.TenantID, original.SkillVersionID)
	}
	if err != nil {
		return domain.CaseRun{}, err
	}
	now := a.clock.Now()
	replayPayload := a.snapshotPayload(ctx, original)
	replay := domain.CaseRun{
		ID:                a.ids.NewID("cr"),
		TenantID:          original.TenantID,
		WorkflowSlug:      original.WorkflowSlug,
		Mode:              original.Mode,
		InputPayload:      replayPayload,
		InputHash:         original.InputHash,
		SkillVersionID:    sv.ID,
		ReplayedFromID:    original.ID,
		Status:            "replayed",
		CreatedAt:         now,
		ReplayTemperature: 0,
	}
	decision := a.evaluateDecision(now, replay, replay.InputPayload, sv)
	replay.DecisionPointID = decision.ID
	decision.Status = "replayed"
	if err := a.store.CreateCaseRun(ctx, replay); err != nil {
		return domain.CaseRun{}, err
	}
	if err := a.store.CreateDecisionPoint(ctx, decision); err != nil {
		return domain.CaseRun{}, err
	}
	return replay, nil
}

func (a *App) snapshotPayload(ctx context.Context, original domain.CaseRun) map[string]any {
	artifacts, err := a.store.ListArtifacts(ctx, original.TenantID, original.ID)
	if err == nil {
		for _, artifact := range artifacts {
			if artifact.ArtifactType == "mcp_read_snapshot" {
				return cloneMap(artifact.Content)
			}
		}
	}
	return cloneMap(original.InputPayload)
}

func (a *App) ReceiveOperatorReply(ctx context.Context, input InboundReply) (domain.ApprovalSignalEnvelope, error) {
	return a.gateway.ReceiveInbound(ctx, input, a.persistSignal)
}

func (a *App) DisconnectGateway(ctx context.Context, tenantID string) {
	a.gateway.Disconnect(ctx, tenantID)
}

func (a *App) RecoverGateway(ctx context.Context, tenantID string) []domain.ReviewPacket {
	return a.gateway.Recover(ctx, tenantID)
}

// RegisterChannelAdapter installs an outbound adapter (WhatsApp / Telegram).
// Safe to call once at startup before the HTTP server begins accepting.
func (a *App) RegisterChannelAdapter(adapter channel.Adapter) {
	a.gateway.RegisterAdapter(adapter)
}

// GetChannelBinding returns the binding for a tenant on a specific channel.
func (a *App) GetChannelBinding(ctx context.Context, tenantID, ch string) (domain.ChannelBinding, error) {
	return a.store.ChannelBindingByTenant(ctx, tenantID, ch)
}

// HandleWhatsAppInbound translates an inbound WhatsApp message (already
// normalized by the whatsmeow Manager) into the gateway's correction loop.
//
// Two-state conversation:
//  1. If the gateway has a pending follow-up for this tenant (we previously
//     asked "what should I have done?"), the current message is the
//     correction reasoning — log it as a wrong_action correction and clear
//     the follow-up state.
//  2. Otherwise classify intent: yes/no/other. "no" without an inline
//     reason starts a follow-up turn. "no with reason" or other substantive
//     text becomes a correction immediately. "yes" approves; any text
//     after "yes" rides along as a free-text note on the envelope.
func (a *App) HandleWhatsAppInbound(ctx context.Context, tenantID string, msg channel.InboundMessage) error {
	binding, err := a.store.ChannelBindingByTenant(ctx, tenantID, string(channel.ChannelWhatsApp))
	if err != nil {
		return fmt.Errorf("inbound: lookup binding: %w", err)
	}
	mode := domain.ParseInboundMode(string(binding.InboundMode))
	senderJID := normalizeInboundJID(msg)
	if handled, err := a.handleWhatsAppCommand(ctx, tenantID, binding, msg, senderJID); handled || err != nil {
		return err
	}
	switch mode {
	case domain.InboundModeReadOnly:
		if !msg.IsFromMe && !contains(binding.CustomerAllowlist, senderJID) {
			_, _ = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, a.clock.Now(), tenantID, "wa_inbound_ignored", "customer", senderJID, "message", msg.ProviderMessageID, nil, map[string]any{
				"channel": string(channel.ChannelWhatsApp),
				"kind":    "ignored",
			}, "wa-ignored", tenantID, msg.ProviderMessageID))
			return nil
		}
		if !msg.IsFromMe {
			return a.routeWhatsAppCustomerMessage(ctx, tenantID, binding, msg, senderJID, "whatsapp_a0")
		}
	case domain.InboundModeFullControl:
		if msg.IsFromMe {
			return nil
		}
		if !contains(binding.CommandIdentities, senderJID) {
			return a.routeWhatsAppCustomerMessage(ctx, tenantID, binding, msg, senderJID, "whatsapp_a1")
		}
	}
	packetID := extractPacketReference(msg.FreeText)
	if packetID == "" {
		if latest, err := a.store.LatestReviewPacket(ctx, tenantID); err == nil {
			packetID = latest.PacketID
		}
	}

	// Path 1: a pending follow-up turn — current text is the correction reason.
	if pendingPacket, ok := a.gateway.ConsumePendingFollowup(tenantID); ok {
		if pendingPacket != "" {
			packetID = pendingPacket
		}
		return a.dispatchInbound(ctx, tenantID, binding, packetID, msg, domain.ActionWrongAction, msg.FreeText)
	}

	intent := classifyIntent(msg.FreeText)
	switch intent.kind {
	case "approve":
		return a.dispatchInbound(ctx, tenantID, binding, packetID, msg, domain.ActionApprove, intent.note)
	case "reject_with_reason":
		return a.dispatchInbound(ctx, tenantID, binding, packetID, msg, domain.ActionWrongAction, intent.note)
	case "reject_need_followup":
		// Stash the packet id so the next message is interpreted as the reason.
		a.gateway.SetPendingFollowup(tenantID, packetID)
		a.gateway.SendNotification(ctx, tenantID, "Got it — what should I have done instead? Just type the reason.")
		return nil
	case "promote":
		return a.handlePromoteCommand(ctx, tenantID, binding.OperatorID)
	case "correction":
		// Power-user path: substantive prose without an explicit yes/no marker.
		return a.dispatchInbound(ctx, tenantID, binding, packetID, msg, domain.ActionWrongAction, intent.note)
	}

	// Empty / unparseable.
	_, _ = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, a.clock.Now(), tenantID, "correction_dead_lettered", "operator", binding.OperatorID, "message", msg.ProviderMessageID, nil, map[string]any{
		"channel":   string(channel.ChannelWhatsApp),
		"packet_id": packetID,
		"free_text": msg.FreeText,
		"sender":    msg.SenderIdentifier,
	}, "deadletter", tenantID, msg.ProviderMessageID))
	return nil
}

func (a *App) HandleTelegramInbound(ctx context.Context, tenantID string, msg channel.InboundMessage) error {
	binding, err := a.store.ChannelBindingByTenant(ctx, tenantID, string(channel.ChannelTelegram))
	if err != nil {
		return err
	}
	if contains(binding.TelegramCustomerChats, msg.SenderIdentifier) {
		_, err := a.IngestCustomerMessage(ctx, IngestionEvent{
			TenantID:           tenantID,
			Channel:            "telegram",
			SourceMessageID:    msg.ProviderMessageID,
			CustomerIdentifier: msg.SenderIdentifier,
			ReceivedAt:         msg.ReceivedAt,
			BodyText:           msg.FreeText,
		})
		return err
	}
	packetID := extractPacketReference(msg.FreeText)
	if packetID == "" {
		if latest, err := a.store.LatestReviewPacket(ctx, tenantID); err == nil {
			packetID = latest.PacketID
		}
	}
	intent := classifyIntent(msg.FreeText)
	switch intent.kind {
	case "approve":
		return a.dispatchInbound(ctx, tenantID, binding, packetID, msg, domain.ActionApprove, intent.note)
	case "reject_with_reason":
		return a.dispatchInbound(ctx, tenantID, binding, packetID, msg, domain.ActionWrongAction, intent.note)
	case "reject_need_followup":
		a.gateway.SetPendingFollowup(tenantID, packetID)
		a.gateway.SendNotification(ctx, tenantID, "Got it — what should I have done instead? Just type the reason.")
		return nil
	case "promote":
		return a.handlePromoteCommand(ctx, tenantID, binding.OperatorID)
	case "correction":
		return a.dispatchInbound(ctx, tenantID, binding, packetID, msg, domain.ActionWrongAction, intent.note)
	default:
		return nil
	}
}

func (a *App) handleWhatsAppCommand(ctx context.Context, tenantID string, binding domain.ChannelBinding, msg channel.InboundMessage, senderJID string) (bool, error) {
	clean := strings.TrimSpace(msg.FreeText)
	lower := strings.ToLower(clean)
	isOperator := msg.IsFromMe || contains(binding.CommandIdentities, senderJID)
	switch {
	case strings.HasPrefix(lower, "register me as operator "):
		secret := strings.TrimSpace(clean[len("register me as operator "):])
		if binding.CommandRegistrationSecret == "" || binding.CommandSecretConsumedAt != nil || secret != binding.CommandRegistrationSecret {
			_, _ = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, a.clock.Now(), tenantID, "command_registration_rejected", "operator", senderJID, "channel_binding", string(channel.ChannelWhatsApp), nil, map[string]any{
				"reason": "invalid_secret",
			}, "command-registration-rejected", tenantID, senderJID, msg.ProviderMessageID))
			a.gateway.SendNotification(ctx, tenantID, "unknown command")
			return true, nil
		}
		now := a.clock.Now().UTC()
		binding.CommandIdentities = appendIfMissing(binding.CommandIdentities, senderJID)
		binding.CommandSecretConsumedAt = &now
		binding.SessionUpdated = now
		if err := a.store.UpsertChannelBinding(ctx, binding); err != nil {
			return true, err
		}
		a.gateway.SendNotification(ctx, tenantID, "✓ registered as operator")
		return true, nil
	case !isOperator:
		return false, nil
	case strings.HasPrefix(lower, "add customer "):
		customer := strings.TrimSpace(clean[len("add customer "):])
		if err := a.AddWhatsAppCustomer(ctx, tenantID, customer); err != nil {
			return true, err
		}
		a.gateway.SendNotification(ctx, tenantID, "✓ added")
		return true, nil
	case strings.HasPrefix(lower, "remove customer "):
		customer := strings.TrimSpace(clean[len("remove customer "):])
		if err := a.RemoveWhatsAppCustomer(ctx, tenantID, customer); err != nil {
			return true, err
		}
		a.gateway.SendNotification(ctx, tenantID, "✓ removed")
		return true, nil
	case lower == "list customers":
		customers, err := a.ListWhatsAppCustomers(ctx, tenantID)
		if err != nil {
			return true, err
		}
		if len(customers) == 0 {
			a.gateway.SendNotification(ctx, tenantID, "No customers are allowlisted.")
		} else {
			a.gateway.SendNotification(ctx, tenantID, strings.Join(customers, "\n"))
		}
		return true, nil
	case lower == "pause":
		until := a.clock.Now().UTC().Add(24 * time.Hour)
		binding.CustomerIntakePausedUntil = &until
		binding.SessionUpdated = a.clock.Now().UTC()
		if err := a.store.UpsertChannelBinding(ctx, binding); err != nil {
			return true, err
		}
		a.gateway.SendNotification(ctx, tenantID, "✓ paused")
		return true, nil
	case lower == "resume":
		binding.CustomerIntakePausedUntil = nil
		binding.SessionUpdated = a.clock.Now().UTC()
		if err := a.store.UpsertChannelBinding(ctx, binding); err != nil {
			return true, err
		}
		a.gateway.SendNotification(ctx, tenantID, "✓ resumed")
		return true, nil
	default:
		return false, nil
	}
}

func (a *App) routeWhatsAppCustomerMessage(ctx context.Context, tenantID string, binding domain.ChannelBinding, msg channel.InboundMessage, senderJID string, ingestChannel string) error {
	if binding.CustomerIntakePausedUntil != nil && a.clock.Now().Before(*binding.CustomerIntakePausedUntil) {
		_, _ = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, a.clock.Now(), tenantID, "customer_intake_paused", "customer", senderJID, "message", msg.ProviderMessageID, nil, map[string]any{
			"customer_identifier": senderJID,
			"channel":             ingestChannel,
		}, "customer-intake-paused", tenantID, ingestChannel, msg.ProviderMessageID))
		return nil
	}
	_, err := a.IngestCustomerMessage(ctx, IngestionEvent{
		TenantID:           tenantID,
		Channel:            ingestChannel,
		SourceMessageID:    msg.ProviderMessageID,
		CustomerIdentifier: senderJID,
		ReceivedAt:         msg.ReceivedAt,
		BodyText:           msg.FreeText,
	})
	return err
}

// handlePromoteCommand resolves a `promote` reply by promoting the most-
// recent under_review candidate for this tenant. Convenience for demo flows
// — production deployments would route via the Rule Review Console (spec §7).
func (a *App) handlePromoteCommand(ctx context.Context, tenantID, operatorID string) error {
	candidates, err := a.store.ListCandidates(ctx, tenantID)
	if err != nil {
		return err
	}
	var pick domain.RuleCandidate
	for _, c := range candidates {
		if c.Status != "under_review" {
			continue
		}
		if pick.ID == "" || (c.UnderReviewAt != nil && pick.UnderReviewAt != nil && c.UnderReviewAt.After(*pick.UnderReviewAt)) {
			pick = c
		}
	}
	if pick.ID == "" {
		a.gateway.SendNotification(ctx, tenantID, "Nothing is ready for promotion yet — no candidate is under_review.")
		return nil
	}
	rule, sv, err := a.PromoteCandidate(ctx, tenantID, pick.ID, "wa:"+operatorID, "promoted via WhatsApp command")
	if err != nil {
		a.gateway.SendNotification(ctx, tenantID, fmt.Sprintf("Promotion failed: %v", err))
		return err
	}
	a.gateway.SendNotification(ctx, tenantID, fmt.Sprintf(
		"✅ *Rule promoted.*\n\nFrom now on, when this case pattern matches I'll *%s* (instead of the workflow default).\n\nSkillVersion bumped to v%d, %d active rule(s) total.",
		rule.RecommendedAction, sv.Version, len(sv.RuleManifest)))
	return nil
}

func (a *App) dispatchInbound(ctx context.Context, tenantID string, binding domain.ChannelBinding, packetID string, msg channel.InboundMessage, action domain.ActionButton, freeText string) error {
	if !action.Valid() || packetID == "" {
		_, _ = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, a.clock.Now(), tenantID, "correction_dead_lettered", "operator", binding.OperatorID, "message", msg.ProviderMessageID, nil, map[string]any{
			"channel":   string(channel.ChannelWhatsApp),
			"packet_id": packetID,
			"free_text": msg.FreeText,
			"sender":    msg.SenderIdentifier,
			"action":    string(action),
		}, "deadletter", tenantID, msg.ProviderMessageID))
		return nil
	}
	_, err := a.ReceiveOperatorReply(ctx, InboundReply{
		Channel:             string(channel.ChannelWhatsApp),
		ProviderNumber:      binding.ProviderNumber,
		PacketID:            packetID,
		SourceMessageID:     msg.ProviderMessageID,
		RawInboundMessageID: string(channel.ChannelWhatsApp) + ":" + msg.ProviderMessageID,
		ActionButton:        action,
		FreeText:            freeText,
		ReceivedAt:          msg.ReceivedAt,
	})
	return err
}

// intentResult captures the parsed intent of an operator's free-text reply.
type intentResult struct {
	kind string // "approve" | "reject_with_reason" | "reject_need_followup" | "correction" | "promote" | ""
	note string // the operator's payload text (any after-yes / after-no commentary)
}

// classifyIntent reads natural-language replies into a yes/no/other shape.
// Spec §7.1 stage-2 (text-match) parser, narrowed to the binary UX the
// operator prefers (rather than the 6-button enum exposed by spec §3.1).
func classifyIntent(text string) intentResult {
	clean := strings.TrimSpace(text)
	if clean == "" {
		return intentResult{}
	}
	lower := strings.ToLower(clean)
	// Operator command: "promote" (or "promote it", "promote please").
	if lower == "promote" || strings.HasPrefix(lower, "promote ") {
		return intentResult{kind: "promote"}
	}
	if rest, ok := stripPrefix(lower, clean, yesTokens); ok {
		return intentResult{kind: "approve", note: cleanupNote(rest)}
	}
	if rest, ok := stripPrefix(lower, clean, noTokens); ok {
		note := cleanupNote(rest)
		if note == "" {
			return intentResult{kind: "reject_need_followup"}
		}
		return intentResult{kind: "reject_with_reason", note: note}
	}
	// No yes/no marker — treat as a substantive correction.
	return intentResult{kind: "correction", note: clean}
}

var (
	yesTokens = []string{"yes", "yep", "yup", "y", "ok", "okay", "sure", "approve", "approved", "approve.", "lgtm", "looks good", "go ahead", "go", "send", "send it", "confirm", "confirmed", "do it", "1", "👍", "👌", "✅"}
	noTokens  = []string{"no", "nope", "nah", "n", "wrong", "incorrect", "negative", "stop", "hold", "don't", "do not", "2", "👎", "❌"}
)

// stripPrefix returns (textAfterPrefix, true) if `lower` starts with one of
// the tokens (whole-word match), else ("", false). The original `original`
// text is sliced so casing is preserved in the returned remainder.
func stripPrefix(lower, original string, tokens []string) (string, bool) {
	for _, token := range tokens {
		if !strings.HasPrefix(lower, token) {
			continue
		}
		// Whole-word: token must be followed by end-of-string, space, or punctuation.
		if len(lower) == len(token) {
			return "", true
		}
		next := lower[len(token)]
		if next == ' ' || next == ',' || next == '.' || next == '!' || next == '-' || next == ':' || next == ';' || next == '?' {
			return original[len(token):], true
		}
	}
	return "", false
}

func cleanupNote(s string) string {
	clean := strings.TrimSpace(s)
	clean = strings.TrimLeft(clean, ",.:;-! ")
	return strings.TrimSpace(clean)
}

// NotifyChannelSession is the callback the WhatsApp Manager invokes on
// session-state transitions; the gateway updates session_status and drains
// the durable queue if appropriate.
func (a *App) NotifyChannelSession(ctx context.Context, tenantID string, ch channel.Channel, status domain.SessionStatus) {
	a.gateway.NotifySessionStatus(ctx, tenantID, ch, status)
}

func extractPacketReference(text string) string {
	const tag = "[packet:"
	i := strings.Index(text, tag)
	if i < 0 {
		return ""
	}
	rest := text[i+len(tag):]
	end := strings.IndexByte(rest, ']')
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// guessButtonFromText derives an action_button from a free-text reply.
// Real operators type natural language, not "wrong action ..." prefixes, so
// we apply a two-stage decision:
//  1. Numeric (1..6) or short positive ack ("approve" / "ok" / "yes" / "👍")
//     → exact button match.
//  2. Anything else (substantive prose) → ActionWrongAction with the prose
//     preserved as free_text. structuredRuleFromCorrection then mines the
//     content for known patterns (singapore/no gst, photos, send anyway, etc).
//
// This is the spec's Stage 2 (text-match) cascade per operator-ux §7.1.
// Stage 3 (LLM-assisted) is deferred to Phase 2 per RESOLVED-OE-OU-2.
func guessButtonFromText(text string) domain.ActionButton {
	clean := strings.ToLower(strings.TrimSpace(text))
	if clean == "" {
		return ""
	}
	// Direct button selection: digit or exact-prefix label.
	switch {
	case clean == "1":
		return domain.ActionApprove
	case clean == "2":
		return domain.ActionWrongFacts
	case clean == "3":
		return domain.ActionWrongAction
	case clean == "4":
		return domain.ActionMissingCondition
	case clean == "5":
		return domain.ActionUseDifferentTemplate
	case clean == "6":
		return domain.ActionAddNote
	}
	// Short positive acknowledgement → approve.
	if isApprovalText(clean) {
		return domain.ActionApprove
	}
	// Explicit button labels still respected when typed verbatim.
	switch {
	case strings.HasPrefix(clean, "wrong facts"):
		return domain.ActionWrongFacts
	case strings.HasPrefix(clean, "wrong action"):
		return domain.ActionWrongAction
	case strings.HasPrefix(clean, "missing"):
		return domain.ActionMissingCondition
	case strings.HasPrefix(clean, "use different"):
		return domain.ActionUseDifferentTemplate
	case strings.HasPrefix(clean, "add note"):
		return domain.ActionAddNote
	}
	// Any other substantive text is treated as a correction. The actual
	// semantic conversion (text → conditions + recommended_action) happens
	// downstream in structuredRuleFromCorrection during the merge step.
	return domain.ActionWrongAction
}

func isApprovalText(clean string) bool {
	if len(clean) > 32 {
		return false
	}
	switch clean {
	case "approve", "approved", "ok", "okay", "yes", "y", "yep", "yup",
		"sure", "looks good", "lgtm", "go", "go ahead", "send", "send it",
		"confirm", "confirmed", "👍", "👌", "✅":
		return true
	}
	return strings.HasPrefix(clean, "approve")
}

func (a *App) ListCandidates(ctx context.Context, tenantID string) ([]domain.RuleCandidate, error) {
	return a.store.ListCandidates(ctx, tenantID)
}

func (a *App) PromoteCandidate(ctx context.Context, tenantID, candidateID, reviewerID, rationale string) (domain.ValidatedRule, domain.SkillVersion, error) {
	if tenantID == "" || candidateID == "" || reviewerID == "" {
		return domain.ValidatedRule{}, domain.SkillVersion{}, domain.ErrInvalidInput
	}
	candidate, err := a.store.GetCandidate(ctx, tenantID, candidateID)
	if err != nil {
		return domain.ValidatedRule{}, domain.SkillVersion{}, err
	}
	now := a.clock.Now()
	supersedes, version, err := a.store.DeprecateActiveRule(ctx, tenantID, candidate.WorkflowSlug, candidate.DecisionType, candidate.ConditionsHash)
	if err != nil {
		return domain.ValidatedRule{}, domain.SkillVersion{}, err
	}
	rule := domain.ValidatedRule{
		ID:                      a.ids.NewID("vr"),
		TenantID:                tenantID,
		WorkflowSlug:            candidate.WorkflowSlug,
		DecisionType:            candidate.DecisionType,
		ConditionsHash:          candidate.ConditionsHash,
		ConditionsCanonical:     append([]domain.Condition(nil), candidate.ConditionsCanonical...),
		RecommendedAction:       candidate.RecommendedAction,
		Scope:                   candidate.Scope,
		Version:                 version,
		Supersedes:              supersedes,
		PromotedFromCandidateID: candidate.ID,
		PromotedBy:              reviewerID,
		PromotedAt:              now,
		Status:                  "active",
		Rationale:               rationale,
	}
	if err := a.store.CreateValidatedRule(ctx, rule); err != nil {
		return domain.ValidatedRule{}, domain.SkillVersion{}, err
	}
	candidate.Status = "promoted"
	if err := a.store.SaveCandidate(ctx, candidate); err != nil {
		return domain.ValidatedRule{}, domain.SkillVersion{}, err
	}
	_, err = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, now, tenantID, "rule_promoted", "reviewer", reviewerID, "validated_rule", rule.ID, map[string]any{
		"candidate_id": candidate.ID,
	}, map[string]any{
		"scope": string(rule.Scope),
	}, "rule-promoted", tenantID, rule.ID))
	if err != nil {
		return domain.ValidatedRule{}, domain.SkillVersion{}, err
	}
	sv, err := a.createSkillVersion(ctx, tenantID, candidate.WorkflowSlug, now)
	if err != nil {
		return domain.ValidatedRule{}, domain.SkillVersion{}, err
	}
	return rule, sv, nil
}

func (a *App) LoadSkillVersion(ctx context.Context, tenantID, workflowSlug string) (domain.SkillVersion, error) {
	return a.store.ActiveSkillVersion(ctx, tenantID, workflowSlug)
}

type VerticalAggregate struct {
	WorkflowSlug        string             `json:"workflow_slug"`
	DecisionType        string             `json:"decision_type"`
	ConditionsCanonical []domain.Condition `json:"conditions_canonical"`
	RecommendedAction   string             `json:"recommended_action"`
	EvidenceCount       int                `json:"evidence_count"`
	Quarantined         bool               `json:"quarantined"`
}

func (a *App) BuildVerticalAggregates(ctx context.Context, vertical, workflowSlug string) ([]VerticalAggregate, []VerticalAggregate, error) {
	tenants, err := a.store.ListTenants(ctx)
	if err != nil {
		return nil, nil, err
	}
	var safe []VerticalAggregate
	var quarantined []VerticalAggregate
	for _, tenant := range tenants {
		if tenant.Vertical != vertical {
			continue
		}
		candidates, err := a.store.ListCandidates(ctx, tenant.ID)
		if err != nil {
			return nil, nil, err
		}
		for _, candidate := range candidates {
			if candidate.WorkflowSlug != workflowSlug {
				continue
			}
			conditions, quarantine := redactAggregateConditions(candidate.ConditionsCanonical)
			aggregate := VerticalAggregate{
				WorkflowSlug:        candidate.WorkflowSlug,
				DecisionType:        candidate.DecisionType,
				ConditionsCanonical: conditions,
				RecommendedAction:   candidate.RecommendedAction,
				EvidenceCount:       candidate.EvidenceCount,
				Quarantined:         quarantine,
			}
			if quarantine {
				quarantined = append(quarantined, aggregate)
				_, _ = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, a.clock.Now(), tenant.ID, "aggregate_quarantined", "system", "aggregation", "rule_candidate", candidate.ID, nil, map[string]any{
					"workflow_slug": candidate.WorkflowSlug,
				}, "aggregate-quarantine", tenant.ID, candidate.ID))
				continue
			}
			safe = append(safe, aggregate)
		}
	}
	return safe, quarantined, nil
}

func (a *App) CallMCPWriteFinal(ctx context.Context, boundTenantID string, serverMode domain.Mode, req domain.MCPRequest) (domain.MCPResult, error) {
	now := a.clock.Now()
	effectiveServerMode := domain.ParseMode(string(serverMode))
	effectiveRequestMode := domain.ParseMode(string(req.Mode))
	if effectiveServerMode == domain.ModeSandbox || effectiveRequestMode == domain.ModeSandbox {
		_, _ = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, now, boundTenantID, "sandbox_escape_blocked", "system", "mcp", "decision_point", req.DecisionPointID, nil, map[string]any{
			"tool_name": req.ToolName,
		}, "mcp-sandbox", boundTenantID, req.CaseRunID, req.DecisionPointID, req.ToolName))
		return domain.MCPResult{OK: false, Code: "SANDBOX_MODE", Error: domain.ErrSandboxMode.Error()}, domain.ErrSandboxMode
	}
	if req.TenantHeader != boundTenantID {
		_, _ = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, now, boundTenantID, "security_violation", "system", "mcp", "decision_point", req.DecisionPointID, nil, map[string]any{
			"tenant_header": req.TenantHeader,
			"bound_tenant":  boundTenantID,
			"tool_name":     req.ToolName,
		}, "mcp-security", boundTenantID, req.CaseRunID, req.DecisionPointID, req.ToolName))
		return domain.MCPResult{OK: false, Code: "TENANT_MISMATCH", Error: domain.ErrSecurityViolation.Error()}, domain.ErrSecurityViolation
	}
	ok, err := a.store.HasApprovalAudit(ctx, boundTenantID, req.CaseRunID, req.DecisionPointID)
	if err != nil {
		return domain.MCPResult{}, err
	}
	if !ok {
		_, _ = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, now, boundTenantID, "blocked_write_attempted", "system", "mcp", "decision_point", req.DecisionPointID, nil, map[string]any{
			"tool_name": req.ToolName,
		}, "mcp-approval", boundTenantID, req.CaseRunID, req.DecisionPointID, req.ToolName))
		return domain.MCPResult{OK: false, Code: "APPROVAL_REQUIRED", Error: domain.ErrApprovalRequired.Error()}, domain.ErrApprovalRequired
	}
	_, err = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, now, boundTenantID, "mcp_tool_called", "system", "mcp", "decision_point", req.DecisionPointID, nil, map[string]any{
		"tool_name":         req.ToolName,
		"side_effect_class": "write_final",
	}, "mcp-tool", boundTenantID, req.CaseRunID, req.DecisionPointID, req.ToolName, req.IdempotencyKey))
	if err != nil {
		return domain.MCPResult{}, err
	}
	return domain.MCPResult{OK: true}, nil
}

func (a *App) MCPListTools(mode domain.Mode) []string {
	tools := []string{"create_draft_email", "create_invoice_draft", "read_file", "parse_invoice_document"}
	if domain.ParseMode(string(mode)) == domain.ModeLive {
		tools = append(tools, "send_draft_email", "publish_document", "finalize_invoice", "submit_to_accounting")
	}
	sort.Strings(tools)
	return tools
}

func (a *App) persistSignal(ctx context.Context, envelope domain.ApprovalSignalEnvelope) error {
	seen, err := a.store.SeenSignal(ctx, envelope.SignalID)
	if err != nil {
		return err
	}
	if seen {
		return nil
	}
	if err := a.store.MarkSignalSeen(ctx, envelope.SignalID); err != nil {
		return err
	}
	if envelope.ActionButton == domain.ActionApprove {
		return a.persistApproval(ctx, envelope)
	}
	return a.persistCorrection(ctx, envelope)
}

func (a *App) persistApproval(ctx context.Context, envelope domain.ApprovalSignalEnvelope) error {
	now := a.clock.Now()
	approvalPayload := map[string]any{
		"action_button": string(envelope.ActionButton),
		"channel":       envelope.Channel,
	}
	if note := strings.TrimSpace(envelope.FreeText); note != "" {
		approvalPayload["operator_note"] = note
	}
	_, err := a.store.CreateAuditEvent(ctx, auditEvent(a.ids, now, envelope.TenantID, "approval_received", "operator", envelope.OperatorID, "decision_point", envelope.DecisionPointID, map[string]any{
		"case_run_id": envelope.CaseRunID,
		"packet_id":   envelope.PacketID,
	}, approvalPayload, "approval", envelope.TenantID, envelope.CaseRunID, envelope.DecisionPointID, string(envelope.ActionButton)))
	if err != nil {
		return err
	}
	if err := a.store.UpdateDecisionPointStatus(ctx, envelope.TenantID, envelope.DecisionPointID, "approved"); err != nil {
		return err
	}
	if err := a.store.UpdateCaseRunStatus(ctx, envelope.TenantID, envelope.CaseRunID, "approved"); err != nil {
		return err
	}
	_, sendErr := a.SendApprovedCustomerReply(ctx, envelope.TenantID, envelope.CaseRunID)
	if errors.Is(sendErr, channel.ErrAdapterNotAvailable) || errors.Is(sendErr, channel.ErrSessionNotConnected) || errors.Is(sendErr, domain.ErrNotFound) {
		return nil
	}
	return sendErr
}

func (a *App) SendApprovedCustomerReply(ctx context.Context, tenantID, caseRunID string) (domain.OutboundToCustomer, error) {
	run, err := a.store.GetCaseRun(ctx, tenantID, caseRunID)
	if err != nil {
		return domain.OutboundToCustomer{}, err
	}
	binding, err := a.store.ChannelBindingByTenant(ctx, tenantID, string(channel.ChannelWhatsApp))
	if err != nil {
		return domain.OutboundToCustomer{}, err
	}
	mode := domain.ParseInboundMode(string(binding.InboundMode))
	body := draftBodyForRun(run)
	bodyHash := domain.SHA256Key(body)
	switch mode {
	case domain.InboundModeReadOnly:
		recipient := binding.DraftDeliveryJID
		if recipient == "" {
			recipient = binding.OperatorJID
		}
		if recipient == "" {
			recipient = normalizeWhatsAppJID(binding.ProviderNumber)
		}
		if err := a.sendWhatsApp(ctx, binding, channel.OutboundMessage{
			TenantID:            tenantID,
			RecipientIdentifier: recipient,
			PacketID:            run.DecisionPointID,
			BodyText:            renderOperatorDraft(run, body),
			IdempotencyKey:      domain.SHA256Key("draft-delivery", tenantID, caseRunID, bodyHash),
		}); err != nil {
			return domain.OutboundToCustomer{}, err
		}
		_, err := a.store.CreateAuditEvent(ctx, auditEvent(a.ids, a.clock.Now(), tenantID, "outbound_draft_delivered_to_operator", "system", "app", "case_run", caseRunID, nil, map[string]any{
			"body_hash": bodyHash,
		}, "draft-delivered", tenantID, caseRunID, bodyHash))
		return domain.OutboundToCustomer{}, err
	case domain.InboundModeFullControl:
		approvalID, err := a.store.ApprovalAuditID(ctx, tenantID, run.ID, run.DecisionPointID)
		if err != nil {
			_, _ = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, a.clock.Now(), tenantID, "mcp_blocked_write_attempted", "system", "app", "case_run", caseRunID, nil, map[string]any{
				"reason": "approval_required",
			}, "mcp-blocked-customer", tenantID, caseRunID, bodyHash))
			return domain.OutboundToCustomer{}, domain.ErrApprovalRequired
		}
		recipient := stringValue(run.InputPayload, "customer_identifier", "")
		if recipient == "" {
			return domain.OutboundToCustomer{}, domain.ErrInvalidInput
		}
		recipient = normalizeWhatsAppJID(recipient)
		out := domain.OutboundToCustomer{
			ID:                  a.ids.NewID("otc"),
			TenantID:            tenantID,
			CaseRunID:           caseRunID,
			Channel:             "whatsapp_a1",
			RecipientIdentifier: recipient,
			BodyHash:            bodyHash,
			MCPApprovalAuditID:  approvalID,
			Status:              "queued",
		}
		created, stored, err := a.store.UpsertOutboundToCustomer(ctx, out)
		if err != nil {
			return domain.OutboundToCustomer{}, err
		}
		if !created && stored.Status == "sent" {
			return stored, nil
		}
		receipt, err := a.sendWhatsAppWithReceipt(ctx, binding, channel.OutboundMessage{
			TenantID:            tenantID,
			RecipientIdentifier: recipient,
			PacketID:            run.DecisionPointID,
			BodyText:            body,
			IdempotencyKey:      domain.SHA256Key("customer-outbound", tenantID, caseRunID, bodyHash),
		})
		if err != nil {
			return out, err
		}
		out.ProviderMessageID = receipt.ProviderMessageID
		sentAt := receipt.SentAt
		if sentAt.IsZero() {
			sentAt = a.clock.Now().UTC()
		}
		out.SentAt = &sentAt
		out.Status = "sent"
		_, out, err = a.store.UpsertOutboundToCustomer(ctx, out)
		if err != nil {
			return domain.OutboundToCustomer{}, err
		}
		_, err = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, a.clock.Now(), tenantID, "customer_outbound_sent", "system", "app", "case_run", caseRunID, map[string]any{
			"mcp_approval_audit_id": approvalID,
		}, map[string]any{
			"recipient_jid":       recipient,
			"body_hash":           bodyHash,
			"provider_message_id": receipt.ProviderMessageID,
		}, "customer-outbound-sent", tenantID, caseRunID, bodyHash))
		return out, err
	default:
		return domain.OutboundToCustomer{}, domain.ErrInvalidInput
	}
}

func (a *App) sendWhatsApp(ctx context.Context, binding domain.ChannelBinding, msg channel.OutboundMessage) error {
	_, err := a.sendWhatsAppWithReceipt(ctx, binding, msg)
	return err
}

func (a *App) sendWhatsAppWithReceipt(ctx context.Context, binding domain.ChannelBinding, msg channel.OutboundMessage) (channel.DeliveryReceipt, error) {
	a.gateway.mu.RLock()
	adapter := a.gateway.adapters[channel.ChannelWhatsApp]
	a.gateway.mu.RUnlock()
	if adapter == nil {
		return channel.DeliveryReceipt{}, channel.ErrAdapterNotAvailable
	}
	if guarded, ok := adapter.(interface {
		SendOutboundWithBinding(context.Context, domain.ChannelBinding, channel.OutboundMessage) (channel.DeliveryReceipt, error)
	}); ok {
		return guarded.SendOutboundWithBinding(ctx, binding, msg)
	}
	return adapter.SendOutbound(ctx, msg)
}

func (a *App) persistCorrection(ctx context.Context, envelope domain.ApprovalSignalEnvelope) error {
	now := a.clock.Now()
	correction := domain.Correction{
		ID:                  a.ids.NewID("corr"),
		SchemaVersion:       envelope.SchemaVersion,
		IdempotencyKey:      envelope.IdempotencyKey,
		PacketID:            envelope.PacketID,
		CaseRunID:           envelope.CaseRunID,
		DecisionPointID:     envelope.DecisionPointID,
		TenantID:            envelope.TenantID,
		OperatorID:          envelope.OperatorID,
		Channel:             envelope.Channel,
		SourceMessageID:     envelope.SourceMessageID,
		RawInboundMessageID: envelope.RawInboundMessageID,
		ActionButton:        envelope.ActionButton,
		FreeText:            envelope.FreeText,
		FollowUpAnswer:      envelope.FollowUpAnswer,
		ScopeHint:           envelope.ScopeHint,
		ParseMethod:         envelope.ParserMethod,
		ParseConfidence:     envelope.ParserConfidence,
		CreatedAt:           envelope.TS,
	}
	created, err := a.store.CreateCorrection(ctx, correction)
	if err != nil {
		return err
	}
	if !created {
		return nil
	}
	_, err = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, now, envelope.TenantID, "correction_received", "operator", envelope.OperatorID, "correction", correction.ID, map[string]any{
		"case_run_id":       envelope.CaseRunID,
		"decision_point_id": envelope.DecisionPointID,
		"packet_id":         envelope.PacketID,
	}, map[string]any{
		"action_button": string(envelope.ActionButton),
		"channel":       envelope.Channel,
	}, "correction", envelope.TenantID, envelope.IdempotencyKey))
	if err != nil {
		return err
	}
	if err := a.store.UpdateDecisionPointStatus(ctx, envelope.TenantID, envelope.DecisionPointID, "corrected"); err != nil {
		return err
	}
	if err := a.store.UpdateCaseRunStatus(ctx, envelope.TenantID, envelope.CaseRunID, "corrected"); err != nil {
		return err
	}
	return a.mergeCandidate(ctx, correction)
}

func (a *App) mergeCandidate(ctx context.Context, correction domain.Correction) error {
	point, err := a.store.GetDecisionPoint(ctx, correction.TenantID, correction.DecisionPointID)
	if err != nil {
		return err
	}
	conditions, recommendedAction := structuredRuleFromCorrection(correction, point)
	if len(conditions) == 0 || recommendedAction == "" {
		_, _ = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, a.clock.Now(), correction.TenantID, "correction_parse_failed", "system", "learning", "correction", correction.ID, nil, map[string]any{
			"action_button": string(correction.ActionButton),
		}, "parse-failed", correction.TenantID, correction.ID))
		return nil
	}
	conditionsHash, canonical, err := domain.ConditionsHash(conditions)
	if err != nil {
		return err
	}
	now := a.clock.Now()
	workflowSlug := workflowFromCaseInput(point.AgentInput)
	siblings, err := a.store.ListCandidatesByConditions(ctx, correction.TenantID, workflowSlug, point.DecisionType, conditionsHash)
	if err != nil {
		return err
	}
	var match domain.RuleCandidate
	var contradicting []domain.RuleCandidate
	for _, sibling := range siblings {
		if sibling.RecommendedAction == recommendedAction {
			match = sibling
		} else {
			contradicting = append(contradicting, sibling)
		}
	}
	if match.ID == "" {
		for _, conflict := range contradicting {
			conflict.ContradictingCount++
			conflict.Confidence = domain.CandidateConfidence(now, conflict.EvidenceCount, conflict.ContradictingCount, conflict.SourceCaseRunIDs, []string{conflict.TenantID}, conflict.LastSeenAt)
			conflict.Status = domain.CandidateStatus(conflict.Confidence, conflict.EvidenceCount, conflict.ContradictingCount)
			if err := a.store.SaveCandidate(ctx, conflict); err != nil {
				return err
			}
			_, _ = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, now, correction.TenantID, "candidate_contradiction_detected", "system", "learning", "rule_candidate", conflict.ID, map[string]any{
				"correction_id":      correction.ID,
				"conflicting_action": recommendedAction,
			}, nil, "candidate-contradiction", correction.TenantID, conflict.ID, correction.ID))
			a.gateway.SendNotification(ctx, correction.TenantID, formatContradictionAlert(conflict, recommendedAction))
		}
		scope := domain.ScopeCase
		if correction.ScopeHint != nil {
			scope = *correction.ScopeHint
		}
		candidate := domain.RuleCandidate{
			ID:                  a.ids.NewID("rc"),
			TenantID:            correction.TenantID,
			WorkflowSlug:        workflowSlug,
			DecisionType:        point.DecisionType,
			ConditionsHash:      conditionsHash,
			ConditionsCanonical: canonical,
			RecommendedAction:   recommendedAction,
			Scope:               scope,
			EvidenceCount:       1,
			SourceCorrectionIDs: []string{correction.ID},
			SourceCaseRunIDs:    []string{correction.CaseRunID},
			Status:              "candidate",
			CreatedAt:           now,
			LastSeenAt:          now,
		}
		candidate.Confidence = domain.CandidateConfidence(now, candidate.EvidenceCount, candidate.ContradictingCount, candidate.SourceCaseRunIDs, []string{candidate.TenantID}, candidate.LastSeenAt)
		candidate.Status = domain.CandidateStatus(candidate.Confidence, candidate.EvidenceCount, candidate.ContradictingCount)
		if candidate.Status == "under_review" {
			candidate.UnderReviewAt = &now
		}
		if err := a.store.SaveCandidate(ctx, candidate); err != nil {
			return err
		}
		_, err = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, now, correction.TenantID, "candidate_created", "system", "learning", "rule_candidate", candidate.ID, map[string]any{
			"correction_id": correction.ID,
		}, nil, "candidate-created", correction.TenantID, candidate.ID))
		a.gateway.SendNotification(ctx, correction.TenantID, formatLearningStatus(candidate, "first"))
		return err
	}
	if contains(match.SourceCorrectionIDs, correction.ID) {
		return nil
	}
	match.SourceCorrectionIDs = append(match.SourceCorrectionIDs, correction.ID)
	match.SourceCaseRunIDs = appendIfMissing(match.SourceCaseRunIDs, correction.CaseRunID)
	match.EvidenceCount++
	match.LastSeenAt = now
	match.Confidence = domain.CandidateConfidence(now, match.EvidenceCount, match.ContradictingCount, match.SourceCaseRunIDs, []string{match.TenantID}, match.LastSeenAt)
	nextStatus := domain.CandidateStatus(match.Confidence, match.EvidenceCount, match.ContradictingCount)
	if match.Status != "under_review" && nextStatus == "under_review" {
		match.UnderReviewAt = &now
	}
	match.Status = nextStatus
	if err := a.store.SaveCandidate(ctx, match); err != nil {
		return err
	}
	_, err = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, now, correction.TenantID, "candidate_evidence_added", "system", "learning", "rule_candidate", match.ID, map[string]any{
		"correction_id": correction.ID,
	}, nil, "candidate-evidence", correction.TenantID, match.ID, correction.ID))
	a.gateway.SendNotification(ctx, correction.TenantID, formatLearningStatus(match, "evidence"))
	return err
}

// formatContradictionAlert is the operator-visible message Victoria sends
// when a new correction contradicts an earlier one for the same conditions.
// Goal: catch operator inconsistency before it becomes a permanent rule.
func formatContradictionAlert(prior domain.RuleCandidate, newAction string) string {
	priorStatus := "we'd been agreeing on"
	if prior.Status == "under_review" {
		priorStatus = "I'd flagged for promotion"
	}
	return fmt.Sprintf(
		"⚠️ *Conflict detected.*\n\nFor this same case pattern %s *%s* (%d matching corrections so far). Your latest reply tells me to do *%s* instead.\n\nI won't promote either rule until this is resolved. Reply with which one is right, or escalate to a senior reviewer.",
		priorStatus, prior.RecommendedAction, prior.EvidenceCount, newAction)
}

// formatLearningStatus narrates Victoria's internal learning state back to
// the operator after a correction is merged. Goal: every correction visibly
// changes something the operator can see.
func formatLearningStatus(candidate domain.RuleCandidate, kind string) string {
	switch candidate.Status {
	case "under_review":
		return fmt.Sprintf(
			"🔔 That's *%d corrections* matching this same case pattern.\n\nI'm flagging a new rule for your review:\n  → %s\n\nReply *promote* (or run /admin/candidates/%s/%s/promote) when ready, and I'll apply it from here on.",
			candidate.EvidenceCount, candidate.RecommendedAction, candidate.TenantID, candidate.ID)
	default:
		// Build a remaining-evidence countdown. We use 3 as the threshold
		// (DefaultMinEvidenceCount) so the operator sees concrete progress.
		remaining := domain.DefaultMinEvidenceCount - candidate.EvidenceCount
		if remaining <= 0 {
			remaining = 1
		}
		switch kind {
		case "first":
			return fmt.Sprintf("✅ Got it — recorded your correction. (%d of %d matches before I propose a rule.)",
				candidate.EvidenceCount, domain.DefaultMinEvidenceCount)
		default:
			return fmt.Sprintf("✅ Got it. (%d of %d matches — %d more to go.)",
				candidate.EvidenceCount, domain.DefaultMinEvidenceCount, remaining)
		}
	}
}

func (a *App) createSkillVersion(ctx context.Context, tenantID, workflowSlug string, now time.Time) (domain.SkillVersion, error) {
	active, err := a.store.ActiveSkillVersion(ctx, tenantID, workflowSlug)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return domain.SkillVersion{}, err
	}
	version := 1
	if err == nil {
		version = active.Version + 1
	}
	rules, err := a.store.ListVisibleValidatedRules(ctx, tenantID, workflowSlug)
	if err != nil {
		return domain.SkillVersion{}, err
	}
	manifest := make([]domain.RuleManifest, 0, len(rules))
	for _, rule := range rules {
		manifest = append(manifest, domain.RuleManifest{
			RuleID:              rule.ID,
			Scope:               rule.Scope,
			Version:             rule.Version,
			DecisionType:        rule.DecisionType,
			ConditionsCanonical: append([]domain.Condition(nil), rule.ConditionsCanonical...),
			RecommendedAction:   rule.RecommendedAction,
			Priority:            scopePriority(rule.Scope),
		})
	}
	sort.Slice(manifest, func(i, j int) bool {
		if manifest[i].Priority != manifest[j].Priority {
			return manifest[i].Priority > manifest[j].Priority
		}
		return manifest[i].RuleID < manifest[j].RuleID
	})
	sv := domain.SkillVersion{
		ID:           a.ids.NewID("sv"),
		TenantID:     tenantID,
		WorkflowSlug: workflowSlug,
		Version:      version,
		RuleManifest: manifest,
		Status:       "active",
		CreatedAt:    now,
	}
	if err := a.store.CreateSkillVersion(ctx, sv); err != nil {
		return domain.SkillVersion{}, err
	}
	_, err = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, now, tenantID, "skill_version_created", "system", "promotion_pipeline", "skill_version", sv.ID, nil, map[string]any{
		"workflow_slug": workflowSlug,
		"rule_count":    len(manifest),
	}, "skill-version", tenantID, sv.ID))
	if err != nil {
		return domain.SkillVersion{}, err
	}
	return sv, nil
}

func (a *App) evaluateDecision(now time.Time, run domain.CaseRun, payload map[string]any, sv domain.SkillVersion) domain.DecisionPoint {
	decisionType, defaultAction := workflowDecision(run.WorkflowSlug)
	action := defaultAction
	for _, rule := range sv.RuleManifest {
		if rule.DecisionType == decisionType && conditionsMatch(payload, rule.ConditionsCanonical) {
			action = rule.RecommendedAction
			break
		}
	}
	agentInput := cloneMap(payload)
	agentInput["workflow_slug"] = run.WorkflowSlug
	return domain.DecisionPoint{
		ID:             a.ids.NewID("dp"),
		TenantID:       run.TenantID,
		CaseRunID:      run.ID,
		DecisionType:   decisionType,
		AgentInput:     agentInput,
		ProposedAction: action,
		Status:         "waiting_for_approval",
		CreatedAt:      now,
	}
}

func (a *App) buildArtifact(now time.Time, run domain.CaseRun, point domain.DecisionPoint) domain.Artifact {
	return domain.Artifact{
		ID:              a.ids.NewID("art"),
		TenantID:        run.TenantID,
		CaseRunID:       run.ID,
		DecisionPointID: point.ID,
		ArtifactType:    "draft_email",
		StoragePath:     fmt.Sprintf("/%s/sandbox/%s/artifacts/draft_email.json", run.TenantID, run.ID),
		Content: map[string]any{
			"proposed_action": point.ProposedAction,
			"workflow_slug":   run.WorkflowSlug,
		},
		CreatedAt: now,
	}
}

func (a *App) buildReviewPacket(now time.Time, run domain.CaseRun, point domain.DecisionPoint, artifact domain.Artifact) domain.ReviewPacket {
	expiresAt := now.Add(48 * time.Hour)
	return domain.ReviewPacket{
		PacketID:        a.ids.NewID("pkt"),
		SchemaVersion:   "1.1",
		TenantID:        run.TenantID,
		CaseRunID:       run.ID,
		DecisionPointID: point.ID,
		WorkflowType:    run.WorkflowSlug,
		Mode:            run.Mode,
		Trigger: map[string]any{
			"summary":        "Sandbox case ready for review",
			"source_channel": "fixture",
			"received_at":    now.Format(time.RFC3339),
		},
		Facts: factsFromPayload(run.InputPayload),
		PlannedAction: domain.PlannedAction{
			Type:             point.ProposedAction,
			Description:      point.ProposedAction,
			IsDestructive:    false,
			RequiresApproval: true,
		},
		ArtifactPreview: domain.ArtifactPreview{
			Type:       artifact.ArtifactType,
			PreviewURL: fmt.Sprintf("/preview/%s", artifact.ID),
			ExpiresAt:  expiresAt,
		},
		ButtonSet: []domain.ActionButton{
			domain.ActionApprove,
			domain.ActionWrongFacts,
			domain.ActionWrongAction,
			domain.ActionMissingCondition,
			domain.ActionUseDifferentTemplate,
			domain.ActionAddNote,
		},
		ExpiresAt:      expiresAt,
		IdempotencyKey: domain.SHA256Key(run.TenantID, point.ID, "packet", "1"),
	}
}

func defaultWorkflowTemplates(vertical string) []domain.WorkflowTemplate {
	return []domain.WorkflowTemplate{
		{ID: "wt_enquiry_" + vertical, Slug: "enquiry_triage", Vertical: vertical, DisplayName: "Enquiry triage", DecisionTypes: []string{"route_or_reply"}},
		{ID: "wt_quote_" + vertical, Slug: "quote_drafting", Vertical: vertical, DisplayName: "Quote drafting", DecisionTypes: []string{"send_or_hold"}},
		{ID: "wt_invoice_" + vertical, Slug: "invoice_handling", Vertical: vertical, DisplayName: "Invoice handling", DecisionTypes: []string{"tax_treatment"}},
	}
}

func workflowDecision(workflowSlug string) (string, string) {
	switch workflowSlug {
	case "invoice_handling":
		return "tax_treatment", "apply_gst"
	case "enquiry_triage":
		return "route_or_reply", "draft_reply"
	default:
		return "send_or_hold", "send_quote"
	}
}

func workflowFromCaseInput(input map[string]any) string {
	if value, ok := input["workflow_slug"].(string); ok && value != "" {
		return value
	}
	return "quote_drafting"
}

func structuredRuleFromCorrection(c domain.Correction, point domain.DecisionPoint) ([]domain.Condition, string) {
	text := strings.ToLower(c.FreeText + " " + c.FollowUpAnswer)
	switch {
	case strings.Contains(text, "singapore") || strings.Contains(text, "no gst"):
		return []domain.Condition{{Field: "supplier_country", Operator: "!=", Value: "AU"}}, "apply_no_gst"
	case strings.Contains(text, "commercial") && strings.Contains(text, "template"):
		return []domain.Condition{{Field: "enquiry_type", Operator: "=", Value: "commercial"}}, "use_corporate_template"
	case strings.Contains(text, "send it anyway") || strings.Contains(text, "go ahead"):
		return []domain.Condition{
			{Field: "photos_complete", Operator: "=", Value: boolValue(point.AgentInput, "photos_complete")},
			{Field: "client_type", Operator: "=", Value: stringValue(point.AgentInput, "client_type", "new")},
		}, "send_quote"
	case strings.Contains(text, "repeat") || strings.Contains(text, "known client"):
		return []domain.Condition{
			{Field: "photos_complete", Operator: "=", Value: boolValue(point.AgentInput, "photos_complete")},
			{Field: "client_type", Operator: "=", Value: "repeat"},
		}, "send_quote"
	case strings.Contains(text, "photo") || strings.Contains(text, "hold") || strings.Contains(text, "more info"):
		return []domain.Condition{
			{Field: "photos_complete", Operator: "=", Value: false},
			{Field: "client_type", Operator: "=", Value: stringValue(point.AgentInput, "client_type", "new")},
		}, "hold_and_request_more_info"
	default:
		if c.ActionButton == domain.ActionUseDifferentTemplate {
			return []domain.Condition{{Field: "enquiry_type", Operator: "=", Value: stringValue(point.AgentInput, "enquiry_type", "commercial")}}, "use_corporate_template"
		}
		return nil, ""
	}
}

func conditionsMatch(payload map[string]any, conditions []domain.Condition) bool {
	for _, condition := range conditions {
		actual, ok := payload[condition.Field]
		switch condition.Operator {
		case "=":
			if !ok || fmt.Sprint(actual) != fmt.Sprint(condition.Value) {
				return false
			}
		case "!=":
			if ok && fmt.Sprint(actual) == fmt.Sprint(condition.Value) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func redactAggregateConditions(conditions []domain.Condition) ([]domain.Condition, bool) {
	out := make([]domain.Condition, 0, len(conditions))
	quarantine := false
	for _, condition := range conditions {
		redacted := condition
		value := fmt.Sprint(condition.Value)
		lowerField := strings.ToLower(condition.Field)
		switch {
		case strings.Contains(value, "@"):
			redacted.Value = "<email>"
		case strings.Contains(lowerField, "client_name") || strings.Contains(lowerField, "customer_name"):
			redacted.Value = "<quarantined:freetext>"
			quarantine = true
		case looksLikeFreeText(value):
			redacted.Value = "<quarantined:freetext>"
			quarantine = true
		}
		out = append(out, redacted)
	}
	return out, quarantine
}

func looksLikeFreeText(value string) bool {
	if value == "" {
		return false
	}
	allowed := map[string]struct{}{
		"true": {}, "false": {}, "new": {}, "repeat": {}, "commercial": {}, "residential": {}, "AU": {}, "SG": {},
	}
	if _, ok := allowed[value]; ok {
		return false
	}
	return strings.Contains(value, " ")
}

func factsFromPayload(payload map[string]any) []domain.Fact {
	keys := make([]string, 0, len(payload))
	for key := range payload {
		if key != "sandbox" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	facts := make([]domain.Fact, 0, len(keys))
	for _, key := range keys {
		facts = append(facts, domain.Fact{Label: key, Value: fmt.Sprint(payload[key]), Confidence: 1})
	}
	return facts
}

func payloadFromIngestionEvent(event IngestionEvent) map[string]any {
	payload := map[string]any{
		"sandbox":             true,
		"source_channel":      event.Channel,
		"source_message_id":   event.SourceMessageID,
		"customer_identifier": event.CustomerIdentifier,
		"received_at":         event.ReceivedAt.UTC().Format(time.RFC3339),
		"body_text":           event.BodyText,
		"message_excerpt":     excerpt(event.BodyText, 180),
		"from":                event.CustomerIdentifier,
	}
	if strings.TrimSpace(event.Subject) != "" {
		payload["subject"] = strings.TrimSpace(event.Subject)
	}
	if len(event.Metadata) > 0 {
		payload["metadata"] = cloneMap(event.Metadata)
	}
	return payload
}

func draftBodyForRun(run domain.CaseRun) string {
	customer := stringValue(run.InputPayload, "customer_identifier", "there")
	switch run.WorkflowSlug {
	case "quote_drafting":
		return fmt.Sprintf("Hi! Thanks for reaching out. We can help with that quote, %s. Could you send 2-3 photos so we can size it correctly?", customer)
	default:
		body := stringValue(run.InputPayload, "body_text", "")
		if body == "" {
			return "Hi! Thanks for reaching out. We'll review this and get back to you shortly."
		}
		return "Hi! Thanks for reaching out. We'll review your message and get back to you shortly."
	}
}

func renderOperatorDraft(run domain.CaseRun, body string) string {
	customer := stringValue(run.InputPayload, "customer_identifier", "the customer")
	return fmt.Sprintf("✅ Approved. Here's the draft to send to %s:\n\n──────\n%s\n──────\n\nLong-press, Forward, pick the customer's chat.", customer, body)
}

func excerpt(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return strings.TrimSpace(value[:limit])
}

func normalizeInboundJID(msg channel.InboundMessage) string {
	if jid := normalizeWhatsAppJID(msg.SenderJID); jid != "" {
		return jid
	}
	return normalizeWhatsAppJID(msg.SenderIdentifier)
}

func normalizeWhatsAppJID(input string) string {
	clean := strings.TrimSpace(input)
	if clean == "" {
		return ""
	}
	if strings.ContainsRune(clean, '@') {
		return clean
	}
	clean = strings.TrimPrefix(clean, "+")
	return clean + "@s.whatsapp.net"
}

func removeString(values []string, target string) []string {
	out := values[:0]
	for _, value := range values {
		if value != target {
			out = append(out, value)
		}
	}
	return out
}

func auditEvent(ids IDGenerator, now time.Time, tenantID, eventType, actorType, actorID, refType, refID string, related map[string]any, payload map[string]any, keyParts ...string) domain.AuditEvent {
	return domain.AuditEvent{
		ID:             ids.NewID("ae"),
		TenantID:       tenantID,
		EventType:      eventType,
		ActorType:      actorType,
		ActorID:        actorID,
		RefType:        refType,
		RefID:          refID,
		RelatedIDs:     cloneMap(related),
		Payload:        cloneMap(payload),
		IdempotencyKey: domain.SHA256Key(keyParts...),
		OccurredAt:     now,
	}
}

func scopePriority(scope domain.Scope) int {
	switch scope {
	case domain.ScopeCase:
		return 4
	case domain.ScopeTenant:
		return 3
	case domain.ScopeVertical:
		return 2
	default:
		return 1
	}
}

func boolValue(payload map[string]any, key string) bool {
	value, ok := payload[key]
	if !ok {
		return false
	}
	if b, ok := value.(bool); ok {
		return b
	}
	return fmt.Sprint(value) == "true"
}

func stringValue(payload map[string]any, key, fallback string) string {
	if value, ok := payload[key]; ok && fmt.Sprint(value) != "" {
		return fmt.Sprint(value)
	}
	return fallback
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func contains(values []string, value string) bool {
	for _, current := range values {
		if current == value {
			return true
		}
	}
	return false
}

func appendIfMissing(values []string, value string) []string {
	if contains(values, value) {
		return values
	}
	return append(values, value)
}
