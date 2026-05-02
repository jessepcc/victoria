package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"victoria/internal/channel"
	"victoria/internal/domain"
)

type InboundReply struct {
	Channel             string              `json:"channel"`
	ProviderNumber      string              `json:"provider_number"`
	PacketID            string              `json:"packet_id"`
	SourceMessageID     string              `json:"source_message_id"`
	RawInboundMessageID string              `json:"raw_inbound_message_id"`
	ActionButton        domain.ActionButton `json:"action_button"`
	FreeText            string              `json:"free_text"`
	FollowUpAnswer      string              `json:"follow_up_answer"`
	ScopeHint           *domain.Scope       `json:"scope_hint,omitempty"`
	ReceivedAt          time.Time           `json:"received_at"`
}

type SignalPersister func(context.Context, domain.ApprovalSignalEnvelope) error

// OutboundQueueMax bounds the per-tenant durable outbound queue per
// operator-ux WA-INV-3.
const OutboundQueueMax = 100

type Gateway struct {
	store Store
	ids   IDGenerator
	clock Clock

	mu               sync.RWMutex
	adapters         map[channel.Channel]channel.Adapter
	tenantStatus     map[string]domain.SessionStatus // keyed by tenantID|channel
	pendingFollowups map[string]pendingFollowup      // keyed by tenantID
}

type pendingFollowup struct {
	PacketID  string
	StartedAt time.Time
}

const followupTTL = 1 * time.Hour

func NewGateway(store Store, ids IDGenerator, clock Clock) *Gateway {
	return &Gateway{
		store:            store,
		ids:              ids,
		clock:            clock,
		adapters:         map[channel.Channel]channel.Adapter{},
		tenantStatus:     map[string]domain.SessionStatus{},
		pendingFollowups: map[string]pendingFollowup{},
	}
}

// SetPendingFollowup records that the gateway is waiting for the operator's
// next message to be the correction reasoning for the given packet.
func (g *Gateway) SetPendingFollowup(tenantID, packetID string) {
	g.mu.Lock()
	g.pendingFollowups[tenantID] = pendingFollowup{PacketID: packetID, StartedAt: g.clock.Now()}
	g.mu.Unlock()
}

// ConsumePendingFollowup atomically returns and clears the pending follow-up
// for the tenant if one exists and hasn't expired.
func (g *Gateway) ConsumePendingFollowup(tenantID string) (string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	state, ok := g.pendingFollowups[tenantID]
	if !ok {
		return "", false
	}
	if g.clock.Now().Sub(state.StartedAt) > followupTTL {
		delete(g.pendingFollowups, tenantID)
		return "", false
	}
	delete(g.pendingFollowups, tenantID)
	return state.PacketID, true
}

// RegisterAdapter installs a ChannelAdapter for outbound delivery. Existing
// inbound paths (the /gateway/inbound webhook) remain unchanged.
func (g *Gateway) RegisterAdapter(adapter channel.Adapter) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.adapters[adapter.Channel()] = adapter
}

// SendNotification pushes a one-off informational message to the operator
// over their preferred channel. Used to surface learning-loop state changes
// (e.g. "got your correction, 2 of 3 matches"). Best-effort: failures don't
// propagate, but a failed notification logs an audit event so the operator
// is at least findable in the audit trail.
func (g *Gateway) SendNotification(ctx context.Context, tenantID, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	ch := g.preferredChannel(ctx, tenantID)
	g.mu.RLock()
	adapter := g.adapters[ch]
	g.mu.RUnlock()
	if adapter == nil || g.sessionStatus(tenantID, ch) != domain.SessionActive {
		return
	}
	binding, err := g.store.ChannelBindingByTenant(ctx, tenantID, string(ch))
	if err != nil {
		return
	}
	_, _ = adapter.SendOutbound(ctx, channel.OutboundMessage{
		TenantID:            tenantID,
		RecipientIdentifier: binding.ProviderNumber,
		BodyText:            text,
		IdempotencyKey:      domain.SHA256Key("notification", tenantID, text, g.clock.Now().Format(time.RFC3339Nano)),
	})
}

