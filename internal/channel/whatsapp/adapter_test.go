package whatsapp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jessepcc/victoria/internal/channel"
	"github.com/jessepcc/victoria/internal/domain"
)

func TestRenderTextPreservesBodyOnly(t *testing.T) {
	// renderText is now identity on the packet body. No appended numbered
	// list, no visible packet-id tag — operator UX is just the narrative.
	// Reply routing falls back to LatestReviewPacket.
	msg := channel.OutboundMessage{
		PacketID: "pkt_xyz",
		BodyText: "review this\n\nApproved? Y / N",
		Buttons: []channel.ButtonSpec{
			{ID: "approve", Label: "Approve"},
			{ID: "wrong_facts", Label: "Wrong facts"},
		},
	}
	got := renderText(msg)
	if got != msg.BodyText {
		t.Fatalf("renderText should be identity on body; got %q", got)
	}
	if strings.Contains(got, "[packet:") {
		t.Fatalf("packet tag should NOT leak into operator-visible body: %q", got)
	}
	if strings.Contains(got, "1. Approve") {
		t.Fatalf("numbered options should NOT be auto-appended: %q", got)
	}
}

func TestButtonForReplyMapsNumbersAndLabels(t *testing.T) {
	buttons := []channel.ButtonSpec{
		{ID: "approve", Label: "Approve"},
		{ID: "wrong_facts", Label: "Wrong facts"},
		{ID: "wrong_action", Label: "Wrong action"},
	}
	cases := []struct {
		input string
		want  string
	}{
		{"1", "approve"},
		{"3", "wrong_action"},
		{"approve", "approve"},
		{"Wrong facts", "wrong_facts"},
		{"wrong action please", "wrong_action"},
		{"99", ""},
		{"garbage", ""},
		{"", ""},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := ButtonForReply(tc.input, buttons)
			if got != tc.want {
				t.Fatalf("ButtonForReply(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestParseJIDAcceptsPhoneAndFullJID(t *testing.T) {
	cases := []struct {
		input    string
		wantUser string
	}{
		{"+61400000000", "61400000000"},
		{"61400000000", "61400000000"},
		{"61400000000@s.whatsapp.net", "61400000000"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			jid, err := parseJID(tc.input)
			if err != nil {
				t.Fatalf("parseJID(%q) err = %v", tc.input, err)
			}
			if jid.User != tc.wantUser {
				t.Fatalf("parseJID(%q).User = %s, want %s", tc.input, jid.User, tc.wantUser)
			}
		})
	}
}

func TestNewRequiresPostgresAndDispatcher(t *testing.T) {
	if _, err := New(context.Background(), Config{}); err == nil {
		t.Fatal("expected error for empty config")
	}
	if _, err := New(context.Background(), Config{PostgresDSN: "x"}); err == nil {
		t.Fatal("expected error for missing inbound dispatcher")
	}
}

// TestSessionStatusEnumExhaustive guards against drift between domain enum
// and the manager's published states.
func TestSessionStatusEnumExhaustive(t *testing.T) {
	expected := []domain.SessionStatus{
		domain.SessionQRNeeded,
		domain.SessionConnecting,
		domain.SessionActive,
		domain.SessionDisconnected,
		domain.SessionSuspended,
	}
	for _, s := range expected {
		if string(s) == "" {
			t.Fatalf("session status %q maps to empty", s)
		}
	}
	if domain.SessionUnknown != "" {
		t.Fatal("SessionUnknown must be empty string sentinel")
	}
}

func TestA0OutboundGuardBlocksCustomerRecipients(t *testing.T) {
	auditor := &captureAuditor{}
	sender := &captureSender{}
	guarded := NewGuardedAdapter(sender, auditor)
	binding := domain.ChannelBinding{
		TenantID:       "t_1",
		Channel:        string(channel.ChannelWhatsApp),
		ProviderNumber: "+61400000000",
		InboundMode:    domain.InboundModeReadOnly,
		OperatorJID:    "61400000000@s.whatsapp.net",
	}

	_, err := guarded.SendOutboundWithBinding(context.Background(), binding, channel.OutboundMessage{
		TenantID:            "t_1",
		RecipientIdentifier: "85299999999@s.whatsapp.net",
		BodyText:            "must not send",
		IdempotencyKey:      "blocked-1",
	})
	if err == nil {
		t.Fatal("expected guard error for customer recipient")
	}
	if len(sender.sent) != 0 {
		t.Fatalf("guarded adapter sent blocked message: %+v", sender.sent)
	}
	if len(auditor.events) != 1 || auditor.events[0].EventType != "outbound_blocked_to_customer" {
		t.Fatalf("audit events = %+v, want outbound_blocked_to_customer", auditor.events)
	}
	if auditor.events[0].Payload["dst_jid"] != "85299999999@s.whatsapp.net" {
		t.Fatalf("audit payload = %+v", auditor.events[0].Payload)
	}

	if _, err := guarded.SendOutboundWithBinding(context.Background(), binding, channel.OutboundMessage{
		TenantID:            "t_1",
		RecipientIdentifier: "61400000000@s.whatsapp.net",
		BodyText:            "allowed to operator",
		IdempotencyKey:      "allowed-1",
	}); err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("allowed send count = %d, want 1", len(sender.sent))
	}

	// Per OQ-2 RESOLVED, the operator can choose a separate draft-delivery JID.
	// The guard must allow that destination, otherwise A0 approval flow breaks.
	bindingWithDraft := binding
	bindingWithDraft.DraftDeliveryJID = "85270000000@s.whatsapp.net"
	if _, err := guarded.SendOutboundWithBinding(context.Background(), bindingWithDraft, channel.OutboundMessage{
		TenantID:            "t_1",
		RecipientIdentifier: "85270000000@s.whatsapp.net",
		BodyText:            "approved draft for forwarding",
		IdempotencyKey:      "allowed-draft",
	}); err != nil {
		t.Fatalf("draft delivery JID blocked: %v", err)
	}
	if len(sender.sent) != 2 {
		t.Fatalf("after draft delivery, sent = %d, want 2", len(sender.sent))
	}
}

type captureSender struct {
	sent []channel.OutboundMessage
}

func (s *captureSender) SendOutbound(ctx context.Context, msg channel.OutboundMessage) (channel.DeliveryReceipt, error) {
	s.sent = append(s.sent, msg)
	return channel.DeliveryReceipt{ProviderMessageID: "wa:test", SentAt: time.Now()}, nil
}

type captureAuditor struct {
	events []domain.AuditEvent
}

func (a *captureAuditor) RecordOutboundBlocked(ctx context.Context, tenantID, dstJID, bodyHash, callSite string) error {
	a.events = append(a.events, domain.AuditEvent{
		TenantID:  tenantID,
		EventType: "outbound_blocked_to_customer",
		Payload: map[string]any{
			"dst_jid":         dstJID,
			"body_hash":       bodyHash,
			"call_site_stack": callSite,
		},
	})
	return nil
}

// TestSentIDsBoundedMembership covers the echo-filter set: membership, bounded
// eviction (so it can't grow without limit), and that empty IDs never match.
func TestSentIDsBoundedMembership(t *testing.T) {
	s := newSentIDs(3)
	if s.has("a") {
		t.Fatal("empty set should not contain 'a'")
	}
	s.add("a")
	s.add("b")
	s.add("c")
	for _, id := range []string{"a", "b", "c"} {
		if !s.has(id) {
			t.Fatalf("set should contain %q", id)
		}
	}
	// A 4th entry evicts the oldest ('a') — the ring holds at most 3.
	s.add("d")
	if s.has("a") {
		t.Fatal("oldest entry 'a' should have been evicted")
	}
	if !s.has("b") || !s.has("c") || !s.has("d") {
		t.Fatal("b, c, d should all be present after evicting a")
	}
	// Empty IDs are ignored on both add and has.
	s.add("")
	if s.has("") {
		t.Fatal("empty id must never be a member")
	}
}
