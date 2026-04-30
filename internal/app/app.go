package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"victoria/internal/domain"
)

type App struct {
	store Store
	ids   IDGenerator
	clock Clock

	gateway *Gateway
}

func New(store Store, opts ...Option) *App {
	a := &App{
		store: store,
		ids:   RandomIDs{},
		clock: SystemClock{},
	}
	for _, opt := range opts {
		opt(a)
	}
	a.gateway = NewGateway(store, a.ids, a.clock)
	return a
}

type Option func(*App)

func WithIDs(ids IDGenerator) Option {
	return func(a *App) { a.ids = ids }
}

func WithClock(clock Clock) Option {
	return func(a *App) { a.clock = clock }
}

func (a *App) ProvisionTenant(ctx context.Context, name, vertical, providerNumber, operatorID string) (domain.Tenant, domain.ProvisioningManifest, error) {
	now := a.clock.Now()
	tenant := domain.Tenant{
		ID:          a.ids.NewID("t"),
		Name:        strings.TrimSpace(name),
		Vertical:    strings.TrimSpace(vertical),
		Status:      "active",
		DefaultMode: domain.ModeSandbox,
		CreatedAt:   now,
	}
	if tenant.Name == "" || tenant.Vertical == "" || providerNumber == "" || operatorID == "" {
		return domain.Tenant{}, domain.ProvisioningManifest{}, domain.ErrInvalidInput
	}

	for _, tmpl := range defaultWorkflowTemplates(tenant.Vertical) {
		if err := a.store.UpsertWorkflowTemplate(ctx, tmpl); err != nil {
			return domain.Tenant{}, domain.ProvisioningManifest{}, err
		}
	}

	initialSV := domain.SkillVersion{
		ID:           a.ids.NewID("sv"),
		TenantID:     tenant.ID,
		WorkflowSlug: "quote_drafting",
		Version:      1,
		RuleManifest: nil,
		Status:       "active",
		CreatedAt:    now,
	}
	manifest := domain.ProvisioningManifest{
		TenantID:          tenant.ID,
		HermesVersion:     "0.3.4",
		Mode:              domain.ModeSandbox,
		WorkflowTemplates: []string{"enquiry_triage", "quote_drafting", "invoice_handling"},
		MCPEndpoints: map[string]string{
			"sandbox_email":   "http://localhost:3001",
			"sandbox_drive":   "http://localhost:3002",
			"sandbox_invoice": "http://localhost:3003",
		},
		SkillVersionEndpoint: "/internal/skill-versions/active",
		Vertical:             tenant.Vertical,
	}
	binding := domain.ChannelBinding{
		TenantID:       tenant.ID,
		Channel:        "telegram",
		ProviderNumber: providerNumber,
		OperatorID:     operatorID,
	}
	if err := a.store.CreateTenant(ctx, tenant, manifest, binding, initialSV); err != nil {
		return domain.Tenant{}, domain.ProvisioningManifest{}, err
	}
	for _, workflowSlug := range []string{"enquiry_triage", "invoice_handling"} {
		if err := a.store.CreateSkillVersion(ctx, domain.SkillVersion{
			ID:           a.ids.NewID("sv"),
			TenantID:     tenant.ID,
			WorkflowSlug: workflowSlug,
			Version:      1,
			RuleManifest: nil,
			Status:       "active",
			CreatedAt:    now,
		}); err != nil {
			return domain.Tenant{}, domain.ProvisioningManifest{}, err
		}
	}
	_, err := a.store.CreateAuditEvent(ctx, auditEvent(a.ids, now, tenant.ID, "tenant_provisioned", "admin", "system", "tenant", tenant.ID, nil, map[string]any{
		"vertical": tenant.Vertical,
	}, "tenant-provisioned", tenant.ID))
	if err != nil {
		return domain.Tenant{}, domain.ProvisioningManifest{}, err
	}
	return tenant, manifest, nil
}

type StartCaseInput struct {
	TenantID     string         `json:"tenant_id"`
	WorkflowSlug string         `json:"workflow_slug"`
	Mode         domain.Mode    `json:"mode"`
	Payload      map[string]any `json:"payload"`
}

