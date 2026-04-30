package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"victoria/internal/domain"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func Connect(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	store := New(pool)
	if err := store.Migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS tenants (
	id TEXT PRIMARY KEY,
	data JSONB NOT NULL
);
CREATE TABLE IF NOT EXISTS workflow_templates (
	slug TEXT PRIMARY KEY,
	data JSONB NOT NULL
);
CREATE TABLE IF NOT EXISTS channel_bindings (
	channel TEXT NOT NULL,
	provider_number TEXT NOT NULL,
	tenant_id TEXT NOT NULL,
	data JSONB NOT NULL,
	PRIMARY KEY (channel, provider_number)
);
CREATE INDEX IF NOT EXISTS channel_bindings_tenant_idx ON channel_bindings (tenant_id);
CREATE TABLE IF NOT EXISTS case_runs (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	workflow_slug TEXT NOT NULL,
	data JSONB NOT NULL
);
CREATE INDEX IF NOT EXISTS case_runs_tenant_idx ON case_runs (tenant_id);
CREATE TABLE IF NOT EXISTS decision_points (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	case_run_id TEXT NOT NULL,
	decision_type TEXT NOT NULL,
	data JSONB NOT NULL
);
CREATE INDEX IF NOT EXISTS decision_points_tenant_idx ON decision_points (tenant_id);
CREATE TABLE IF NOT EXISTS artifacts (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	case_run_id TEXT NOT NULL,
	data JSONB NOT NULL
);
CREATE INDEX IF NOT EXISTS artifacts_case_idx ON artifacts (tenant_id, case_run_id);
CREATE TABLE IF NOT EXISTS review_packets (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	data JSONB NOT NULL
);
CREATE INDEX IF NOT EXISTS review_packets_tenant_idx ON review_packets (tenant_id);
CREATE TABLE IF NOT EXISTS audit_events (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	event_type TEXT NOT NULL,
	ref_id TEXT NOT NULL,
	idempotency_key TEXT NOT NULL UNIQUE,
	occurred_at TIMESTAMPTZ NOT NULL,
	data JSONB NOT NULL
);
CREATE INDEX IF NOT EXISTS audit_events_tenant_idx ON audit_events (tenant_id, occurred_at);
CREATE INDEX IF NOT EXISTS audit_events_approval_idx ON audit_events (tenant_id, event_type, ref_id);
CREATE OR REPLACE FUNCTION victoria_reject_audit_events_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
	RAISE EXCEPTION 'audit_events is insert-only';
END;
$$;
DROP TRIGGER IF EXISTS audit_events_insert_only ON audit_events;
CREATE TRIGGER audit_events_insert_only
BEFORE UPDATE OR DELETE OR TRUNCATE ON audit_events
FOR EACH STATEMENT EXECUTE FUNCTION victoria_reject_audit_events_mutation();
CREATE OR REPLACE VIEW mcp_approval_events AS
SELECT
	tenant_id,
	ref_id AS decision_point_id,
	data->'related_ids'->>'case_run_id' AS case_run_id,
	occurred_at
FROM audit_events
WHERE event_type='approval_received';
CREATE TABLE IF NOT EXISTS signals (
	signal_id TEXT PRIMARY KEY
);
CREATE TABLE IF NOT EXISTS corrections (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	idempotency_key TEXT NOT NULL UNIQUE,
	created_at TIMESTAMPTZ NOT NULL,
	data JSONB NOT NULL
);
CREATE INDEX IF NOT EXISTS corrections_tenant_idx ON corrections (tenant_id, created_at);
CREATE TABLE IF NOT EXISTS rule_candidates (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	workflow_slug TEXT NOT NULL,
	decision_type TEXT NOT NULL,
	conditions_hash TEXT NOT NULL,
	recommended_action TEXT NOT NULL,
	status TEXT NOT NULL,
	data JSONB NOT NULL
);
CREATE INDEX IF NOT EXISTS rule_candidates_lookup_idx ON rule_candidates (tenant_id, workflow_slug, decision_type, conditions_hash, recommended_action);
CREATE TABLE IF NOT EXISTS validated_rules (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL DEFAULT '',
	workflow_slug TEXT NOT NULL,
	decision_type TEXT NOT NULL,
	conditions_hash TEXT NOT NULL,
	status TEXT NOT NULL,
	scope TEXT NOT NULL,
	vertical TEXT NOT NULL DEFAULT '',
	promoted_at TIMESTAMPTZ NOT NULL,
	data JSONB NOT NULL
);
CREATE INDEX IF NOT EXISTS validated_rules_visible_idx ON validated_rules (workflow_slug, status, scope, tenant_id, vertical);
CREATE TABLE IF NOT EXISTS skill_versions (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	workflow_slug TEXT NOT NULL,
	version INT NOT NULL,
	status TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	data JSONB NOT NULL
);
CREATE INDEX IF NOT EXISTS skill_versions_tenant_idx ON skill_versions (tenant_id, workflow_slug, status);
CREATE TABLE IF NOT EXISTS active_skill_versions (
	tenant_id TEXT NOT NULL,
	workflow_slug TEXT NOT NULL,
	skill_version_id TEXT NOT NULL REFERENCES skill_versions(id),
	PRIMARY KEY (tenant_id, workflow_slug)
);
`)
	return err
}

func (s *Store) CreateTenant(ctx context.Context, tenant domain.Tenant, _ domain.ProvisioningManifest, binding domain.ChannelBinding, initialSkillVersion domain.SkillVersion) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	if _, err := tx.Exec(ctx, `INSERT INTO tenants (id, data) VALUES ($1, $2)`, tenant.ID, mustJSON(tenant)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO channel_bindings (channel, provider_number, tenant_id, data) VALUES ($1, $2, $3, $4)`, binding.Channel, binding.ProviderNumber, binding.TenantID, mustJSON(binding)); err != nil {
		return err
	}
	if err := insertSkillVersion(ctx, tx, initialSkillVersion); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO active_skill_versions (tenant_id, workflow_slug, skill_version_id) VALUES ($1, $2, $3)`, initialSkillVersion.TenantID, initialSkillVersion.WorkflowSlug, initialSkillVersion.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) GetTenant(ctx context.Context, tenantID string) (domain.Tenant, error) {
	return oneJSON[domain.Tenant](ctx, s.pool, `SELECT data FROM tenants WHERE id=$1`, tenantID)
}

func (s *Store) ListTenants(ctx context.Context) ([]domain.Tenant, error) {
	return manyJSON[domain.Tenant](ctx, s.pool, `SELECT data FROM tenants ORDER BY id`)
}

func (s *Store) UpsertWorkflowTemplate(ctx context.Context, tmpl domain.WorkflowTemplate) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO workflow_templates (slug, data) VALUES ($1, $2) ON CONFLICT (slug) DO UPDATE SET data=EXCLUDED.data`, tmpl.Slug, mustJSON(tmpl))
	return err
}

