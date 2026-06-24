package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jessepcc/victoria/internal/channel"
	"github.com/jessepcc/victoria/internal/domain"
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
	commandSecret, err := newCommandSecret()
	if err != nil {
		return domain.Tenant{}, domain.ProvisioningManifest{}, err
	}
	waBinding := domain.ChannelBinding{
		TenantID:                  tenant.ID,
		Channel:                   "whatsapp",
		ProviderNumber:            providerNumber,
		OperatorID:                operatorID,
		SessionStatus:             domain.SessionQRNeeded,
		SessionUpdated:            now,
		InboundMode:               domain.InboundModeReadOnly,
		RetentionMinutes:          30,
		OperatorJID:               normalizeWhatsAppJID(providerNumber),
		CommandRegistrationSecret: commandSecret,
	}
	if err := a.store.UpsertChannelBinding(ctx, waBinding); err != nil {
		return domain.Tenant{}, domain.ProvisioningManifest{}, err
	}
	manifest.WhatsAppCommandSecret = commandSecret
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
	_, err = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, now, tenant.ID, "tenant_provisioned", "admin", "system", "tenant", tenant.ID, nil, map[string]any{
		"vertical": tenant.Vertical,
	}, "tenant-provisioned", tenant.ID))
	if err != nil {
		return domain.Tenant{}, domain.ProvisioningManifest{}, err
	}
	return tenant, manifest, nil
}

// ReissueWhatsAppCommandSecret rotates the secret used to authorize
// `register me as operator <secret>` from a new operator JID. Use it when the
// original secret was leaked, lost, or already consumed but a new operator
// needs to register. Returns the new plaintext secret exactly once.
func (a *App) ReissueWhatsAppCommandSecret(ctx context.Context, tenantID string) (string, error) {
	binding, err := a.store.ChannelBindingByTenant(ctx, tenantID, string(channel.ChannelWhatsApp))
	if err != nil {
		return "", err
	}
	secret, err := newCommandSecret()
	if err != nil {
		return "", err
	}
	binding.CommandRegistrationSecret = secret
	binding.CommandSecretConsumedAt = nil
	binding.SessionUpdated = a.clock.Now().UTC()
	if err := a.store.UpsertChannelBinding(ctx, binding); err != nil {
		return "", err
	}
	return secret, nil
}

