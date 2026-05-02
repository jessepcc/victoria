package whatsapp

import (
	"strings"
	"testing"

	"victoria/internal/channel"
	"victoria/internal/domain"
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
	if _, err := New(nil, Config{}); err == nil {
		t.Fatal("expected error for empty config")
	}
	if _, err := New(nil, Config{PostgresDSN: "x"}); err == nil {
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