func (s *Store) GetWorkflowTemplate(ctx context.Context, slug string) (domain.WorkflowTemplate, error) {
	return oneJSON[domain.WorkflowTemplate](ctx, s.pool, `SELECT data FROM workflow_templates WHERE slug=$1`, slug)
}

func (s *Store) UpsertChannelBinding(ctx context.Context, binding domain.ChannelBinding) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO channel_bindings (channel, provider_number, tenant_id, data) VALUES ($1,$2,$3,$4) ON CONFLICT (channel, provider_number) DO UPDATE SET tenant_id=EXCLUDED.tenant_id, data=EXCLUDED.data`, binding.Channel, binding.ProviderNumber, binding.TenantID, mustJSON(binding))
	return err
}

func (s *Store) ChannelBindingByProvider(ctx context.Context, channel, providerNumber string) (domain.ChannelBinding, error) {
	return oneJSON[domain.ChannelBinding](ctx, s.pool, `SELECT data FROM channel_bindings WHERE channel=$1 AND provider_number=$2`, channel, providerNumber)
}

func (s *Store) CreateCaseRun(ctx context.Context, run domain.CaseRun) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO case_runs (id, tenant_id, workflow_slug, data) VALUES ($1,$2,$3,$4)`, run.ID, run.TenantID, run.WorkflowSlug, mustJSON(run))
	return err
}