// newCommandSecret generates a 16-byte (128-bit) hex-encoded secret for the
// `register me as operator` flow. Single-use; persisted on the binding until
// consumed or reissued.
func newCommandSecret() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("command secret: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
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

// IngestCustomerMessage is the canonical 00-channel ingestion entry point
// per spec §4.1. Idempotent on (tenant_id, channel, source_message_id);
// re-deliveries return the existing case_run_id.
func (a *App) IngestCustomerMessage(ctx context.Context, event IngestionEvent) (string, error) {
	id, _, err := a.IngestCustomerMessageWithPacket(ctx, event)
	return id, err
}

// IngestCustomerMessageWithPacket is the same operation but also returns the
// freshly-created review packet so HTTP callers can surface it without a
// separate lookup (useful for demos and ops tooling). On idempotent replay
// the packet is empty — the existing case_run_id alone is the contract.
func (a *App) IngestCustomerMessageWithPacket(ctx context.Context, event IngestionEvent) (string, domain.ReviewPacket, error) {
	event.TenantID = strings.TrimSpace(event.TenantID)
	event.Channel = strings.TrimSpace(event.Channel)
	event.SourceMessageID = strings.TrimSpace(event.SourceMessageID)
	event.CustomerIdentifier = strings.TrimSpace(event.CustomerIdentifier)
	event.BodyText = strings.TrimSpace(event.BodyText)
	if event.TenantID == "" || event.Channel == "" || event.SourceMessageID == "" || event.CustomerIdentifier == "" || event.BodyText == "" {
		return "", domain.ReviewPacket{}, domain.ErrInvalidInput
	}
	if event.ReceivedAt.IsZero() {
		event.ReceivedAt = a.clock.Now()
	}
	if _, err := a.store.GetTenant(ctx, event.TenantID); err != nil {
		return "", domain.ReviewPacket{}, err
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
		return "", domain.ReviewPacket{}, err
	}
	if !created {
		// Idempotent replay. If a case was already started, its id is the
		// contract. But if a previous attempt failed before StartCase
		// succeeded (no case_run_id recorded), reprocess rather than returning
		// an empty id as success — otherwise a transient failure would
		// permanently poison this source_message_id against retry.
		if stored.CaseRunID != "" {
			return stored.CaseRunID, domain.ReviewPacket{}, nil
		}
		msg = stored
	}
	run, packet, err := a.StartCase(ctx, StartCaseInput{
		TenantID:     event.TenantID,
		WorkflowSlug: "enquiry_triage",
		Mode:         domain.ModeSandbox,
		Payload:      payloadFromIngestionEvent(event),
	})
	if err != nil {
		_ = a.store.UpdateCustomerMessageCase(ctx, event.TenantID, event.Channel, event.SourceMessageID, "", "failed")
		return "", domain.ReviewPacket{}, err
	}
	if err := a.store.UpdateCustomerMessageCase(ctx, event.TenantID, event.Channel, event.SourceMessageID, run.ID, "ingested"); err != nil {
		return "", domain.ReviewPacket{}, err
	}
	_, err = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, a.clock.Now(), event.TenantID, "customer_message_received", "customer", event.CustomerIdentifier, "customer_message", msg.ID, map[string]any{
		"case_run_id": run.ID,
	}, map[string]any{
		"channel":             event.Channel,
		"source_message_id":   event.SourceMessageID,
		"customer_identifier": event.CustomerIdentifier,
	}, "customer-message-received", event.TenantID, event.Channel, event.SourceMessageID))
	if err != nil {
		return "", domain.ReviewPacket{}, err
	}
	return run.ID, packet, nil
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
		// AC-A1.1 closing half: any review packets queued before a command
		// identity was registered now have a recipient and can be delivered.
		a.gateway.DrainQueue(ctx, tenantID, channel.ChannelWhatsApp)
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

func (a *App) ListCandidates(ctx context.Context, tenantID string) ([]domain.RuleCandidate, error) {
	return a.store.ListCandidates(ctx, tenantID)
}