func (a *App) StartCase(ctx context.Context, input StartCaseInput) (domain.CaseRun, domain.ReviewPacket, error) {
	if input.TenantID == "" || input.WorkflowSlug == "" || !input.Mode.ValidForWorkflowInput() {
		return domain.CaseRun{}, domain.ReviewPacket{}, domain.ErrInvalidInput
	}
	if input.Mode == domain.ModeSandbox && !boolValue(input.Payload, "sandbox") {
		return domain.CaseRun{}, domain.ReviewPacket{}, domain.ErrSandboxContamination
	}
	if _, err := a.store.GetTenant(ctx, input.TenantID); err != nil {
		return domain.CaseRun{}, domain.ReviewPacket{}, err
	}
	if _, err := a.store.GetWorkflowTemplate(ctx, input.WorkflowSlug); err != nil {
		return domain.CaseRun{}, domain.ReviewPacket{}, err
	}

	now := a.clock.Now()
	inputHash, err := domain.JSONHash(input.Payload)
	if err != nil {
		return domain.CaseRun{}, domain.ReviewPacket{}, err
	}
	sv, err := a.store.ActiveSkillVersion(ctx, input.TenantID, input.WorkflowSlug)
	if err != nil {
		return domain.CaseRun{}, domain.ReviewPacket{}, err
	}
	caseRun := domain.CaseRun{
		ID:                a.ids.NewID("cr"),
		TenantID:          input.TenantID,
		WorkflowSlug:      input.WorkflowSlug,
		Mode:              input.Mode,
		InputPayload:      cloneMap(input.Payload),
		InputHash:         inputHash,
		SkillVersionID:    sv.ID,
		Status:            "waiting_for_approval",
		CreatedAt:         now,
		ReplayTemperature: 0.2,
	}
	decision := a.evaluateDecision(now, caseRun, input.Payload, sv)
	caseRun.DecisionPointID = decision.ID
	artifact := a.buildArtifact(now, caseRun, decision)
	packet := a.buildReviewPacket(now, caseRun, decision, artifact)

	if err := a.store.CreateCaseRun(ctx, caseRun); err != nil {
		return domain.CaseRun{}, domain.ReviewPacket{}, err
	}
	if err := a.store.CreateDecisionPoint(ctx, decision); err != nil {
		return domain.CaseRun{}, domain.ReviewPacket{}, err
	}
	if err := a.store.CreateArtifact(ctx, artifact); err != nil {
		return domain.CaseRun{}, domain.ReviewPacket{}, err
	}
	snapshot := artifact
	snapshot.ID = a.ids.NewID("art")
	snapshot.ArtifactType = "mcp_read_snapshot"
	snapshot.StoragePath = fmt.Sprintf("/%s/sandbox/%s/snapshots/input.json", caseRun.TenantID, caseRun.ID)
	snapshot.Content = cloneMap(input.Payload)
	if err := a.store.CreateArtifact(ctx, snapshot); err != nil {
		return domain.CaseRun{}, domain.ReviewPacket{}, err
	}
	if err := a.gateway.SendApprovalPacket(ctx, packet); err != nil {
		return domain.CaseRun{}, domain.ReviewPacket{}, err
	}
	return caseRun, packet, nil
}

type ReplayInput struct {
	TenantID       string `json:"tenant_id"`
	CaseRunID      string `json:"case_run_id"`
	SkillVersionID string `json:"skill_version_id,omitempty"`
	ReplayRunID    string `json:"replay_run_id,omitempty"`
	ForceCurrentSV bool   `json:"force_current_skill_version,omitempty"`
}

func (a *App) ReplayCase(ctx context.Context, input ReplayInput) (domain.CaseRun, error) {
	original, err := a.store.GetCaseRun(ctx, input.TenantID, input.CaseRunID)
	if err != nil {
		return domain.CaseRun{}, err
	}
	var sv domain.SkillVersion
	switch {
	case input.SkillVersionID != "":
		sv, err = a.store.GetSkillVersion(ctx, input.TenantID, input.SkillVersionID)
	case input.ForceCurrentSV:
		sv, err = a.store.ActiveSkillVersion(ctx, input.TenantID, original.WorkflowSlug)
	default:
		sv, err = a.store.GetSkillVersion(ctx, input.TenantID, original.SkillVersionID)
	}
	if err != nil {
		return domain.CaseRun{}, err
	}
	now := a.clock.Now()
	replay := domain.CaseRun{
		ID:                a.ids.NewID("cr"),
		TenantID:          original.TenantID,
		WorkflowSlug:      original.WorkflowSlug,
		Mode:              domain.ModeSandbox,
		InputPayload:      cloneMap(original.InputPayload),
		InputHash:         original.InputHash,
		SkillVersionID:    sv.ID,
		ReplayedFromID:    original.ID,
		Status:            "replayed",
		CreatedAt:         now,
		ReplayTemperature: 0,
	}
	decision := a.evaluateDecision(now, replay, replay.InputPayload, sv)
	replay.DecisionPointID = decision.ID
	decision.Status = "replayed"
	if err := a.store.CreateCaseRun(ctx, replay); err != nil {
		return domain.CaseRun{}, err
	}
	if err := a.store.CreateDecisionPoint(ctx, decision); err != nil {
		return domain.CaseRun{}, err
	}
	return replay, nil
}