func (s *Store) UpdateCaseRunStatus(ctx context.Context, tenantID, caseRunID, status string) error {
	run, err := s.GetCaseRun(ctx, tenantID, caseRunID)
	if err != nil {
		return err
	}
	run.Status = status
	tag, err := s.pool.Exec(ctx, `UPDATE case_runs SET data=$1 WHERE id=$2 AND tenant_id=$3`, mustJSON(run), caseRunID, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) GetCaseRun(ctx context.Context, tenantID, caseRunID string) (domain.CaseRun, error) {
	return oneJSON[domain.CaseRun](ctx, s.pool, `SELECT data FROM case_runs WHERE id=$1 AND tenant_id=$2`, caseRunID, tenantID)
}

func (s *Store) CreateDecisionPoint(ctx context.Context, point domain.DecisionPoint) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO decision_points (id, tenant_id, case_run_id, decision_type, data) VALUES ($1,$2,$3,$4,$5)`, point.ID, point.TenantID, point.CaseRunID, point.DecisionType, mustJSON(point))
	return err
}

func (s *Store) UpdateDecisionPointStatus(ctx context.Context, tenantID, decisionPointID, status string) error {
	point, err := s.GetDecisionPoint(ctx, tenantID, decisionPointID)
	if err != nil {
		return err
	}
	point.Status = status
	tag, err := s.pool.Exec(ctx, `UPDATE decision_points SET data=$1 WHERE id=$2 AND tenant_id=$3`, mustJSON(point), decisionPointID, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) GetDecisionPoint(ctx context.Context, tenantID, decisionPointID string) (domain.DecisionPoint, error) {
	return oneJSON[domain.DecisionPoint](ctx, s.pool, `SELECT data FROM decision_points WHERE id=$1 AND tenant_id=$2`, decisionPointID, tenantID)
}

func (s *Store) CreateArtifact(ctx context.Context, artifact domain.Artifact) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO artifacts (id, tenant_id, case_run_id, data) VALUES ($1,$2,$3,$4)`, artifact.ID, artifact.TenantID, artifact.CaseRunID, mustJSON(artifact))
	return err
}

func (s *Store) ListArtifacts(ctx context.Context, tenantID, caseRunID string) ([]domain.Artifact, error) {
	return manyJSON[domain.Artifact](ctx, s.pool, `SELECT data FROM artifacts WHERE tenant_id=$1 AND case_run_id=$2 ORDER BY id`, tenantID, caseRunID)
}

func (s *Store) CreateReviewPacket(ctx context.Context, packet domain.ReviewPacket) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO review_packets (id, tenant_id, data) VALUES ($1,$2,$3)`, packet.PacketID, packet.TenantID, mustJSON(packet))
	return err
}

func (s *Store) GetReviewPacket(ctx context.Context, tenantID, packetID string) (domain.ReviewPacket, error) {
	return oneJSON[domain.ReviewPacket](ctx, s.pool, `SELECT data FROM review_packets WHERE id=$1 AND tenant_id=$2`, packetID, tenantID)
}

func (s *Store) MarkReviewPacketDelivered(ctx context.Context, tenantID, packetID string) error {
	packet, err := s.GetReviewPacket(ctx, tenantID, packetID)
	if err != nil {
		return err
	}
	packet.Delivered = true
	tag, err := s.pool.Exec(ctx, `UPDATE review_packets SET data=$1 WHERE id=$2 AND tenant_id=$3`, mustJSON(packet), packetID, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) CreateAuditEvent(ctx context.Context, event domain.AuditEvent) (bool, error) {
	tag, err := s.pool.Exec(ctx, `INSERT INTO audit_events (id, tenant_id, event_type, ref_id, idempotency_key, occurred_at, data) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (idempotency_key) DO NOTHING`, event.ID, event.TenantID, event.EventType, event.RefID, event.IdempotencyKey, event.OccurredAt, mustJSON(event))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) ListAuditEvents(ctx context.Context, tenantID string) ([]domain.AuditEvent, error) {
	if tenantID == "" {
		return manyJSON[domain.AuditEvent](ctx, s.pool, `SELECT data FROM audit_events ORDER BY occurred_at, id`)
	}
	return manyJSON[domain.AuditEvent](ctx, s.pool, `SELECT data FROM audit_events WHERE tenant_id=$1 ORDER BY occurred_at, id`, tenantID)
}

func (s *Store) HasApprovalAudit(ctx context.Context, tenantID, caseRunID, decisionPointID string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM audit_events
		WHERE tenant_id=$1 AND event_type='approval_received' AND ref_id=$2 AND data->'related_ids'->>'case_run_id'=$3
	)`, tenantID, decisionPointID, caseRunID).Scan(&exists)
	return exists, err
}

