package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"victoria/internal/domain"
	"victoria/internal/store/memory"
)

type sequenceIDs struct {
	next int
}

func (s *sequenceIDs) NewID(prefix string) string {
	s.next++
	return fmt.Sprintf("%s_%03d", prefix, s.next)
}

type mutableClock struct {
	now time.Time
}

func (c *mutableClock) Now() time.Time {
	return c.now
}

func (c *mutableClock) Add(d time.Duration) {
	c.now = c.now.Add(d)
}

func newTestApp(t *testing.T) (*App, *memory.Store, *mutableClock, domain.Tenant) {
	t.Helper()
	store := memory.New()
	clock := &mutableClock{now: time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)}
	application := New(store, WithIDs(&sequenceIDs{}), WithClock(clock))
	tenant, _, err := application.ProvisionTenant(context.Background(), "ABC Roofing", "roofing", "+61400000000", "op_telegram:owner")
	if err != nil {
		t.Fatal(err)
	}
	return application, store, clock, tenant
}

func quotePayload(caseName string) map[string]any {
	return map[string]any{
		"sandbox":         true,
		"case_name":       caseName,
		"client_type":     "new",
		"photos_complete": false,
	}
}

func rejectCase(t *testing.T, application *App, tenant domain.Tenant, caseName, sourceID string) {
	t.Helper()
	_, packet, err := application.StartCase(context.Background(), StartCaseInput{
		TenantID:     tenant.ID,
		WorkflowSlug: "quote_drafting",
		Mode:         domain.ModeSandbox,
		Payload:      quotePayload(caseName),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = application.ReceiveOperatorReply(context.Background(), InboundReply{
		Channel:         "telegram",
		ProviderNumber:  "+61400000000",
		PacketID:        packet.PacketID,
		SourceMessageID: sourceID,
		ActionButton:    domain.ActionWrongAction,
		FreeText:        "Should have held and asked for more photos.",
		FollowUpAnswer:  "always when client is new and photos are incomplete",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGoldenCorrectionPromotionAndFutureRun(t *testing.T) {
	ctx := context.Background()
	application, _, _, tenant := newTestApp(t)

	rejectCase(t, application, tenant, "one", "msg_1")
	rejectCase(t, application, tenant, "two", "msg_2")
	rejectCase(t, application, tenant, "three", "msg_3")

	candidates, err := application.ListCandidates(ctx, tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1", len(candidates))
	}
	candidate := candidates[0]
	if candidate.EvidenceCount != 3 || candidate.Status != "under_review" {
		t.Fatalf("candidate evidence/status = %d/%s, want 3/under_review", candidate.EvidenceCount, candidate.Status)
	}

	rule, sv, err := application.PromoteCandidate(ctx, tenant.ID, candidate.ID, "reviewer:alice", "three clean corrections")
	if err != nil {
		t.Fatal(err)
	}
	if rule.Status != "active" || len(sv.RuleManifest) != 1 {
		t.Fatalf("promotion failed: rule=%s manifest=%d", rule.Status, len(sv.RuleManifest))
	}

	_, packet, err := application.StartCase(ctx, StartCaseInput{
		TenantID:     tenant.ID,
		WorkflowSlug: "quote_drafting",
		Mode:         domain.ModeSandbox,
		Payload:      quotePayload("after_promotion"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if packet.PlannedAction.Type != "hold_and_request_more_info" {
		t.Fatalf("planned action = %s, want hold_and_request_more_info", packet.PlannedAction.Type)
	}
}

func TestDoubleTapSignalIsIdempotent(t *testing.T) {
	ctx := context.Background()
	application, store, _, tenant := newTestApp(t)
	_, packet, err := application.StartCase(ctx, StartCaseInput{
		TenantID:     tenant.ID,
		WorkflowSlug: "quote_drafting",
		Mode:         domain.ModeSandbox,
		Payload:      quotePayload("idempotency"),
	})
	if err != nil {
		t.Fatal(err)
	}
	reply := InboundReply{
		Channel:         "telegram",
		ProviderNumber:  "+61400000000",
		PacketID:        packet.PacketID,
		SourceMessageID: "same_msg",
		ActionButton:    domain.ActionWrongAction,
		FreeText:        "Should have held and asked for more photos.",
		FollowUpAnswer:  "always when client is new and photos are incomplete",
	}
	if _, err := application.ReceiveOperatorReply(ctx, reply); err != nil {
		t.Fatal(err)
	}
	if _, err := application.ReceiveOperatorReply(ctx, reply); err != nil {
		t.Fatal(err)
	}
	corrections, err := store.ListCorrections(ctx, tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(corrections) != 1 {
		t.Fatalf("corrections = %d, want 1", len(corrections))
	}
	events, _ := store.ListAuditEvents(ctx, tenant.ID)
	if countEvents(events, "correction_received") != 1 {
		t.Fatalf("correction_received count = %d, want 1", countEvents(events, "correction_received"))
	}
}

func TestReplayPinsOriginalSkillVersion(t *testing.T) {
	ctx := context.Background()
	application, store, _, tenant := newTestApp(t)
	original, _, err := application.StartCase(ctx, StartCaseInput{
		TenantID:     tenant.ID,
		WorkflowSlug: "quote_drafting",
		Mode:         domain.ModeSandbox,
		Payload:      quotePayload("before_rule"),
	})
	if err != nil {
		t.Fatal(err)
	}
	rejectCase(t, application, tenant, "one", "msg_1")
	rejectCase(t, application, tenant, "two", "msg_2")
	rejectCase(t, application, tenant, "three", "msg_3")
	candidates, _ := application.ListCandidates(ctx, tenant.ID)
	if _, _, err := application.PromoteCandidate(ctx, tenant.ID, candidates[0].ID, "reviewer:alice", "promote"); err != nil {
		t.Fatal(err)
	}

	pinned, err := application.ReplayCase(ctx, ReplayInput{TenantID: tenant.ID, CaseRunID: original.ID})
	if err != nil {
		t.Fatal(err)
	}
	pinnedPoint, err := store.GetDecisionPoint(ctx, tenant.ID, pinned.DecisionPointID)
	if err != nil {
		t.Fatal(err)
	}
	if pinned.ReplayTemperature != 0 {
		t.Fatalf("replay temperature = %.1f, want 0", pinned.ReplayTemperature)
	}
	if pinnedPoint.ProposedAction != "send_quote" {
		t.Fatalf("pinned replay action = %s, want original send_quote", pinnedPoint.ProposedAction)
	}

	current, err := application.ReplayCase(ctx, ReplayInput{TenantID: tenant.ID, CaseRunID: original.ID, ForceCurrentSV: true})
	if err != nil {
		t.Fatal(err)
	}
	currentPoint, _ := store.GetDecisionPoint(ctx, tenant.ID, current.DecisionPointID)
	if currentPoint.ProposedAction != "hold_and_request_more_info" {
		t.Fatalf("current replay action = %s, want promoted rule action", currentPoint.ProposedAction)
	}
}

func TestMCPThreeGatePreflight(t *testing.T) {
	ctx := context.Background()
	application, _, _, tenant := newTestApp(t)
	if containsString(application.MCPListTools(domain.ModeSandbox), "send_draft_email") {
		t.Fatal("sandbox tool manifest exposed write_final tool")
	}

	run, packet, err := application.StartCase(ctx, StartCaseInput{
		TenantID:     tenant.ID,
		WorkflowSlug: "quote_drafting",
		Mode:         domain.ModeLive,
		Payload:      quotePayload("live"),
	})
	if err != nil {
		t.Fatal(err)
	}
	baseReq := domain.MCPRequest{
		TenantHeader:    tenant.ID,
		CaseRunID:       run.ID,
		DecisionPointID: run.DecisionPointID,
		Mode:            domain.ModeLive,
		ToolName:        "send_draft_email",
		IdempotencyKey:  "mcp-key",
	}

	if _, err := application.CallMCPWriteFinal(ctx, tenant.ID, domain.ModeLive, baseReq); !errors.Is(err, domain.ErrApprovalRequired) {
		t.Fatalf("live without approval err = %v, want approval required", err)
	}
	if _, err := application.ReceiveOperatorReply(ctx, InboundReply{
		Channel:         "telegram",
		ProviderNumber:  "+61400000000",
		PacketID:        packet.PacketID,
		SourceMessageID: "approve_msg",
		ActionButton:    domain.ActionApprove,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.CallMCPWriteFinal(ctx, tenant.ID, domain.ModeSandbox, baseReq); !errors.Is(err, domain.ErrSandboxMode) {
		t.Fatalf("sandbox err = %v, want sandbox mode", err)
	}
	if _, err := application.CallMCPWriteFinal(ctx, tenant.ID, domain.ModeLive, domain.MCPRequest{
		TenantHeader:    "t_other",
		CaseRunID:       run.ID,
		DecisionPointID: run.DecisionPointID,
		Mode:            domain.ModeLive,
		ToolName:        "send_draft_email",
	}); !errors.Is(err, domain.ErrSecurityViolation) {
		t.Fatalf("tenant mismatch err = %v, want security violation", err)
	}
	if result, err := application.CallMCPWriteFinal(ctx, tenant.ID, domain.ModeLive, baseReq); err != nil || !result.OK {
		t.Fatalf("approved live write failed: result=%+v err=%v", result, err)
	}
}

func TestTenantIsolationAndPIIAggregateQuarantine(t *testing.T) {
	ctx := context.Background()
	application, store, _, tenantA := newTestApp(t)
	tenantB, _, err := application.ProvisionTenant(ctx, "Other Roofing", "roofing", "+61400000001", "op_telegram:other")
	if err != nil {
		t.Fatal(err)
	}
	rejectCase(t, application, tenantA, "one", "msg_1")
	rejectCase(t, application, tenantA, "two", "msg_2")
	rejectCase(t, application, tenantA, "three", "msg_3")
	candidates, _ := application.ListCandidates(ctx, tenantA.ID)
	if _, _, err := application.PromoteCandidate(ctx, tenantA.ID, candidates[0].ID, "reviewer:alice", "promote"); err != nil {
		t.Fatal(err)
	}

	_, packetB, err := application.StartCase(ctx, StartCaseInput{
		TenantID:     tenantB.ID,
		WorkflowSlug: "quote_drafting",
		Mode:         domain.ModeSandbox,
		Payload:      quotePayload("tenant_b"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if packetB.PlannedAction.Type != "send_quote" {
		t.Fatalf("tenant B saw tenant A rule: action=%s", packetB.PlannedAction.Type)
	}

	hash, canonical, err := domain.ConditionsHash([]domain.Condition{{Field: "client_name", Operator: "=", Value: "ABC Realty"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCandidate(ctx, domain.RuleCandidate{
		ID:                  "rc_pii",
		TenantID:            tenantA.ID,
		WorkflowSlug:        "quote_drafting",
		DecisionType:        "send_or_hold",
		ConditionsHash:      hash,
		ConditionsCanonical: canonical,
		RecommendedAction:   "hold_and_request_more_info",
		Scope:               domain.ScopeTenant,
		EvidenceCount:       1,
		Status:              "candidate",
	}); err != nil {
		t.Fatal(err)
	}
	safe, quarantined, err := application.BuildVerticalAggregates(ctx, "roofing", "quote_drafting")
	if err != nil {
		t.Fatal(err)
	}
	safeJSON, _ := json.Marshal(safe)
	quarantineJSON, _ := json.Marshal(quarantined)
	if strings.Contains(string(safeJSON), "ABC Realty") || strings.Contains(string(quarantineJSON), "ABC Realty") {
		t.Fatalf("PII leaked in aggregate safe=%s quarantine=%s", safeJSON, quarantineJSON)
	}
	if !strings.Contains(string(quarantineJSON), "quarantined:freetext") {
		t.Fatalf("quarantine redaction missing: %s", quarantineJSON)
	}
}

func TestGatewayOutageQueuesAndExpiredReplyAbandonsPacket(t *testing.T) {
	ctx := context.Background()
	application, store, clock, tenant := newTestApp(t)
	application.DisconnectGateway(ctx, tenant.ID)
	_, packet, err := application.StartCase(ctx, StartCaseInput{
		TenantID:     tenant.ID,
		WorkflowSlug: "quote_drafting",
		Mode:         domain.ModeSandbox,
		Payload:      quotePayload("outage"),
	})
	if err != nil {
		t.Fatal(err)
	}
	drained := application.RecoverGateway(ctx, tenant.ID)
	if len(drained) != 1 || drained[0].PacketID != packet.PacketID {
		t.Fatalf("drained packets = %+v, want packet %s", drained, packet.PacketID)
	}

	clock.Add(49 * time.Hour)
	_, err = application.ReceiveOperatorReply(ctx, InboundReply{
		Channel:         "telegram",
		ProviderNumber:  "+61400000000",
		PacketID:        packet.PacketID,
		SourceMessageID: "late",
		ActionButton:    domain.ActionWrongAction,
		FreeText:        "too late",
	})
	if !errors.Is(err, domain.ErrExpired) {
		t.Fatalf("late reply err = %v, want expired", err)
	}
	corrections, _ := store.ListCorrections(ctx, tenant.ID)
	if len(corrections) != 0 {
		t.Fatalf("expired reply wrote corrections: %d", len(corrections))
	}
}

func TestPromotionSupersedesSameConditionRule(t *testing.T) {
	ctx := context.Background()
	application, _, _, tenant := newTestApp(t)
	rejectCase(t, application, tenant, "one", "msg_1")
	rejectCase(t, application, tenant, "two", "msg_2")
	rejectCase(t, application, tenant, "three", "msg_3")
	candidates, _ := application.ListCandidates(ctx, tenant.ID)
	first, _, err := application.PromoteCandidate(ctx, tenant.ID, candidates[0].ID, "reviewer:alice", "promote")
	if err != nil {
		t.Fatal(err)
	}

	_, packet, err := application.StartCase(ctx, StartCaseInput{
		TenantID:     tenant.ID,
		WorkflowSlug: "quote_drafting",
		Mode:         domain.ModeSandbox,
		Payload:      quotePayload("override"),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = application.ReceiveOperatorReply(ctx, InboundReply{
		Channel:         "telegram",
		ProviderNumber:  "+61400000000",
		PacketID:        packet.PacketID,
		SourceMessageID: "override_msg",
		ActionButton:    domain.ActionWrongAction,
		FreeText:        "Go ahead, send it anyway when photos are incomplete.",
		FollowUpAnswer:  "always for this client type",
	})
	if err != nil {
		t.Fatal(err)
	}
	candidates, _ = application.ListCandidates(ctx, tenant.ID)
	var replacement domain.RuleCandidate
	for _, candidate := range candidates {
		if candidate.RecommendedAction == "send_quote" {
			replacement = candidate
		}
	}
	if replacement.ID == "" {
		t.Fatal("replacement candidate not created")
	}
	second, _, err := application.PromoteCandidate(ctx, tenant.ID, replacement.ID, "reviewer:alice", "supersede")
	if err != nil {
		t.Fatal(err)
	}
	if second.Supersedes != first.ID || second.Version != first.Version+1 {
		t.Fatalf("supersession got supersedes=%s version=%d, want %s/%d", second.Supersedes, second.Version, first.ID, first.Version+1)
	}
}

func countEvents(events []domain.AuditEvent, eventType string) int {
	count := 0
	for _, event := range events {
		if event.EventType == eventType {
			count++
		}
	}
	return count
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