func (a *App) ReceiveOperatorReply(ctx context.Context, input InboundReply) (domain.ApprovalSignalEnvelope, error) {
	return a.gateway.ReceiveInbound(ctx, input, a.persistSignal)
}

func (a *App) DisconnectGateway(ctx context.Context, tenantID string) {
	a.gateway.Disconnect(ctx, tenantID)
}

func (a *App) RecoverGateway(ctx context.Context, tenantID string) []domain.ReviewPacket {
	return a.gateway.Recover(ctx, tenantID)
}

func (a *App) ListCandidates(ctx context.Context, tenantID string) ([]domain.RuleCandidate, error) {
	return a.store.ListCandidates(ctx, tenantID)
}

func (a *App) PromoteCandidate(ctx context.Context, tenantID, candidateID, reviewerID, rationale string) (domain.ValidatedRule, domain.SkillVersion, error) {
	if reviewerID == "" {
		return domain.ValidatedRule{}, domain.SkillVersion{}, domain.ErrInvalidInput
	}
	candidate, err := a.store.GetCandidate(ctx, tenantID, candidateID)
	if err != nil {
		return domain.ValidatedRule{}, domain.SkillVersion{}, err
	}
	now := a.clock.Now()
	supersedes, version, err := a.store.DeprecateActiveRule(ctx, tenantID, candidate.WorkflowSlug, candidate.DecisionType, candidate.ConditionsHash)
	if err != nil {
		return domain.ValidatedRule{}, domain.SkillVersion{}, err
	}
	rule := domain.ValidatedRule{
		ID:                      a.ids.NewID("vr"),
		TenantID:                tenantID,
		WorkflowSlug:            candidate.WorkflowSlug,
		DecisionType:            candidate.DecisionType,
		ConditionsHash:          candidate.ConditionsHash,
		ConditionsCanonical:     append([]domain.Condition(nil), candidate.ConditionsCanonical...),
		RecommendedAction:       candidate.RecommendedAction,
		Scope:                   candidate.Scope,
		Version:                 version,
		Supersedes:              supersedes,
		PromotedFromCandidateID: candidate.ID,
		PromotedBy:              reviewerID,
		PromotedAt:              now,
		Status:                  "active",
		Rationale:               rationale,
	}
	if err := a.store.CreateValidatedRule(ctx, rule); err != nil {
		return domain.ValidatedRule{}, domain.SkillVersion{}, err
	}
	candidate.Status = "promoted"
	if err := a.store.SaveCandidate(ctx, candidate); err != nil {
		return domain.ValidatedRule{}, domain.SkillVersion{}, err
	}
	_, err = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, now, tenantID, "rule_promoted", "reviewer", reviewerID, "validated_rule", rule.ID, map[string]any{
		"candidate_id": candidate.ID,
	}, map[string]any{
		"scope": string(rule.Scope),
	}, "rule-promoted", tenantID, rule.ID))
	if err != nil {
		return domain.ValidatedRule{}, domain.SkillVersion{}, err
	}
	sv, err := a.createSkillVersion(ctx, tenantID, candidate.WorkflowSlug, now)
	if err != nil {
		return domain.ValidatedRule{}, domain.SkillVersion{}, err
	}
	return rule, sv, nil
}

func (a *App) LoadSkillVersion(ctx context.Context, tenantID, workflowSlug string) (domain.SkillVersion, error) {
	return a.store.ActiveSkillVersion(ctx, tenantID, workflowSlug)
}

type VerticalAggregate struct {
	WorkflowSlug        string             `json:"workflow_slug"`
	DecisionType        string             `json:"decision_type"`
	ConditionsCanonical []domain.Condition `json:"conditions_canonical"`
	RecommendedAction   string             `json:"recommended_action"`
	EvidenceCount       int                `json:"evidence_count"`
	Quarantined         bool               `json:"quarantined"`
}