func (s *Store) SeenSignal(ctx context.Context, signalID string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM signals WHERE signal_id=$1)`, signalID).Scan(&exists)
	return exists, err
}

func (s *Store) MarkSignalSeen(ctx context.Context, signalID string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO signals (signal_id) VALUES ($1) ON CONFLICT DO NOTHING`, signalID)
	return err
}

func (s *Store) CreateCorrection(ctx context.Context, correction domain.Correction) (bool, error) {
	tag, err := s.pool.Exec(ctx, `INSERT INTO corrections (id, tenant_id, idempotency_key, created_at, data) VALUES ($1,$2,$3,$4,$5) ON CONFLICT (idempotency_key) DO NOTHING`, correction.ID, correction.TenantID, correction.IdempotencyKey, correction.CreatedAt, mustJSON(correction))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) ListCorrections(ctx context.Context, tenantID string) ([]domain.Correction, error) {
	return manyJSON[domain.Correction](ctx, s.pool, `SELECT data FROM corrections WHERE tenant_id=$1 ORDER BY created_at, id`, tenantID)
}

func (s *Store) FindCandidate(ctx context.Context, tenantID, workflowSlug, decisionType, conditionsHash string, recommendedAction string) (domain.RuleCandidate, error) {
	return oneJSON[domain.RuleCandidate](ctx, s.pool, `SELECT data FROM rule_candidates WHERE tenant_id=$1 AND workflow_slug=$2 AND decision_type=$3 AND conditions_hash=$4 AND recommended_action=$5 AND status <> 'rejected' ORDER BY id LIMIT 1`, tenantID, workflowSlug, decisionType, conditionsHash, recommendedAction)
}

func (s *Store) ListCandidates(ctx context.Context, tenantID string) ([]domain.RuleCandidate, error) {
	return manyJSON[domain.RuleCandidate](ctx, s.pool, `SELECT data FROM rule_candidates WHERE tenant_id=$1 ORDER BY id`, tenantID)
}

func (s *Store) GetCandidate(ctx context.Context, tenantID, candidateID string) (domain.RuleCandidate, error) {
	return oneJSON[domain.RuleCandidate](ctx, s.pool, `SELECT data FROM rule_candidates WHERE id=$1 AND tenant_id=$2`, candidateID, tenantID)
}

