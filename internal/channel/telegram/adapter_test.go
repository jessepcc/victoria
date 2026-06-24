package telegram

import (
	"context"
	"testing"

	"github.com/jessepcc/victoria/internal/channel"
)

func TestNormalizeInboundExtractsButtonAndText(t *testing.T) {
	a := New()
	got, err := a.NormalizeInboundWebhook([]byte(`{
		"chat_id": "12345",
		"provider_number": "+61400000000",
		"message_id": "m1",
		"button_payload": "approve",
		"text": "approve",
		"timestamp": "2026-05-01T10:00:00Z"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got))
	}
	msg := got[0]
	if msg.SenderIdentifier != "+61400000000" {
		t.Fatalf("sender = %s", msg.SenderIdentifier)
	}
	if msg.ButtonPayload != "approve" || msg.FreeText != "approve" {
		t.Fatalf("payload/text = %+v", msg)
	}
	if msg.Channel != channel.ChannelTelegram {
		t.Fatalf("channel = %s", msg.Channel)
	}
}

func TestSendOutboundRequiresPacketID(t *testing.T) {
	a := New()
	if _, err := a.SendOutbound(context.Background(), channel.OutboundMessage{}); err == nil {
		t.Fatal("expected error for empty packet id")
	}
	receipt, err := a.SendOutbound(context.Background(), channel.OutboundMessage{PacketID: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ProviderMessageID != "tg:p1" {
		t.Fatalf("receipt = %+v", receipt)
	}
}