// NotifySessionStatus updates the gateway's view of a tenant/channel's session
// state. When the status flips to active the durable outbound queue for that
// tenant is drained.
func (g *Gateway) NotifySessionStatus(ctx context.Context, tenantID string, ch channel.Channel, status domain.SessionStatus) {
	g.mu.Lock()
	prev := g.tenantStatus[statusKey(tenantID, ch)]
	g.tenantStatus[statusKey(tenantID, ch)] = status
	g.mu.Unlock()
	_ = g.store.UpdateChannelSessionStatus(ctx, tenantID, string(ch), status)
	if status == domain.SessionActive && prev != domain.SessionActive {
		if err := g.drainQueue(ctx, tenantID, ch); err != nil {
			_, _ = g.store.CreateAuditEvent(ctx, auditEvent(g.ids, g.clock.Now(), tenantID, "outbound_drain_failed", "system", "gateway", "tenant", tenantID, nil, map[string]any{
				"channel": string(ch),
				"error":   err.Error(),
			}, "drain-failed", tenantID, string(ch)))
		}
	}
}

func (g *Gateway) sessionStatus(tenantID string, ch channel.Channel) domain.SessionStatus {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.tenantStatus[statusKey(tenantID, ch)]
}

func statusKey(tenantID string, ch channel.Channel) string {
	return tenantID + "\x00" + string(ch)
}

func (g *Gateway) SendApprovalPacket(ctx context.Context, packet domain.ReviewPacket) error {
	if err := g.store.CreateReviewPacket(ctx, packet); err != nil {
		return err
	}
	ch := g.preferredChannel(ctx, packet.TenantID)
	if g.canDeliverNow(packet.TenantID, ch) {
		if err := g.deliverPacket(ctx, packet, ch); err != nil {
			// Send failed at the adapter — fall back to durable queue so the
			// next reconnect retries it. Critical: do NOT silently swallow.
			return g.enqueuePacket(ctx, packet, ch)
		}
		return nil
	}
	return g.enqueuePacket(ctx, packet, ch)
}

// preferredChannel resolves a tenant to its primary outbound channel.
// WhatsApp wins whenever an adapter is registered AND a WhatsApp binding
// exists, regardless of current session status — so packets queue durably
// during outages instead of silently failing over to Telegram.
func (g *Gateway) preferredChannel(ctx context.Context, tenantID string) channel.Channel {
	g.mu.RLock()
	_, hasWA := g.adapters[channel.ChannelWhatsApp]
	g.mu.RUnlock()
	if hasWA {
		if _, err := g.store.ChannelBindingByTenant(ctx, tenantID, string(channel.ChannelWhatsApp)); err == nil {
			return channel.ChannelWhatsApp
		}
	}
	return channel.ChannelTelegram
}

func (g *Gateway) canDeliverNow(tenantID string, ch channel.Channel) bool {
	status := g.sessionStatus(tenantID, ch)
	if status == "" {
		// Telegram dev/in-memory: never explicitly transitioned, treat as ready.
		// WhatsApp must be explicitly active.
		return ch == channel.ChannelTelegram
	}
	return status == domain.SessionActive
}

// deliverPacket attempts a single send via the registered adapter.
// On adapter failure it returns the error WITHOUT mutating queue state — the
// caller (initial send vs. drain) decides what to do. This separation is
// what makes the drain loop safe under partial failure.
func (g *Gateway) deliverPacket(ctx context.Context, packet domain.ReviewPacket, ch channel.Channel) error {
	g.mu.RLock()
	adapter := g.adapters[ch]
	g.mu.RUnlock()
	if adapter != nil {
		binding, err := g.store.ChannelBindingByTenant(ctx, packet.TenantID, string(ch))
		if err != nil {
			return fmt.Errorf("gateway: lookup binding: %w", err)
		}
		out := channel.RenderPacket(packet, binding.ProviderNumber)
		if _, err := adapter.SendOutbound(ctx, out); err != nil {
			return fmt.Errorf("gateway: adapter send: %w", err)
		}
	}
	if err := g.store.MarkReviewPacketDelivered(ctx, packet.TenantID, packet.PacketID); err != nil {
		return err
	}
	_, err := g.store.CreateAuditEvent(ctx, auditEvent(g.ids, g.clock.Now(), packet.TenantID, "packet_sent", "system", "gateway", "review_packet", packet.PacketID, map[string]any{
		"case_run_id":       packet.CaseRunID,
		"decision_point_id": packet.DecisionPointID,
	}, map[string]any{
		"mode":    string(packet.Mode),
		"channel": string(ch),
	}, "packet-sent", packet.TenantID, packet.PacketID))
	return err
}

