package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"victoria/internal/channel"
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

func TestMCPGateOrderSandboxFiresFirst(t *testing.T) {
	ctx := context.Background()
	application, store, _, tenant := newTestApp(t)
	run, _, err := application.StartCase(ctx, StartCaseInput{
		TenantID:     tenant.ID,
		WorkflowSlug: "quote_drafting",
		Mode:         domain.ModeLive,
		Payload:      quotePayload("gate-order"),
	})
	if err != nil {
		t.Fatal(err)
	}
	req := domain.MCPRequest{
		TenantHeader:    "t_other",
		CaseRunID:       run.ID,
		DecisionPointID: run.DecisionPointID,
		Mode:            domain.ModeLive,
		ToolName:        "send_draft_email",
	}
	if _, err := application.CallMCPWriteFinal(ctx, tenant.ID, domain.ModeSandbox, req); !errors.Is(err, domain.ErrSandboxMode) {
		t.Fatalf("sandbox + tenant mismatch err = %v, want sandbox mode (Gate 1 fires first)", err)
	}
	events, _ := store.ListAuditEvents(ctx, tenant.ID)
	if countEvents(events, "sandbox_escape_blocked") == 0 {
		t.Fatal("expected sandbox_escape_blocked audit event")
	}
	if countEvents(events, "security_violation") != 0 {
		t.Fatal("Gate 3 fired before Gate 1; got security_violation audit when sandbox should have blocked first")
	}
}