func (a *App) BuildVerticalAggregates(ctx context.Context, vertical, workflowSlug string) ([]VerticalAggregate, []VerticalAggregate, error) {
	tenants, err := a.store.ListTenants(ctx)
	if err != nil {
		return nil, nil, err
	}
	var safe []VerticalAggregate
	var quarantined []VerticalAggregate
	for _, tenant := range tenants {
		if tenant.Vertical != vertical {
			continue
		}
		candidates, err := a.store.ListCandidates(ctx, tenant.ID)
		if err != nil {
			return nil, nil, err
		}
		for _, candidate := range candidates {
			if candidate.WorkflowSlug != workflowSlug {
				continue
			}
			conditions, quarantine := redactAggregateConditions(candidate.ConditionsCanonical)
			aggregate := VerticalAggregate{
				WorkflowSlug:        candidate.WorkflowSlug,
				DecisionType:        candidate.DecisionType,
				ConditionsCanonical: conditions,
				RecommendedAction:   candidate.RecommendedAction,
				EvidenceCount:       candidate.EvidenceCount,
				Quarantined:         quarantine,
			}
			if quarantine {
				quarantined = append(quarantined, aggregate)
				_, _ = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, a.clock.Now(), tenant.ID, "aggregate_quarantined", "system", "aggregation", "rule_candidate", candidate.ID, nil, map[string]any{
					"workflow_slug": candidate.WorkflowSlug,
				}, "aggregate-quarantine", tenant.ID, candidate.ID))
				continue
			}
			safe = append(safe, aggregate)
		}
	}
	return safe, quarantined, nil
}

func (a *App) CallMCPWriteFinal(ctx context.Context, boundTenantID string, serverMode domain.Mode, req domain.MCPRequest) (domain.MCPResult, error) {
	now := a.clock.Now()
	if req.TenantHeader != boundTenantID {
		_, _ = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, now, boundTenantID, "security_violation", "system", "mcp", "decision_point", req.DecisionPointID, nil, map[string]any{
			"tenant_header": req.TenantHeader,
			"bound_tenant":  boundTenantID,
			"tool_name":     req.ToolName,
		}, "mcp-security", boundTenantID, req.CaseRunID, req.DecisionPointID, req.ToolName))
		return domain.MCPResult{OK: false, Code: "TENANT_MISMATCH", Error: domain.ErrSecurityViolation.Error()}, domain.ErrSecurityViolation
	}
	effectiveServerMode := domain.ParseMode(string(serverMode))
	effectiveRequestMode := domain.ParseMode(string(req.Mode))
	if effectiveServerMode == domain.ModeSandbox || effectiveRequestMode == domain.ModeSandbox {
		_, _ = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, now, boundTenantID, "sandbox_escape_blocked", "system", "mcp", "decision_point", req.DecisionPointID, nil, map[string]any{
			"tool_name": req.ToolName,
		}, "mcp-sandbox", boundTenantID, req.CaseRunID, req.DecisionPointID, req.ToolName))
		return domain.MCPResult{OK: false, Code: "SANDBOX_MODE", Error: domain.ErrSandboxMode.Error()}, domain.ErrSandboxMode
	}
	ok, err := a.store.HasApprovalAudit(ctx, boundTenantID, req.CaseRunID, req.DecisionPointID)
	if err != nil {
		return domain.MCPResult{}, err
	}
	if !ok {
		_, _ = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, now, boundTenantID, "blocked_write_attempted", "system", "mcp", "decision_point", req.DecisionPointID, nil, map[string]any{
			"tool_name": req.ToolName,
		}, "mcp-approval", boundTenantID, req.CaseRunID, req.DecisionPointID, req.ToolName))
		return domain.MCPResult{OK: false, Code: "APPROVAL_REQUIRED", Error: domain.ErrApprovalRequired.Error()}, domain.ErrApprovalRequired
	}
	_, err = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, now, boundTenantID, "mcp_tool_called", "system", "mcp", "decision_point", req.DecisionPointID, nil, map[string]any{
		"tool_name":         req.ToolName,
		"side_effect_class": "write_final",
	}, "mcp-tool", boundTenantID, req.CaseRunID, req.DecisionPointID, req.ToolName, req.IdempotencyKey))
	if err != nil {
		return domain.MCPResult{}, err
	}
	return domain.MCPResult{OK: true}, nil
}

func (a *App) MCPListTools(mode domain.Mode) []string {
	tools := []string{"create_draft_email", "create_invoice_draft", "read_file", "parse_invoice_document"}
	if domain.ParseMode(string(mode)) == domain.ModeLive {
		tools = append(tools, "send_draft_email", "publish_document", "finalize_invoice", "submit_to_accounting")
	}
	sort.Strings(tools)
	return tools
}

func (a *App) persistSignal(ctx context.Context, envelope domain.ApprovalSignalEnvelope) error {
	seen, err := a.store.SeenSignal(ctx, envelope.SignalID)
	if err != nil {
		return err
	}
	if seen {
		return nil
	}
	if err := a.store.MarkSignalSeen(ctx, envelope.SignalID); err != nil {
		return err
	}
	if envelope.ActionButton == domain.ActionApprove {
		return a.persistApproval(ctx, envelope)
	}
	return a.persistCorrection(ctx, envelope)
}

