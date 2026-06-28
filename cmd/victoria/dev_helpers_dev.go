//go:build dev

package main

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/jessepcc/victoria/internal/app"
	"github.com/jessepcc/victoria/internal/channel"
)

// devWAAdapter is a stub channel.Adapter used by demo flows when the real
// whatsmeow Manager is not configured (no postgres DSN). It records every
// outbound send in memory so audit events fire and the operator-bound paths
// (A0 drafts, A1 customer replies) are observable end-to-end.
type devWAAdapter struct {
	mu   sync.Mutex
	sent []channel.OutboundMessage
}

func (*devWAAdapter) Channel() channel.Channel { return channel.ChannelWhatsApp }
func (a *devWAAdapter) SendOutbound(_ context.Context, msg channel.OutboundMessage) (channel.DeliveryReceipt, error) {
	a.mu.Lock()
	a.sent = append(a.sent, msg)
	a.mu.Unlock()
	return channel.DeliveryReceipt{ProviderMessageID: "wa-dev:" + msg.PacketID, SentAt: time.Now().UTC()}, nil
}
func (*devWAAdapter) NormalizeInboundWebhook([]byte) ([]channel.InboundMessage, error) {
	return nil, nil
}

// enableDevHelpers is invoked unconditionally from main.go. In dev builds it
// registers a fake WhatsApp adapter when no real Manager is up and logs the
// loud-warning banner. The production stub (dev_helpers_stub.go) is a no-op,
// so a binary built without `-tags dev` cannot accidentally activate any of
// this even with a misconfigured environment.
func enableDevHelpers(application *app.App, hasRealWA bool) {
	log.Print("WARNING: dev build (-tags dev) — DO NOT USE IN PRODUCTION")
	log.Print("         /admin/dev/whatsapp/{inbound,session-status} are mounted; they can impersonate any operator (IsFromMe).")
	if !hasRealWA {
		application.RegisterChannelAdapter(&devWAAdapter{})
		log.Print("         dev WhatsApp adapter registered (records sends in memory).")
	}
}