func (s *Store) SaveCandidate(ctx context.Context, candidate domain.RuleCandidate) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO rule_candidates (id, tenant_id, workflow_slug, decision_type, conditions_hash, recommended_action, status, data) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
	ON CONFLICT (id) DO UPDATE SET status=EXCLUDED.status, data=EXCLUDED.data`, candidate.ID, candidate.TenantID, candidate.WorkflowSlug, candidate.DecisionType, candidate.ConditionsHash, candidate.RecommendedAction, candidate.Status, mustJSON(candidate))
	return err
}

func (s *Store) CreateValidatedRule(ctx context.Context, rule domain.ValidatedRule) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO validated_rules (id, tenant_id, workflow_slug, decision_type, conditions_hash, status, scope, vertical, promoted_at, data) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, rule.ID, rule.TenantID, rule.WorkflowSlug, rule.DecisionType, rule.ConditionsHash, rule.Status, string(rule.Scope), rule.Vertical, rule.PromotedAt, mustJSON(rule))
	return err
}

func (s *Store) DeprecateActiveRule(ctx context.Context, tenantID, workflowSlug, decisionType, conditionsHash string) (string, int, error) {
	rule, err := oneJSON[domain.ValidatedRule](ctx, s.pool, `SELECT data FROM validated_rules WHERE tenant_id=$1 AND workflow_slug=$2 AND decision_type=$3 AND conditions_hash=$4 AND status='active' ORDER BY promoted_at DESC LIMIT 1`, tenantID, workflowSlug, decisionType, conditionsHash)
	if errors.Is(err, domain.ErrNotFound) {
		return "", 1, nil
	}
	if err != nil {
		return "", 0, err
	}
	rule.Status = "deprecated"
	tag, err := s.pool.Exec(ctx, `UPDATE validated_rules SET status='deprecated', data=$1 WHERE id=$2`, mustJSON(rule), rule.ID)
	if err != nil {
		return "", 0, err
	}
	if tag.RowsAffected() == 0 {
		return "", 0, domain.ErrNotFound
	}
	return rule.ID, rule.Version + 1, nil
}

func (s *Store) ListVisibleValidatedRules(ctx context.Context, tenantID, workflowSlug string) ([]domain.ValidatedRule, error) {
	tenant, err := s.GetTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	rules, err := manyJSON[domain.ValidatedRule](ctx, s.pool, `SELECT data FROM validated_rules WHERE workflow_slug=$1 AND status='active' AND (scope='default' OR (scope='vertical' AND vertical=$2) OR tenant_id=$3)`, workflowSlug, tenant.Vertical, tenantID)
	if err != nil {
		return nil, err
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Scope != rules[j].Scope {
			return scopeRank(rules[i].Scope) > scopeRank(rules[j].Scope)
		}
		return rules[i].PromotedAt.After(rules[j].PromotedAt)
	})
	return rules, nil
}

func (s *Store) ListTenantValidatedRules(ctx context.Context, tenantID string) ([]domain.ValidatedRule, error) {
	return manyJSON[domain.ValidatedRule](ctx, s.pool, `SELECT data FROM validated_rules WHERE tenant_id=$1 OR scope='default' ORDER BY promoted_at`, tenantID)
}

func (s *Store) CreateSkillVersion(ctx context.Context, sv domain.SkillVersion) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	rows, err := tx.Query(ctx, `SELECT data FROM skill_versions WHERE tenant_id=$1 AND workflow_slug=$2 AND status='active'`, sv.TenantID, sv.WorkflowSlug)
	if err != nil {
		return err
	}
	var existing []domain.SkillVersion
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			rows.Close()
			return err
		}
		var current domain.SkillVersion
		if err := json.Unmarshal(data, &current); err != nil {
			rows.Close()
			return err
		}
		existing = append(existing, current)
	}
	rows.Close()
	if rows.Err() != nil {
		return rows.Err()
	}
	for _, current := range existing {
		current.Status = "deprecated"
		if _, err := tx.Exec(ctx, `UPDATE skill_versions SET status='deprecated', data=$1 WHERE id=$2`, mustJSON(current), current.ID); err != nil {
			return err
		}
	}
	if err := insertSkillVersion(ctx, tx, sv); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO active_skill_versions (tenant_id, workflow_slug, skill_version_id) VALUES ($1,$2,$3) ON CONFLICT (tenant_id, workflow_slug) DO UPDATE SET skill_version_id=EXCLUDED.skill_version_id`, sv.TenantID, sv.WorkflowSlug, sv.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) ActiveSkillVersion(ctx context.Context, tenantID, workflowSlug string) (domain.SkillVersion, error) {
	return oneJSON[domain.SkillVersion](ctx, s.pool, `SELECT sv.data FROM active_skill_versions a JOIN skill_versions sv ON sv.id=a.skill_version_id WHERE a.tenant_id=$1 AND a.workflow_slug=$2`, tenantID, workflowSlug)
}

func (s *Store) GetSkillVersion(ctx context.Context, tenantID, skillVersionID string) (domain.SkillVersion, error) {
	return oneJSON[domain.SkillVersion](ctx, s.pool, `SELECT data FROM skill_versions WHERE id=$1 AND tenant_id=$2`, skillVersionID, tenantID)
}

func insertSkillVersion(ctx context.Context, tx pgx.Tx, sv domain.SkillVersion) error {
	_, err := tx.Exec(ctx, `INSERT INTO skill_versions (id, tenant_id, workflow_slug, version, status, created_at, data) VALUES ($1,$2,$3,$4,$5,$6,$7)`, sv.ID, sv.TenantID, sv.WorkflowSlug, sv.Version, sv.Status, sv.CreatedAt, mustJSON(sv))
	return err
}

type queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func oneJSON[T any](ctx context.Context, q queryer, sql string, args ...any) (T, error) {
	var out T
	var data []byte
	if err := q.QueryRow(ctx, sql, args...).Scan(&data); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return out, domain.ErrNotFound
		}
		return out, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, err
	}
	return out, nil
}

func manyJSON[T any](ctx context.Context, q queryer, sql string, args ...any) ([]T, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var item T
		if err := json.Unmarshal(data, &item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func mustJSON(value any) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("marshal %T: %v", value, err))
	}
	return data
}

func rollback(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(ctx)
}

func scopeRank(scope domain.Scope) int {
	switch scope {
	case domain.ScopeCase:
		return 4
	case domain.ScopeTenant:
		return 3
	case domain.ScopeVertical:
		return 2
	default:
		return 1
	}
}

func QuoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