func (a *App) persistApproval(ctx context.Context, envelope domain.ApprovalSignalEnvelope) error {
	now := a.clock.Now()
	_, err := a.store.CreateAuditEvent(ctx, auditEvent(a.ids, now, envelope.TenantID, "approval_received", "operator", envelope.OperatorID, "decision_point", envelope.DecisionPointID, map[string]any{
		"case_run_id": envelope.CaseRunID,
		"packet_id":   envelope.PacketID,
	}, map[string]any{
		"action_button": string(envelope.ActionButton),
		"channel":       envelope.Channel,
	}, "approval", envelope.TenantID, envelope.CaseRunID, envelope.DecisionPointID, string(envelope.ActionButton)))
	if err != nil {
		return err
	}
	if err := a.store.UpdateDecisionPointStatus(ctx, envelope.TenantID, envelope.DecisionPointID, "approved"); err != nil {
		return err
	}
	return a.store.UpdateCaseRunStatus(ctx, envelope.TenantID, envelope.CaseRunID, "approved")
}

func (a *App) persistCorrection(ctx context.Context, envelope domain.ApprovalSignalEnvelope) error {
	now := a.clock.Now()
	correction := domain.Correction{
		ID:                  a.ids.NewID("corr"),
		SchemaVersion:       envelope.SchemaVersion,
		IdempotencyKey:      envelope.IdempotencyKey,
		PacketID:            envelope.PacketID,
		CaseRunID:           envelope.CaseRunID,
		DecisionPointID:     envelope.DecisionPointID,
		TenantID:            envelope.TenantID,
		OperatorID:          envelope.OperatorID,
		Channel:             envelope.Channel,
		SourceMessageID:     envelope.SourceMessageID,
		RawInboundMessageID: envelope.RawInboundMessageID,
		ActionButton:        envelope.ActionButton,
		FreeText:            envelope.FreeText,
		FollowUpAnswer:      envelope.FollowUpAnswer,
		ScopeHint:           envelope.ScopeHint,
		ParseMethod:         envelope.ParserMethod,
		ParseConfidence:     envelope.ParserConfidence,
		CreatedAt:           envelope.TS,
	}
	created, err := a.store.CreateCorrection(ctx, correction)
	if err != nil {
		return err
	}
	if !created {
		return nil
	}
	_, err = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, now, envelope.TenantID, "correction_received", "operator", envelope.OperatorID, "correction", correction.ID, map[string]any{
		"case_run_id":       envelope.CaseRunID,
		"decision_point_id": envelope.DecisionPointID,
		"packet_id":         envelope.PacketID,
	}, map[string]any{
		"action_button": string(envelope.ActionButton),
		"channel":       envelope.Channel,
	}, "correction", envelope.TenantID, envelope.IdempotencyKey))
	if err != nil {
		return err
	}
	if err := a.store.UpdateDecisionPointStatus(ctx, envelope.TenantID, envelope.DecisionPointID, "corrected"); err != nil {
		return err
	}
	if err := a.store.UpdateCaseRunStatus(ctx, envelope.TenantID, envelope.CaseRunID, "corrected"); err != nil {
		return err
	}
	return a.mergeCandidate(ctx, correction)
}