func (g *Gateway) enqueuePacket(ctx context.Context, packet domain.ReviewPacket, ch channel.Channel) error {
	depth, err := g.store.OutboundQueueDepth(ctx, packet.TenantID, string(ch))
	if err != nil {
		return err
	}
	if depth >= OutboundQueueMax {
		tombstoneID, err := g.store.DeleteOldestOutboundQueueEntry(ctx, packet.TenantID, string(ch))
		if err != nil {
			return err
		}
		_, _ = g.store.CreateAuditEvent(ctx, auditEvent(g.ids, g.clock.Now(), packet.TenantID, "packet_tombstoned", "system", "gateway", "review_packet", packet.PacketID, nil, map[string]any{
			"channel":      string(ch),
			"tombstone_id": tombstoneID,
		}, "packet-tombstoned", packet.TenantID, packet.PacketID))
	}
	if err := g.store.EnqueueOutbound(ctx, domain.OutboundQueueEntry{
		TenantID:       packet.TenantID,
		Channel:        string(ch),
		PacketID:       packet.PacketID,
		IdempotencyKey: packet.IdempotencyKey,
		EnqueuedAt:     g.clock.Now().UTC(),
	}); err != nil {
		return err
	}
	_, _ = g.store.CreateAuditEvent(ctx, auditEvent(g.ids, g.clock.Now(), packet.TenantID, "packet_queued", "system", "gateway", "review_packet", packet.PacketID, nil, map[string]any{
		"mode":    string(packet.Mode),
		"channel": string(ch),
	}, "packet-queued", packet.TenantID, packet.PacketID))
	return nil
}

// drainQueue replays pending packets in FIFO order. On adapter send failure
// the entry is LEFT IN PLACE so the next reconnect retries it. This is what
// keeps WA-INV-3 honest under mid-drain disconnects.
func (g *Gateway) drainQueue(ctx context.Context, tenantID string, ch channel.Channel) error {
	entries, err := g.store.ListOutboundQueue(ctx, tenantID, string(ch))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		packet, err := g.store.GetReviewPacket(ctx, tenantID, entry.PacketID)
		if err != nil {
			// Packet record vanished (e.g. tenant deprovisioned). Drop the
			// queue entry — there's nothing to deliver anymore.
			_ = g.store.DeleteOutboundQueueEntry(ctx, entry.ID)
			continue
		}
		if g.clock.Now().After(packet.ExpiresAt) {
			_ = g.store.DeleteOutboundQueueEntry(ctx, entry.ID)
			_, _ = g.store.CreateAuditEvent(ctx, auditEvent(g.ids, g.clock.Now(), tenantID, "packet_tombstoned", "system", "gateway", "review_packet", packet.PacketID, nil, map[string]any{
				"channel": string(ch),
				"reason":  "expired_in_queue",
			}, "packet-expired", tenantID, packet.PacketID))
			continue
		}
		if err := g.deliverPacket(ctx, packet, ch); err != nil {
			// Leave entry in place; next session-active transition retries it.
			return err
		}
		_ = g.store.DeleteOutboundQueueEntry(ctx, entry.ID)
	}
	return nil
}

