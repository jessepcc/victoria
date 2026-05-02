package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	waLog "go.mau.fi/whatsmeow/util/log"

	"victoria/internal/app"
	"victoria/internal/channel"
	"victoria/internal/channel/telegram"
	"victoria/internal/channel/whatsapp"
	"victoria/internal/domain"
	"victoria/internal/httpapi"
	"victoria/internal/store/memory"
	"victoria/internal/store/postgres"
)

func main() {
	ctx := context.Background()
	addr := os.Getenv("VICTORIA_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	var store app.Store
	dsn := os.Getenv("VICTORIA_DATABASE_URL")
	if dsn != "" {
		pgStore, err := postgres.Connect(ctx, dsn)
		if err != nil {
			log.Fatal(err)
		}
		defer pgStore.Close()
		store = pgStore
		log.Print("using postgres store")
	} else {
		store = memory.New()
		log.Print("using in-memory store")
	}
	application := app.New(store)

	application.RegisterChannelAdapter(telegram.New())

	var waManager *whatsapp.Manager
	if dsn != "" && os.Getenv("VICTORIA_WHATSAPP_DISABLED") != "1" {
		mgr, err := whatsapp.New(ctx, whatsapp.Config{
			PostgresDSN: dsn,
			Logger:      waLog.Stdout("WA", "DEBUG", true),
			OnSession: func(u whatsapp.SessionUpdate) {
				log.Printf("whatsapp session: tenant=%s status=%s jid=%s", u.TenantID, u.Status, u.JID)
				application.NotifyChannelSession(ctx, u.TenantID, channel.ChannelWhatsApp, u.Status)
			},
			BindingForTenant: func(c context.Context, tenantID string) (domain.ChannelBinding, error) {
				return application.GetChannelBinding(c, tenantID, string(channel.ChannelWhatsApp))
			},
			AuditOutboundBlocked: func(c context.Context, tenantID, dstJID, bodyHash, callSite string) error {
				return application.RecordOutboundBlocked(c, tenantID, dstJID, bodyHash, callSite)
			},
			Inbound: func(c context.Context, tenantID string, m channel.InboundMessage) error {
				return application.HandleWhatsAppInbound(c, tenantID, m)
			},
		})
		if err != nil {
			log.Fatalf("whatsapp manager: %v", err)
		}
		waManager = mgr
		bindings, err := store.ListChannelBindingsByChannel(ctx, string(channel.ChannelWhatsApp))
		if err != nil {
			log.Printf("warning: list whatsapp bindings: %v", err)
		}
		if err := waManager.Restore(ctx, bindings); err != nil {
			log.Printf("warning: whatsapp restore: %v", err)
		}
		application.RegisterChannelAdapter(waManager)
		log.Print("whatsapp adapter active")
		defer waManager.Close()
	} else {
		log.Print("whatsapp adapter disabled (no postgres or VICTORIA_WHATSAPP_DISABLED=1)")
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           httpapi.New(application, waManager).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("victoria listening on %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
	_ = domain.SessionActive // keep import
}