func (a *App) mergeCandidate(ctx context.Context, correction domain.Correction) error {
	point, err := a.store.GetDecisionPoint(ctx, correction.TenantID, correction.DecisionPointID)
	if err != nil {
		return err
	}
	conditions, recommendedAction := structuredRuleFromCorrection(correction, point)
	if len(conditions) == 0 || recommendedAction == "" {
		_, _ = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, a.clock.Now(), correction.TenantID, "correction_parse_failed", "system", "learning", "correction", correction.ID, nil, map[string]any{
			"action_button": string(correction.ActionButton),
		}, "parse-failed", correction.TenantID, correction.ID))
		return nil
	}
	conditionsHash, canonical, err := domain.ConditionsHash(conditions)
	if err != nil {
		return err
	}
	now := a.clock.Now()
	workflowSlug := workflowFromCaseInput(point.AgentInput)
	candidate, err := a.store.FindCandidate(ctx, correction.TenantID, workflowSlug, point.DecisionType, conditionsHash, recommendedAction)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	if errors.Is(err, domain.ErrNotFound) {
		scope := domain.ScopeCase
		if correction.ScopeHint != nil {
			scope = *correction.ScopeHint
		}
		candidate = domain.RuleCandidate{
			ID:                  a.ids.NewID("rc"),
			TenantID:            correction.TenantID,
			WorkflowSlug:        workflowSlug,
			DecisionType:        point.DecisionType,
			ConditionsHash:      conditionsHash,
			ConditionsCanonical: canonical,
			RecommendedAction:   recommendedAction,
			Scope:               scope,
			EvidenceCount:       1,
			SourceCorrectionIDs: []string{correction.ID},
			SourceCaseRunIDs:    []string{correction.CaseRunID},
			Status:              "candidate",
			CreatedAt:           now,
			LastSeenAt:          now,
		}
		candidate.Confidence = domain.CandidateConfidence(now, candidate.EvidenceCount, candidate.ContradictingCount, candidate.SourceCaseRunIDs, []string{candidate.TenantID}, candidate.LastSeenAt)
		candidate.Status = domain.CandidateStatus(candidate.Confidence, candidate.EvidenceCount, candidate.ContradictingCount)
		if candidate.Status == "under_review" {
			candidate.UnderReviewAt = &now
		}
		if err := a.store.SaveCandidate(ctx, candidate); err != nil {
			return err
		}
		_, err = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, now, correction.TenantID, "candidate_created", "system", "learning", "rule_candidate", candidate.ID, map[string]any{
			"correction_id": correction.ID,
		}, nil, "candidate-created", correction.TenantID, candidate.ID))
		return err
	}
	if contains(candidate.SourceCorrectionIDs, correction.ID) {
		return nil
	}
	candidate.SourceCorrectionIDs = append(candidate.SourceCorrectionIDs, correction.ID)
	candidate.SourceCaseRunIDs = appendIfMissing(candidate.SourceCaseRunIDs, correction.CaseRunID)
	candidate.EvidenceCount++
	candidate.LastSeenAt = now
	candidate.Confidence = domain.CandidateConfidence(now, candidate.EvidenceCount, candidate.ContradictingCount, candidate.SourceCaseRunIDs, []string{candidate.TenantID}, candidate.LastSeenAt)
	nextStatus := domain.CandidateStatus(candidate.Confidence, candidate.EvidenceCount, candidate.ContradictingCount)
	if candidate.Status != "under_review" && nextStatus == "under_review" {
		candidate.UnderReviewAt = &now
	}
	candidate.Status = nextStatus
	if err := a.store.SaveCandidate(ctx, candidate); err != nil {
		return err
	}
	_, err = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, now, correction.TenantID, "candidate_evidence_added", "system", "learning", "rule_candidate", candidate.ID, map[string]any{
		"correction_id": correction.ID,
	}, nil, "candidate-evidence", correction.TenantID, candidate.ID, correction.ID))
	return err
}

func (a *App) createSkillVersion(ctx context.Context, tenantID, workflowSlug string, now time.Time) (domain.SkillVersion, error) {
	active, err := a.store.ActiveSkillVersion(ctx, tenantID, workflowSlug)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return domain.SkillVersion{}, err
	}
	version := 1
	if err == nil {
		version = active.Version + 1
	}
	rules, err := a.store.ListVisibleValidatedRules(ctx, tenantID, workflowSlug)
	if err != nil {
		return domain.SkillVersion{}, err
	}
	manifest := make([]domain.RuleManifest, 0, len(rules))
	for _, rule := range rules {
		manifest = append(manifest, domain.RuleManifest{
			RuleID:              rule.ID,
			Scope:               rule.Scope,
			Version:             rule.Version,
			DecisionType:        rule.DecisionType,
			ConditionsCanonical: append([]domain.Condition(nil), rule.ConditionsCanonical...),
			RecommendedAction:   rule.RecommendedAction,
			Priority:            scopePriority(rule.Scope),
		})
	}
	sort.Slice(manifest, func(i, j int) bool {
		if manifest[i].Priority != manifest[j].Priority {
			return manifest[i].Priority > manifest[j].Priority
		}
		return manifest[i].RuleID < manifest[j].RuleID
	})
	sv := domain.SkillVersion{
		ID:           a.ids.NewID("sv"),
		TenantID:     tenantID,
		WorkflowSlug: workflowSlug,
		Version:      version,
		RuleManifest: manifest,
		Status:       "active",
		CreatedAt:    now,
	}
	if err := a.store.CreateSkillVersion(ctx, sv); err != nil {
		return domain.SkillVersion{}, err
	}
	_, err = a.store.CreateAuditEvent(ctx, auditEvent(a.ids, now, tenantID, "skill_version_created", "system", "promotion_pipeline", "skill_version", sv.ID, nil, map[string]any{
		"workflow_slug": workflowSlug,
		"rule_count":    len(manifest),
	}, "skill-version", tenantID, sv.ID))
	if err != nil {
		return domain.SkillVersion{}, err
	}
	return sv, nil
}