func (g *Gateway) ReceiveInbound(ctx context.Context, input InboundReply, persist SignalPersister) (domain.ApprovalSignalEnvelope, error) {
	if input.Channel == "" {
		input.Channel = "telegram"
	}
	if input.ReceivedAt.IsZero() {
		input.ReceivedAt = g.clock.Now()
	}
	if input.RawInboundMessageID == "" {
		input.RawInboundMessageID = input.Channel + ":" + input.SourceMessageID
	}
	if !input.ActionButton.Valid() {
		return domain.ApprovalSignalEnvelope{}, domain.ErrInvalidInput
	}
	binding, err := g.store.ChannelBindingByProvider(ctx, input.Channel, input.ProviderNumber)
	if err != nil {
		_, _ = g.store.CreateAuditEvent(ctx, auditEvent(g.ids, g.clock.Now(), "", "unbound_inbound_dropped", "operator", "unknown", "message", input.RawInboundMessageID, nil, map[string]any{
			"provider_number": input.ProviderNumber,
			"channel":         input.Channel,
		}, "unbound", input.Channel, input.ProviderNumber, input.RawInboundMessageID))
		return domain.ApprovalSignalEnvelope{}, err
	}
	packet, err := g.store.GetReviewPacket(ctx, binding.TenantID, input.PacketID)
	if err != nil {
		return domain.ApprovalSignalEnvelope{}, err
	}
	if packet.TenantID != binding.TenantID {
		return domain.ApprovalSignalEnvelope{}, domain.ErrTenantMismatch
	}
	if g.clock.Now().After(packet.ExpiresAt) {
		_, _ = g.store.CreateAuditEvent(ctx, auditEvent(g.ids, g.clock.Now(), binding.TenantID, "stale_reply", "operator", binding.OperatorID, "review_packet", input.PacketID, nil, nil, "stale", binding.TenantID, input.PacketID, input.RawInboundMessageID))
		return domain.ApprovalSignalEnvelope{}, domain.ErrExpired
	}
	scopeHint := input.ScopeHint
	if scopeHint == nil {
		scopeHint = parseScopeHint(input.FollowUpAnswer)
	}
	envelope := domain.ApprovalSignalEnvelope{
		SchemaVersion:       "1",
		SignalID:            domain.SHA256Key(binding.TenantID, input.PacketID, string(input.ActionButton), input.RawInboundMessageID),
		IdempotencyKey:      domain.SHA256Key(binding.TenantID, packet.DecisionPointID, input.PacketID, string(input.ActionButton)),
		PacketID:            input.PacketID,
		CaseRunID:           packet.CaseRunID,
		DecisionPointID:     packet.DecisionPointID,
		TenantID:            binding.TenantID,
		OperatorID:          binding.OperatorID,
		Channel:             input.Channel,
		SourceMessageID:     input.SourceMessageID,
		RawInboundMessageID: input.RawInboundMessageID,
		TS:                  input.ReceivedAt.UTC(),
		ActionButton:        input.ActionButton,
		FreeText:            input.FreeText,
		FollowUpAnswer:      input.FollowUpAnswer,
		ScopeHint:           scopeHint,
		ParserMethod:        "button",
		ParserConfidence:    1.0,
	}
	if err := persist(ctx, envelope); err != nil {
		return domain.ApprovalSignalEnvelope{}, err
	}
	_, err = g.store.CreateAuditEvent(ctx, auditEvent(g.ids, g.clock.Now(), binding.TenantID, "correction_resolved_at_gateway", "operator", binding.OperatorID, "review_packet", input.PacketID, nil, map[string]any{
		"signal_id":     envelope.SignalID,
		"action_button": string(input.ActionButton),
	}, "gateway-resolved", binding.TenantID, envelope.SignalID))
	return envelope, err
}

// Disconnect simulates a session outage for the default Telegram channel.
// Used by tests; production transitions go through NotifySessionStatus.
func (g *Gateway) Disconnect(ctx context.Context, tenantID string) {
	g.NotifySessionStatus(ctx, tenantID, channel.ChannelTelegram, domain.SessionDisconnected)
	_, _ = g.store.CreateAuditEvent(ctx, auditEvent(g.ids, g.clock.Now(), tenantID, "channel_session_disconnected", "system", "gateway", "tenant", tenantID, nil, nil, "session-disconnect", tenantID, g.clock.Now().String()))
}

// Recover marks the default Telegram channel as active again and returns the
// drained packets (for tests). The drain itself is performed by
// NotifySessionStatus via drainQueue.
func (g *Gateway) Recover(ctx context.Context, tenantID string) []domain.ReviewPacket {
	queued, _ := g.store.ListOutboundQueue(ctx, tenantID, string(channel.ChannelTelegram))
	g.NotifySessionStatus(ctx, tenantID, channel.ChannelTelegram, domain.SessionActive)
	drained := make([]domain.ReviewPacket, 0, len(queued))
	for _, entry := range queued {
		packet, err := g.store.GetReviewPacket(ctx, tenantID, entry.PacketID)
		if err == nil {
			drained = append(drained, packet)
		}
	}
	_, _ = g.store.CreateAuditEvent(ctx, auditEvent(g.ids, g.clock.Now(), tenantID, "channel_session_recovered", "system", "gateway", "tenant", tenantID, nil, map[string]any{
		"drained": len(drained),
	}, "session-recover", tenantID, g.clock.Now().String()))
	return drained
}

func parseScopeHint(value string) *domain.Scope {
	lower := strings.ToLower(value)
	switch {
	case strings.Contains(lower, "always") || strings.Contains(lower, "tenant"):
		scope := domain.ScopeTenant
		return &scope
	case strings.Contains(lower, "this case") || strings.Contains(lower, "case"):
		scope := domain.ScopeCase
		return &scope
	default:
		return nil
	}
}