func TestContradictingCorrectionFlagsExistingCandidate(t *testing.T) {
	ctx := context.Background()
	application, store, _, tenant := newTestApp(t)
	rejectCase(t, application, tenant, "one", "msg_1")
	rejectCase(t, application, tenant, "two", "msg_2")

	_, packet, err := application.StartCase(ctx, StartCaseInput{
		TenantID:     tenant.ID,
		WorkflowSlug: "quote_drafting",
		Mode:         domain.ModeSandbox,
		Payload:      quotePayload("contradiction"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.ReceiveOperatorReply(ctx, InboundReply{
		Channel:         "telegram",
		ProviderNumber:  "+61400000000",
		PacketID:        packet.PacketID,
		SourceMessageID: "contra_msg",
		ActionButton:    domain.ActionWrongAction,
		FreeText:        "Go ahead, send it anyway when photos are incomplete.",
		FollowUpAnswer:  "always for this client type",
	}); err != nil {
		t.Fatal(err)
	}

	candidates, err := application.ListCandidates(ctx, tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	var holdCandidate domain.RuleCandidate
	var sendCandidate domain.RuleCandidate
	for _, candidate := range candidates {
		switch candidate.RecommendedAction {
		case "hold_and_request_more_info":
			holdCandidate = candidate
		case "send_quote":
			sendCandidate = candidate
		}
	}
	if holdCandidate.ID == "" || sendCandidate.ID == "" {
		t.Fatalf("expected both candidates; got %+v", candidates)
	}
	if holdCandidate.ConditionsHash != sendCandidate.ConditionsHash {
		t.Fatalf("conditions_hash differ: %s vs %s", holdCandidate.ConditionsHash, sendCandidate.ConditionsHash)
	}
	if holdCandidate.ContradictingCount != 1 {
		t.Fatalf("hold candidate ContradictingCount = %d, want 1", holdCandidate.ContradictingCount)
	}
	events, _ := store.ListAuditEvents(ctx, tenant.ID)
	if countEvents(events, "candidate_contradiction_detected") != 1 {
		t.Fatalf("candidate_contradiction_detected count = %d, want 1", countEvents(events, "candidate_contradiction_detected"))
	}
}

func TestReplayPreservesOriginalMode(t *testing.T) {
	ctx := context.Background()
	application, _, _, tenant := newTestApp(t)
	original, _, err := application.StartCase(ctx, StartCaseInput{
		TenantID:     tenant.ID,
		WorkflowSlug: "quote_drafting",
		Mode:         domain.ModeLive,
		Payload:      quotePayload("live"),
	})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := application.ReplayCase(ctx, ReplayInput{TenantID: tenant.ID, CaseRunID: original.ID})
	if err != nil {
		t.Fatal(err)
	}
	if replay.Mode != domain.ModeLive {
		t.Fatalf("replay mode = %s, want %s (INV-CR3 immutable mode)", replay.Mode, domain.ModeLive)
	}
}

func TestReplayUsesMCPReadSnapshot(t *testing.T) {
	ctx := context.Background()
	application, store, _, tenant := newTestApp(t)
	original, _, err := application.StartCase(ctx, StartCaseInput{
		TenantID:     tenant.ID,
		WorkflowSlug: "quote_drafting",
		Mode:         domain.ModeSandbox,
		Payload:      quotePayload("snapshot"),
	})
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := store.ListArtifacts(ctx, tenant.ID, original.ID)
	if err != nil {
		t.Fatal(err)
	}
	var snapshotID string
	for _, artifact := range artifacts {
		if artifact.ArtifactType == "mcp_read_snapshot" {
			snapshotID = artifact.ID
			break
		}
	}
	if snapshotID == "" {
		t.Fatal("mcp_read_snapshot artifact not created on StartCase")
	}
	mutated := domain.Artifact{
		ID:              snapshotID,
		TenantID:        tenant.ID,
		CaseRunID:       original.ID,
		DecisionPointID: original.DecisionPointID,
		ArtifactType:    "mcp_read_snapshot",
		StoragePath:     "/mutated",
		Content: map[string]any{
			"sandbox":         true,
			"client_type":     "repeat",
			"photos_complete": true,
			"snapshot_marker": "frozen",
		},
		CreatedAt: original.CreatedAt,
	}
	if err := store.CreateArtifact(ctx, mutated); err != nil {
		t.Fatal(err)
	}
	replay, err := application.ReplayCase(ctx, ReplayInput{TenantID: tenant.ID, CaseRunID: original.ID})
	if err != nil {
		t.Fatal(err)
	}
	if replay.InputPayload["snapshot_marker"] != "frozen" {
		t.Fatalf("replay input payload not loaded from snapshot artifact; got %+v", replay.InputPayload)
	}
}

func TestQueuedPacketNotMarkedDeliveredDuringOutage(t *testing.T) {
	ctx := context.Background()
	application, store, _, tenant := newTestApp(t)
	application.DisconnectGateway(ctx, tenant.ID)
	_, packet, err := application.StartCase(ctx, StartCaseInput{
		TenantID:     tenant.ID,
		WorkflowSlug: "quote_drafting",
		Mode:         domain.ModeSandbox,
		Payload:      quotePayload("queued"),
	})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := store.GetReviewPacket(ctx, tenant.ID, packet.PacketID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Delivered {
		t.Fatal("packet stored as delivered while session disconnected")
	}
	drained := application.RecoverGateway(ctx, tenant.ID)
	if len(drained) != 1 {
		t.Fatalf("drained len = %d, want 1", len(drained))
	}
	stored, err = store.GetReviewPacket(ctx, tenant.ID, packet.PacketID)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Delivered {
		t.Fatal("packet not marked delivered after recover")
	}
}

// fakeWAAdapter is a stand-in for the whatsmeow Manager used to test the
// gateway's adapter-aware send + queue + drain path without touching the
// network.
type fakeWAAdapter struct {
	sentPacketIDs []string
	failNext      bool
}

func (*fakeWAAdapter) Channel() channel.Channel { return channel.ChannelWhatsApp }
func (a *fakeWAAdapter) SendOutbound(_ context.Context, msg channel.OutboundMessage) (channel.DeliveryReceipt, error) {
	if a.failNext {
		a.failNext = false
		return channel.DeliveryReceipt{}, fmt.Errorf("simulated send failure")
	}
	a.sentPacketIDs = append(a.sentPacketIDs, msg.PacketID)
	return channel.DeliveryReceipt{ProviderMessageID: "wa:" + msg.PacketID, SentAt: time.Now()}, nil
}
func (*fakeWAAdapter) NormalizeInboundWebhook([]byte) ([]channel.InboundMessage, error) {
	return nil, nil
}

func TestWhatsAppOutageQueuesDurablyAndDrainsOnReconnect(t *testing.T) {
	ctx := context.Background()
	application, store, _, tenant := newTestApp(t)
	wa := &fakeWAAdapter{}
	application.RegisterChannelAdapter(wa)

	// Mark WhatsApp session active so packets prefer it.
	application.NotifyChannelSession(ctx, tenant.ID, channel.ChannelWhatsApp, domain.SessionActive)
	if _, _, err := application.StartCase(ctx, StartCaseInput{
		TenantID:     tenant.ID,
		WorkflowSlug: "quote_drafting",
		Mode:         domain.ModeSandbox,
		Payload:      quotePayload("wa-online"),
	}); err != nil {
		t.Fatal(err)
	}
	if len(wa.sentPacketIDs) != 1 {
		t.Fatalf("expected 1 packet sent while active, got %d", len(wa.sentPacketIDs))
	}

	// Now simulate disconnect; subsequent packets must be persisted, not sent.
	application.NotifyChannelSession(ctx, tenant.ID, channel.ChannelWhatsApp, domain.SessionDisconnected)
	for i := 0; i < 3; i++ {
		if _, _, err := application.StartCase(ctx, StartCaseInput{
			TenantID:     tenant.ID,
			WorkflowSlug: "quote_drafting",
			Mode:         domain.ModeSandbox,
			Payload:      quotePayload(fmt.Sprintf("wa-down-%d", i)),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if len(wa.sentPacketIDs) != 1 {
		t.Fatalf("packets sent during outage: got %d, want 1 (only pre-outage)", len(wa.sentPacketIDs))
	}
	depth, err := store.OutboundQueueDepth(ctx, tenant.ID, string(channel.ChannelWhatsApp))
	if err != nil {
		t.Fatal(err)
	}
	if depth != 3 {
		t.Fatalf("queue depth = %d, want 3", depth)
	}

	// Reconnect: gateway must drain in FIFO order.
	application.NotifyChannelSession(ctx, tenant.ID, channel.ChannelWhatsApp, domain.SessionActive)
	if len(wa.sentPacketIDs) != 4 {
		t.Fatalf("after drain, sent count = %d, want 4", len(wa.sentPacketIDs))
	}
	depthAfter, _ := store.OutboundQueueDepth(ctx, tenant.ID, string(channel.ChannelWhatsApp))
	if depthAfter != 0 {
		t.Fatalf("queue depth after drain = %d, want 0", depthAfter)
	}
}

func TestWhatsAppDrainPreservesQueueOnSendFailure(t *testing.T) {
	ctx := context.Background()
	application, store, _, tenant := newTestApp(t)
	wa := &fakeWAAdapter{}
	application.RegisterChannelAdapter(wa)

	// Queue 2 packets while disconnected.
	application.NotifyChannelSession(ctx, tenant.ID, channel.ChannelWhatsApp, domain.SessionDisconnected)
	for i := 0; i < 2; i++ {
		if _, _, err := application.StartCase(ctx, StartCaseInput{
			TenantID:     tenant.ID,
			WorkflowSlug: "quote_drafting",
			Mode:         domain.ModeSandbox,
			Payload:      quotePayload(fmt.Sprintf("preq-%d", i)),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if depth, _ := store.OutboundQueueDepth(ctx, tenant.ID, string(channel.ChannelWhatsApp)); depth != 2 {
		t.Fatalf("queue depth before drain = %d, want 2", depth)
	}

	// Arm the adapter to fail on the very first drain attempt.
	wa.failNext = true
	application.NotifyChannelSession(ctx, tenant.ID, channel.ChannelWhatsApp, domain.SessionActive)

	// After a failed drain attempt, all entries must remain so the next
	// reconnect can retry. WA-INV-3: no silent loss.
	depth, _ := store.OutboundQueueDepth(ctx, tenant.ID, string(channel.ChannelWhatsApp))
	if depth != 2 {
		t.Fatalf("queue depth after failed drain = %d, want 2 (no silent loss)", depth)
	}
	if len(wa.sentPacketIDs) != 0 {
		t.Fatalf("packets reported sent on failed drain: %d", len(wa.sentPacketIDs))
	}

	// Trigger another reconnect; this time the adapter accepts both packets.
	application.NotifyChannelSession(ctx, tenant.ID, channel.ChannelWhatsApp, domain.SessionDisconnected)
	application.NotifyChannelSession(ctx, tenant.ID, channel.ChannelWhatsApp, domain.SessionActive)
	if len(wa.sentPacketIDs) != 2 {
		t.Fatalf("after retry drain, sent = %d, want 2", len(wa.sentPacketIDs))
	}
	finalDepth, _ := store.OutboundQueueDepth(ctx, tenant.ID, string(channel.ChannelWhatsApp))
	if finalDepth != 0 {
		t.Fatalf("queue depth after successful drain = %d, want 0", finalDepth)
	}
}

func TestWhatsAppQueueOverflowTombstonesOldest(t *testing.T) {
	ctx := context.Background()
	application, store, _, tenant := newTestApp(t)
	application.RegisterChannelAdapter(&fakeWAAdapter{})
	application.NotifyChannelSession(ctx, tenant.ID, channel.ChannelWhatsApp, domain.SessionDisconnected)

	for i := 0; i < OutboundQueueMax+5; i++ {
		if _, _, err := application.StartCase(ctx, StartCaseInput{
			TenantID:     tenant.ID,
			WorkflowSlug: "quote_drafting",
			Mode:         domain.ModeSandbox,
			Payload:      quotePayload(fmt.Sprintf("flood-%d", i)),
		}); err != nil {
			t.Fatal(err)
		}
	}
	depth, _ := store.OutboundQueueDepth(ctx, tenant.ID, string(channel.ChannelWhatsApp))
	if depth != OutboundQueueMax {
		t.Fatalf("queue depth = %d, want capped at %d", depth, OutboundQueueMax)
	}
	events, _ := store.ListAuditEvents(ctx, tenant.ID)
	if countEvents(events, "packet_tombstoned") < 5 {
		t.Fatalf("packet_tombstoned count = %d, want >=5", countEvents(events, "packet_tombstoned"))
	}
}

func TestExtractPacketReferenceParsesTag(t *testing.T) {
	cases := map[string]string{
		"approve\n[packet:pkt_abc]":    "pkt_abc",
		"hello world":                  "",
		"prefix [packet:pkt_xyz] tail": "pkt_xyz",
	}
	for in, want := range cases {
		if got := extractPacketReference(in); got != want {
			t.Fatalf("extractPacketReference(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGuessButtonFromTextHandlesNumberAndLabel(t *testing.T) {
	cases := map[string]domain.ActionButton{
		// Direct numeric/label paths.
		"1":              domain.ActionApprove,
		"approve please": domain.ActionApprove,
		"3":              domain.ActionWrongAction,
		"add note: hi":   domain.ActionAddNote,
		// Short positive acknowledgements → approve.
		"ok":         domain.ActionApprove,
		"yes":        domain.ActionApprove,
		"looks good": domain.ActionApprove,
		"👍":          domain.ActionApprove,
		// Substantive prose without a recognised approval token →
		// wrong_action; structured parser downstream extracts the rule.
		"no, this is from singapore": domain.ActionWrongAction,
		"send it anyway":             domain.ActionWrongAction,
		"???":                        domain.ActionWrongAction,
		// Empty stays empty (would be dead-lettered upstream anyway).
		"": "",
	}
	for in, want := range cases {
		if got := guessButtonFromText(in); got != want {
			t.Fatalf("guessButtonFromText(%q) = %q, want %q", in, got, want)
		}
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