func (a *App) evaluateDecision(now time.Time, run domain.CaseRun, payload map[string]any, sv domain.SkillVersion) domain.DecisionPoint {
	decisionType, defaultAction := workflowDecision(run.WorkflowSlug)
	action := defaultAction
	for _, rule := range sv.RuleManifest {
		if rule.DecisionType == decisionType && conditionsMatch(payload, rule.ConditionsCanonical) {
			action = rule.RecommendedAction
			break
		}
	}
	agentInput := cloneMap(payload)
	agentInput["workflow_slug"] = run.WorkflowSlug
	return domain.DecisionPoint{
		ID:             a.ids.NewID("dp"),
		TenantID:       run.TenantID,
		CaseRunID:      run.ID,
		DecisionType:   decisionType,
		AgentInput:     agentInput,
		ProposedAction: action,
		Status:         "waiting_for_approval",
		CreatedAt:      now,
	}
}

func (a *App) buildArtifact(now time.Time, run domain.CaseRun, point domain.DecisionPoint) domain.Artifact {
	return domain.Artifact{
		ID:              a.ids.NewID("art"),
		TenantID:        run.TenantID,
		CaseRunID:       run.ID,
		DecisionPointID: point.ID,
		ArtifactType:    "draft_email",
		StoragePath:     fmt.Sprintf("/%s/sandbox/%s/artifacts/draft_email.json", run.TenantID, run.ID),
		Content: map[string]any{
			"proposed_action": point.ProposedAction,
			"workflow_slug":   run.WorkflowSlug,
		},
		CreatedAt: now,
	}
}

func (a *App) buildReviewPacket(now time.Time, run domain.CaseRun, point domain.DecisionPoint, artifact domain.Artifact) domain.ReviewPacket {
	expiresAt := now.Add(48 * time.Hour)
	return domain.ReviewPacket{
		PacketID:        a.ids.NewID("pkt"),
		SchemaVersion:   "1.1",
		TenantID:        run.TenantID,
		CaseRunID:       run.ID,
		DecisionPointID: point.ID,
		WorkflowType:    run.WorkflowSlug,
		Mode:            run.Mode,
		Trigger: map[string]any{
			"summary":        "Sandbox case ready for review",
			"source_channel": "fixture",
			"received_at":    now.Format(time.RFC3339),
		},
		Facts: factsFromPayload(run.InputPayload),
		PlannedAction: domain.PlannedAction{
			Type:             point.ProposedAction,
			Description:      point.ProposedAction,
			IsDestructive:    false,
			RequiresApproval: true,
		},
		ArtifactPreview: domain.ArtifactPreview{
			Type:       artifact.ArtifactType,
			PreviewURL: fmt.Sprintf("/preview/%s", artifact.ID),
			ExpiresAt:  expiresAt,
		},
		ButtonSet: []domain.ActionButton{
			domain.ActionApprove,
			domain.ActionWrongFacts,
			domain.ActionWrongAction,
			domain.ActionMissingCondition,
			domain.ActionUseDifferentTemplate,
			domain.ActionAddNote,
		},
		ExpiresAt:      expiresAt,
		IdempotencyKey: domain.SHA256Key(run.TenantID, point.ID, "packet", "1"),
	}
}

func defaultWorkflowTemplates(vertical string) []domain.WorkflowTemplate {
	return []domain.WorkflowTemplate{
		{ID: "wt_enquiry_" + vertical, Slug: "enquiry_triage", Vertical: vertical, DisplayName: "Enquiry triage", DecisionTypes: []string{"route_or_reply"}},
		{ID: "wt_quote_" + vertical, Slug: "quote_drafting", Vertical: vertical, DisplayName: "Quote drafting", DecisionTypes: []string{"send_or_hold"}},
		{ID: "wt_invoice_" + vertical, Slug: "invoice_handling", Vertical: vertical, DisplayName: "Invoice handling", DecisionTypes: []string{"tax_treatment"}},
	}
}

func workflowDecision(workflowSlug string) (string, string) {
	switch workflowSlug {
	case "invoice_handling":
		return "tax_treatment", "apply_gst"
	case "enquiry_triage":
		return "route_or_reply", "draft_reply"
	default:
		return "send_or_hold", "send_quote"
	}
}

func workflowFromCaseInput(input map[string]any) string {
	if value, ok := input["workflow_slug"].(string); ok && value != "" {
		return value
	}
	return "quote_drafting"
}