// ListAuditEvents returns immutable audit events for a tenant in occurrence
// order. Optional filters narrow the result by event_type list and apply a
// max-count cap (0 = unlimited). Used by ops + demo tooling — no PII filter
// is applied since audit data is intentionally retained.
func (a *App) ListAuditEvents(ctx context.Context, tenantID string, eventTypes []string, limit int) ([]domain.AuditEvent, error) {
	events, err := a.store.ListAuditEvents(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if len(eventTypes) > 0 {
		filter := make(map[string]struct{}, len(eventTypes))
		for _, t := range eventTypes {
			filter[t] = struct{}{}
		}
		filtered := events[:0]
		for _, e := range events {
			if _, ok := filter[e.EventType]; ok {
				filtered = append(filtered, e)
			}
		}
		events = filtered
	}
	if limit > 0 && len(events) > limit {
		events = events[len(events)-limit:]
	}
	return events, nil
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
		recipient := operatorRecipient(binding)
		if recipient == "" {
			recipient = normalizeWhatsAppJID(binding.ProviderNumber)
		}
		// A0 draft delivery is split into TWO WhatsApp messages so the operator
		// can long-press the second (clean draft body) and Forward straight to
		// the customer's chat — no copy/paste, no editing out a wrapper. The
		// first message carries the approval acknowledgement + instructions.
		customer := pickCustomerLabel(run.InputPayload)
		instruction := fmt.Sprintf("✅ Approved. Long-press the next message → Forward → pick %s's chat.", customer)
		if err := a.sendWhatsApp(ctx, binding, channel.OutboundMessage{
			TenantID:            tenantID,
			RecipientIdentifier: recipient,
			PacketID:            run.DecisionPointID,
			BodyText:            instruction,
			IdempotencyKey:      domain.SHA256Key("draft-instruction", tenantID, caseRunID, bodyHash),
		}); err != nil {
			return domain.OutboundToCustomer{}, err
		}
		if err := a.sendWhatsApp(ctx, binding, channel.OutboundMessage{
			TenantID:            tenantID,
			RecipientIdentifier: recipient,
			PacketID:            run.DecisionPointID,
			BodyText:            body,
			IdempotencyKey:      domain.SHA256Key("draft-body", tenantID, caseRunID, bodyHash),
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
		// Tier E: clarification turn. Silent failure is bad UX — operators
		// don't know whether their correction took. Send back an explicit
		// "I didn't catch that" so they know to rephrase.
		a.gateway.SendNotification(ctx, correction.TenantID, formatClarificationRequest(correction.FreeText))
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
		a.deliverCorrectionDraftIfPossible(ctx, correction, point, recommendedAction)
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
	a.deliverCorrectionDraftIfPossible(ctx, correction, point, recommendedAction)
	return err
}

// deliverCorrectionDraftIfPossible sends a 2-message forwardable draft to the
// operator when the structured parser successfully extracted a recommended
// action from the correction. Closes the operator's immediate loop: they
// just corrected Victoria, but they still need to actually reply to THIS
// customer — Victoria should hand them a forward-ready draft.
//
// A0 (read_only) only. In A1, approval sends to the customer directly so
// the operator never needs to forward by hand. Best-effort: send failures
// are silently swallowed (the rule-learning side-effect is the durable bit).
func (a *App) deliverCorrectionDraftIfPossible(ctx context.Context, correction domain.Correction, point domain.DecisionPoint, recommendedAction string) {
	binding, err := a.store.ChannelBindingByTenant(ctx, correction.TenantID, string(channel.ChannelWhatsApp))
	if err != nil {
		return
	}
	if domain.ParseInboundMode(string(binding.InboundMode)) != domain.InboundModeReadOnly {
		return
	}
	body := draftBodyForAction(recommendedAction, point.AgentInput)
	if body == "" {
		return
	}
	recipient := operatorRecipient(binding)
	if recipient == "" {
		recipient = normalizeWhatsAppJID(binding.ProviderNumber)
	}
	customer := pickCustomerLabel(point.AgentInput)
	instruction := fmt.Sprintf("📝 Draft for %s based on your correction — long-press the next message → Forward.", customer)
	bodyHash := domain.SHA256Key(body)
	if err := a.sendWhatsApp(ctx, binding, channel.OutboundMessage{
		TenantID:            correction.TenantID,
		RecipientIdentifier: recipient,
		PacketID:            correction.DecisionPointID,
		BodyText:            instruction,
		IdempotencyKey:      domain.SHA256Key("correction-draft-instruction", correction.ID, bodyHash),
	}); err != nil {
		return
	}
	if err := a.sendWhatsApp(ctx, binding, channel.OutboundMessage{
		TenantID:            correction.TenantID,
		RecipientIdentifier: recipient,
		PacketID:            correction.DecisionPointID,
		BodyText:            body,
		IdempotencyKey:      domain.SHA256Key("correction-draft-body", correction.ID, bodyHash),
	}); err != nil {
		return
	}
	_, _ = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, a.clock.Now(), correction.TenantID, "outbound_correction_draft_delivered", "system", "app", "correction", correction.ID, nil, map[string]any{
		"body_hash":          bodyHash,
		"recommended_action": recommendedAction,
	}, "correction-draft-delivered", correction.TenantID, correction.ID, bodyHash))
}

// formatClarificationRequest is the operator-visible message Victoria sends
// when the structured parser couldn't extract a rule from the correction
// text — typically a typo, a too-vague instruction, or a phrasing the
// keyword + Levenshtein layers don't cover. The point is to keep the loop
// alive: the operator knows their correction didn't take, and they can
// rephrase. Silent failure (the previous behavior) led to operators
// thinking they'd corrected Victoria when they hadn't.
func formatClarificationRequest(freeText string) string {
	cleaned := strings.TrimSpace(freeText)
	if len(cleaned) > 80 {
		cleaned = cleaned[:80] + "…"
	}
	return fmt.Sprintf(
		"📝 I didn't quite catch that (\"%s\"). Could you rephrase?\n\nExamples:\n• \"hold and ask for photos\"\n• \"send it anyway\"\n• \"no GST — overseas supplier\"\n• \"use corporate template\"",
		cleaned,
	)
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
