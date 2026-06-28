package whatsapp_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jessepcc/victoria/internal/app"
	"github.com/jessepcc/victoria/internal/channel"
	"github.com/jessepcc/victoria/internal/channel/whatsapp"
	"github.com/jessepcc/victoria/internal/domain"
	"github.com/jessepcc/victoria/internal/store/postgres"
)

// TestPRIV2SweeperPurgesNonAllowlistedSecrets verifies the spec §5.7 / OQ-1
// retention sweep against a real Postgres + whatsmeow schema. We seed a
// tenant in A0 mode with one allowlisted customer JID, populate
// whatsmeow_message_secrets with a mix of allowlisted, operator-self, and
// non-allowlisted (incl. group-chat) secret rows, then run SweepNow and
// assert the right rows survived.
func TestPRIV2SweeperPurgesNonAllowlistedSecrets(t *testing.T) {
	dsn := os.Getenv("VICTORIA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set VICTORIA_TEST_DATABASE_URL to run Postgres integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	resetSchema(t, pool)

	store := postgres.New(pool)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	// Seed a tenant in A0 with one allowlisted customer.
	application := app.New(store)
	tenant, _, err := application.ProvisionTenant(ctx, "Demo", "roofing", "+61400000000", "op:demo")
	if err != nil {
		t.Fatal(err)
	}
	if err := application.AcknowledgeWhatsAppConsent(ctx, tenant.ID, app.AcknowledgeWhatsAppConsentInput{
		InboundMode:      domain.InboundModeReadOnly,
		DraftDeliveryJID: "+61400000000",
		OperatorJID:      "+61400000000",
	}); err != nil {
		t.Fatal(err)
	}
	if err := application.AddWhatsAppCustomer(ctx, tenant.ID, "+85299999999"); err != nil {
		t.Fatal(err)
	}

	// Stand up the whatsmeow schema (Manager.New runs container.Upgrade).
	deviceJID := "61400000000@s.whatsapp.net"
	customerJID := "85299999999@s.whatsapp.net"
	friendJID := "999111222333@s.whatsapp.net"
	groupJID := "120363408000000000@g.us"

	mustExec(t, pool, ctx, `CREATE TABLE IF NOT EXISTS whatsmeow_device (jid TEXT PRIMARY KEY)`)
	mustExec(t, pool, ctx, `CREATE TABLE IF NOT EXISTS whatsmeow_message_secrets (
		our_jid TEXT NOT NULL,
		chat_jid TEXT NOT NULL,
		sender_jid TEXT NOT NULL,
		message_id TEXT NOT NULL,
		key BYTEA NOT NULL,
		PRIMARY KEY (our_jid, chat_jid, sender_jid, message_id),
		FOREIGN KEY (our_jid) REFERENCES whatsmeow_device(jid) ON UPDATE CASCADE ON DELETE CASCADE
	)`)
	mustExec(t, pool, ctx, `INSERT INTO whatsmeow_device (jid) VALUES ($1) ON CONFLICT DO NOTHING`, deviceJID)
	insert := func(chatJID, senderJID, msgID string) {
		t.Helper()
		if _, err := pool.Exec(ctx,
			`INSERT INTO whatsmeow_message_secrets (our_jid, chat_jid, sender_jid, message_id, key)
			 VALUES ($1, $2, $3, $4, $5)`,
			deviceJID, chatJID, senderJID, msgID, []byte{0x01, 0x02}); err != nil {
			t.Fatal(err)
		}
	}
	// Customer chat (allowlisted) — must survive.
	insert(customerJID, customerJID, "msg-customer-1")
	insert(customerJID, customerJID, "msg-customer-2")
	// Operator self-chat — must survive.
	insert(deviceJID, deviceJID, "msg-self-1")
	// Friend in a 1:1 chat (NOT allowlisted) — must purge.
	insert(friendJID, friendJID, "msg-friend-1")
	insert(friendJID, friendJID, "msg-friend-2")
	// Group chat — never allowlistable, must purge.
	insert(groupJID, friendJID, "msg-group-1")
	insert(groupJID, customerJID, "msg-group-2")

	// Run the sweep.
	sweeper := whatsapp.NewPRIV2Sweeper(pool, nil)
	if err := sweeper.SweepNow(ctx); err != nil {
		t.Fatal(err)
	}

	// Verify the expected survivors.
	var surviving []string
	rows, err := pool.Query(ctx,
		`SELECT chat_jid || '|' || message_id FROM whatsmeow_message_secrets WHERE our_jid = $1 ORDER BY 1`,
		deviceJID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		surviving = append(surviving, s)
	}
	want := []string{
		"61400000000@s.whatsapp.net|msg-self-1",
		"85299999999@s.whatsapp.net|msg-customer-1",
		"85299999999@s.whatsapp.net|msg-customer-2",
	}
	if !equalStrings(surviving, want) {
		t.Fatalf("survivors = %v, want %v", surviving, want)
	}
}

// TestPRIV2SweeperSkipsA1Tenants verifies that A1 (full_control) tenants
// are not touched — A1 only ever sees customer traffic on its dedicated
// number, so there's nothing to purge and the sweep would otherwise risk
// deleting legitimate customer-decryption material.
func TestPRIV2SweeperSkipsA1Tenants(t *testing.T) {
	dsn := os.Getenv("VICTORIA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set VICTORIA_TEST_DATABASE_URL to run Postgres integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	resetSchema(t, pool)

	store := postgres.New(pool)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	application := app.New(store)
	tenant, _, err := application.ProvisionTenant(ctx, "Premium", "roofing", "+61400000001", "op:premium")
	if err != nil {
		t.Fatal(err)
	}
	if err := application.AcknowledgeWhatsAppConsent(ctx, tenant.ID, app.AcknowledgeWhatsAppConsentInput{
		InboundMode: domain.InboundModeFullControl,
	}); err != nil {
		t.Fatal(err)
	}
	deviceJID := "61400000001@s.whatsapp.net"
	mustExec(t, pool, ctx, `CREATE TABLE IF NOT EXISTS whatsmeow_device (jid TEXT PRIMARY KEY)`)
	mustExec(t, pool, ctx, `CREATE TABLE IF NOT EXISTS whatsmeow_message_secrets (
		our_jid TEXT NOT NULL,
		chat_jid TEXT NOT NULL,
		sender_jid TEXT NOT NULL,
		message_id TEXT NOT NULL,
		key BYTEA NOT NULL,
		PRIMARY KEY (our_jid, chat_jid, sender_jid, message_id),
		FOREIGN KEY (our_jid) REFERENCES whatsmeow_device(jid) ON UPDATE CASCADE ON DELETE CASCADE
	)`)
	mustExec(t, pool, ctx, `INSERT INTO whatsmeow_device (jid) VALUES ($1) ON CONFLICT DO NOTHING`, deviceJID)
	mustExec(t, pool, ctx,
		`INSERT INTO whatsmeow_message_secrets (our_jid, chat_jid, sender_jid, message_id, key)
		 VALUES ($1, '85288888888@s.whatsapp.net', '85288888888@s.whatsapp.net', 'm1', $2)`,
		deviceJID, []byte{0x01})

	sweeper := whatsapp.NewPRIV2Sweeper(pool, nil)
	if err := sweeper.SweepNow(ctx); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM whatsmeow_message_secrets WHERE our_jid = $1`, deviceJID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("A1 secrets remaining = %d, want 1 (sweep must skip full_control tenants)", n)
	}
	// Marker for a tenant we observed was full_control.
	_ = channel.ChannelWhatsApp
}

func resetSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	stmts := []string{
		`DROP VIEW IF EXISTS mcp_approval_events`,
		`DROP TABLE IF EXISTS active_skill_versions`,
		`DROP TABLE IF EXISTS skill_versions`,
		`DROP TABLE IF EXISTS validated_rules`,
		`DROP TABLE IF EXISTS rule_candidates`,
		`DROP TABLE IF EXISTS corrections`,
		`DROP TABLE IF EXISTS signals`,
		`DROP TABLE IF EXISTS outbound_to_customer`,
		`DROP TABLE IF EXISTS customer_messages`,
		`DROP TABLE IF EXISTS audit_events`,
		`DROP TABLE IF EXISTS review_packets`,
		`DROP TABLE IF EXISTS artifacts`,
		`DROP TABLE IF EXISTS decision_points`,
		`DROP TABLE IF EXISTS case_runs`,
		`DROP TABLE IF EXISTS outbound_queue`,
		`DROP TABLE IF EXISTS channel_bindings`,
		`DROP TABLE IF EXISTS workflow_templates`,
		`DROP TABLE IF EXISTS tenants`,
		`DROP TABLE IF EXISTS whatsmeow_message_secrets`,
		`DROP TABLE IF EXISTS whatsmeow_device`,
		`DROP FUNCTION IF EXISTS victoria_reject_audit_events_mutation()`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
}

func mustExec(t *testing.T, pool *pgxpool.Pool, ctx context.Context, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