func structuredRuleFromCorrection(c domain.Correction, point domain.DecisionPoint) ([]domain.Condition, string) {
	text := strings.ToLower(c.FreeText + " " + c.FollowUpAnswer)
	switch {
	case strings.Contains(text, "singapore") || strings.Contains(text, "no gst"):
		return []domain.Condition{{Field: "supplier_country", Operator: "!=", Value: "AU"}}, "apply_no_gst"
	case strings.Contains(text, "commercial") && strings.Contains(text, "template"):
		return []domain.Condition{{Field: "enquiry_type", Operator: "=", Value: "commercial"}}, "use_corporate_template"
	case strings.Contains(text, "send it anyway") || strings.Contains(text, "go ahead"):
		return []domain.Condition{
			{Field: "photos_complete", Operator: "=", Value: boolValue(point.AgentInput, "photos_complete")},
			{Field: "client_type", Operator: "=", Value: stringValue(point.AgentInput, "client_type", "new")},
		}, "send_quote"
	case strings.Contains(text, "repeat") || strings.Contains(text, "known client"):
		return []domain.Condition{
			{Field: "photos_complete", Operator: "=", Value: boolValue(point.AgentInput, "photos_complete")},
			{Field: "client_type", Operator: "=", Value: "repeat"},
		}, "send_quote"
	case strings.Contains(text, "photo") || strings.Contains(text, "hold") || strings.Contains(text, "more info"):
		return []domain.Condition{
			{Field: "photos_complete", Operator: "=", Value: false},
			{Field: "client_type", Operator: "=", Value: stringValue(point.AgentInput, "client_type", "new")},
		}, "hold_and_request_more_info"
	default:
		if c.ActionButton == domain.ActionUseDifferentTemplate {
			return []domain.Condition{{Field: "enquiry_type", Operator: "=", Value: stringValue(point.AgentInput, "enquiry_type", "commercial")}}, "use_corporate_template"
		}
		return nil, ""
	}
}

func conditionsMatch(payload map[string]any, conditions []domain.Condition) bool {
	for _, condition := range conditions {
		actual, ok := payload[condition.Field]
		switch condition.Operator {
		case "=":
			if !ok || fmt.Sprint(actual) != fmt.Sprint(condition.Value) {
				return false
			}
		case "!=":
			if ok && fmt.Sprint(actual) == fmt.Sprint(condition.Value) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func redactAggregateConditions(conditions []domain.Condition) ([]domain.Condition, bool) {
	out := make([]domain.Condition, 0, len(conditions))
	quarantine := false
	for _, condition := range conditions {
		redacted := condition
		value := fmt.Sprint(condition.Value)
		lowerField := strings.ToLower(condition.Field)
		switch {
		case strings.Contains(value, "@"):
			redacted.Value = "<email>"
		case strings.Contains(lowerField, "client_name") || strings.Contains(lowerField, "customer_name"):
			redacted.Value = "<quarantined:freetext>"
			quarantine = true
		case looksLikeFreeText(value):
			redacted.Value = "<quarantined:freetext>"
			quarantine = true
		}
		out = append(out, redacted)
	}
	return out, quarantine
}

func looksLikeFreeText(value string) bool {
	if value == "" {
		return false
	}
	allowed := map[string]struct{}{
		"true": {}, "false": {}, "new": {}, "repeat": {}, "commercial": {}, "residential": {}, "AU": {}, "SG": {},
	}
	if _, ok := allowed[value]; ok {
		return false
	}
	return strings.Contains(value, " ")
}

func factsFromPayload(payload map[string]any) []domain.Fact {
	keys := make([]string, 0, len(payload))
	for key := range payload {
		if key != "sandbox" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	facts := make([]domain.Fact, 0, len(keys))
	for _, key := range keys {
		facts = append(facts, domain.Fact{Label: key, Value: fmt.Sprint(payload[key]), Confidence: 1})
	}
	return facts
}

func auditEvent(ids IDGenerator, now time.Time, tenantID, eventType, actorType, actorID, refType, refID string, related map[string]any, payload map[string]any, keyParts ...string) domain.AuditEvent {
	return domain.AuditEvent{
		ID:             ids.NewID("ae"),
		TenantID:       tenantID,
		EventType:      eventType,
		ActorType:      actorType,
		ActorID:        actorID,
		RefType:        refType,
		RefID:          refID,
		RelatedIDs:     cloneMap(related),
		Payload:        cloneMap(payload),
		IdempotencyKey: domain.SHA256Key(keyParts...),
		OccurredAt:     now,
	}
}

func scopePriority(scope domain.Scope) int {
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

func boolValue(payload map[string]any, key string) bool {
	value, ok := payload[key]
	if !ok {
		return false
	}
	if b, ok := value.(bool); ok {
		return b
	}
	return fmt.Sprint(value) == "true"
}

func stringValue(payload map[string]any, key, fallback string) string {
	if value, ok := payload[key]; ok && fmt.Sprint(value) != "" {
		return fmt.Sprint(value)
	}
	return fallback
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func contains(values []string, value string) bool {
	for _, current := range values {
		if current == value {
			return true
		}
	}
	return false
}

func appendIfMissing(values []string, value string) []string {
	if contains(values, value) {
		return values
	}
	return append(values, value)
}
