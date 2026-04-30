package app

import (
	"context"
	"strings"
	"time"

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

type Gateway struct {
	store Store
	ids   IDGenerator
	clock Clock

	sessions map[string]*GatewaySession
}

type GatewaySession struct {
	Connected bool
	Queue     []domain.ReviewPacket
}

func NewGateway(store Store, ids IDGenerator, clock Clock) *Gateway {
	return &Gateway{
		store:    store,
		ids:      ids,
		clock:    clock,
		sessions: map[string]*GatewaySession{},
	}
}

func (g *Gateway) SendApprovalPacket(ctx context.Context, packet domain.ReviewPacket) error {
	session := g.session(packet.TenantID)
	if !session.Connected {
		if len(session.Queue) >= 100 {
			session.Queue = session.Queue[1:]
			_, _ = g.store.CreateAuditEvent(ctx, auditEvent(g.ids, g.clock.Now(), packet.TenantID, "packet_tombstoned", "system", "gateway", "review_packet", packet.PacketID, nil, nil, "packet-tombstoned", packet.TenantID, packet.PacketID))
		}
		session.Queue = append(session.Queue, packet)
	}
	if err := g.store.CreateReviewPacket(ctx, packet); err != nil {
		return err
	}
	_ = g.store.MarkReviewPacketDelivered(ctx, packet.TenantID, packet.PacketID)
	_, err := g.store.CreateAuditEvent(ctx, auditEvent(g.ids, g.clock.Now(), packet.TenantID, "packet_sent", "system", "gateway", "review_packet", packet.PacketID, map[string]any{
		"case_run_id":       packet.CaseRunID,
		"decision_point_id": packet.DecisionPointID,
	}, map[string]any{
		"mode": string(packet.Mode),
	}, "packet-sent", packet.TenantID, packet.PacketID))
	return err
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

func (g *Gateway) Disconnect(ctx context.Context, tenantID string) {
	session := g.session(tenantID)
	session.Connected = false
	_, _ = g.store.CreateAuditEvent(ctx, auditEvent(g.ids, g.clock.Now(), tenantID, "channel_session_disconnected", "system", "gateway", "tenant", tenantID, nil, nil, "session-disconnect", tenantID, g.clock.Now().String()))
}

func (g *Gateway) Recover(ctx context.Context, tenantID string) []domain.ReviewPacket {
	session := g.session(tenantID)
	session.Connected = true
	drained := append([]domain.ReviewPacket(nil), session.Queue...)
	session.Queue = nil
	_, _ = g.store.CreateAuditEvent(ctx, auditEvent(g.ids, g.clock.Now(), tenantID, "channel_session_recovered", "system", "gateway", "tenant", tenantID, nil, map[string]any{
		"drained": len(drained),
	}, "session-recover", tenantID, g.clock.Now().String()))
	return drained
}

func (g *Gateway) session(tenantID string) *GatewaySession {
	session, ok := g.sessions[tenantID]
	if !ok {
		session = &GatewaySession{Connected: true}
		g.sessions[tenantID] = session
	}
	return session
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
