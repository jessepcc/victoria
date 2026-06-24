package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	waLog "go.mau.fi/whatsmeow/util/log"

	"github.com/jessepcc/victoria/internal/app"
	"github.com/jessepcc/victoria/internal/channel"
	"github.com/jessepcc/victoria/internal/channel/telegram"
	"github.com/jessepcc/victoria/internal/channel/whatsapp"
	"github.com/jessepcc/victoria/internal/domain"
	"github.com/jessepcc/victoria/internal/httpapi"
	"github.com/jessepcc/victoria/internal/store/memory"
	"github.com/jessepcc/victoria/internal/store/postgres"
)

func main() {
	// A signal-cancellable context drives graceful shutdown: it cancels on
	// SIGINT/SIGTERM, draining the HTTP server and stopping the background
	// PRIV-2 sweeper before deferred Close() calls run.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	addr := os.Getenv("VICTORIA_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	var store app.Store
	var pgStore *postgres.Store
	dsn := os.Getenv("VICTORIA_DATABASE_URL")
	if dsn != "" {
		s, err := postgres.Connect(ctx, dsn)
		if err != nil {
			log.Fatal(err)
		}
		defer s.Close()
		pgStore = s
		store = s
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
		// Spec §5.7 PRIV-2: bound the worst-case retention window for
		// non-allowlisted senders' decryption keys. Cadence 15 min ⇒ at most
		// ~30 min between a non-customer message arriving and its
		// whatsmeow_message_secrets row being purged.
		sweeper := whatsapp.NewPRIV2Sweeper(pgStore.Pool(), waLog.Stdout("PRIV2", "INFO", true))
		go sweeper.Run(ctx)
		log.Print("priv2 retention sweeper running (cadence 15m)")
	} else {
		log.Print("whatsapp adapter disabled (no postgres or VICTORIA_WHATSAPP_DISABLED=1)")
	}

	apiServer := httpapi.New(application, waManager)
	gwToken := os.Getenv("VICTORIA_GATEWAY_INBOUND_TOKEN")
	if gwToken == "" {
		log.Fatal("VICTORIA_GATEWAY_INBOUND_TOKEN is required (authenticates Telegram-style webhook posts to /gateway/inbound)")
	}
	apiServer.SetGatewayInboundToken(gwToken)
	adminToken := os.Getenv("VICTORIA_ADMIN_TOKEN")
	if adminToken == "" {
		log.Fatal("VICTORIA_ADMIN_TOKEN is required (authenticates privileged /admin/* control-plane routes)")
	}
	apiServer.SetAdminToken(adminToken)
	// In production builds (no `-tags dev`) this is a no-op. In dev builds it
	// registers the fake WhatsApp adapter when no real Manager is up and prints
	// the warning banner — see dev_helpers_dev.go.
	enableDevHelpers(application, waManager != nil)
	server := &http.Server{
		Addr:              addr,
		Handler:           apiServer.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
	}

	go func() {
		log.Printf("victoria listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-ctx.Done()
	stop() // restore default signal handling: a second Ctrl-C force-quits
	log.Print("shutdown signal received; draining connections...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown error: %v", err)
	}
}
