package postgres_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"victoria/internal/app"
	"victoria/internal/domain"
	"victoria/internal/store/postgres"
)

func TestPostgresStoreCorrectionLoopAndAuditImmutability(t *testing.T) {
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
	mustResetSchema(t, pool)

	store := postgres.New(pool)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	application := app.New(store)
	tenant, _, err := application.ProvisionTenant(ctx, "ABC Roofing", "roofing", "+61400000000", "op_telegram:owner")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		_, packet, err := application.StartCase(ctx, app.StartCaseInput{
			TenantID:     tenant.ID,
			WorkflowSlug: "quote_drafting",
			Mode:         domain.ModeSandbox,
			Payload: map[string]any{
				"sandbox":         true,
				"client_type":     "new",
				"photos_complete": false,
				"case_number":     i,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = application.ReceiveOperatorReply(ctx, app.InboundReply{
			Channel:         "telegram",
			ProviderNumber:  "+61400000000",
			PacketID:        packet.PacketID,
			SourceMessageID: "msg-" + time.Now().Add(time.Duration(i)).Format("150405.000000000"),
			ActionButton:    domain.ActionWrongAction,
			FreeText:        "Should have held and asked for more photos.",
			FollowUpAnswer:  "always when client is new and photos are incomplete",
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	candidates, err := application.ListCandidates(ctx, tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].Status != "under_review" {
		t.Fatalf("candidates = %+v, want one under_review", candidates)
	}
	_, sv, err := application.PromoteCandidate(ctx, tenant.ID, candidates[0].ID, "reviewer:alice", "three matching corrections")
	if err != nil {
		t.Fatal(err)
	}
	if len(sv.RuleManifest) != 1 {
		t.Fatalf("skill manifest = %d, want 1", len(sv.RuleManifest))
	}
	if _, err := pool.Exec(ctx, `UPDATE audit_events SET event_type='tampered'`); err == nil {
		t.Fatal("audit_events update succeeded, want trigger rejection")
	}
}

func mustResetSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
DROP VIEW IF EXISTS mcp_approval_events;
DROP TABLE IF EXISTS active_skill_versions;
DROP TABLE IF EXISTS skill_versions;
DROP TABLE IF EXISTS validated_rules;
DROP TABLE IF EXISTS rule_candidates;
DROP TABLE IF EXISTS corrections;
DROP TABLE IF EXISTS signals;
DROP TABLE IF EXISTS audit_events;
DROP TABLE IF EXISTS review_packets;
DROP TABLE IF EXISTS artifacts;
DROP TABLE IF EXISTS decision_points;
DROP TABLE IF EXISTS case_runs;
DROP TABLE IF EXISTS channel_bindings;
DROP TABLE IF EXISTS workflow_templates;
DROP TABLE IF EXISTS tenants;
DROP FUNCTION IF EXISTS victoria_reject_audit_events_mutation();
`)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
