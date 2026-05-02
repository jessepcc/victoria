package app

import (
	"context"

	"victoria/internal/domain"
)

type Store interface {
	CreateTenant(ctx context.Context, tenant domain.Tenant, manifest domain.ProvisioningManifest, binding domain.ChannelBinding, initialSkillVersion domain.SkillVersion) error
	GetTenant(ctx context.Context, tenantID string) (domain.Tenant, error)
	ListTenants(ctx context.Context) ([]domain.Tenant, error)

	UpsertWorkflowTemplate(ctx context.Context, tmpl domain.WorkflowTemplate) error
	GetWorkflowTemplate(ctx context.Context, slug string) (domain.WorkflowTemplate, error)

	UpsertChannelBinding(ctx context.Context, binding domain.ChannelBinding) error
	ChannelBindingByProvider(ctx context.Context, channel, providerNumber string) (domain.ChannelBinding, error)
	ChannelBindingByTenant(ctx context.Context, tenantID, channel string) (domain.ChannelBinding, error)
	UpdateChannelSessionStatus(ctx context.Context, tenantID, channel string, status domain.SessionStatus) error
	ListChannelBindingsByChannel(ctx context.Context, channel string) ([]domain.ChannelBinding, error)

	EnqueueOutbound(ctx context.Context, entry domain.OutboundQueueEntry) error
	ListOutboundQueue(ctx context.Context, tenantID, channel string) ([]domain.OutboundQueueEntry, error)
	OutboundQueueDepth(ctx context.Context, tenantID, channel string) (int, error)
	DeleteOutboundQueueEntry(ctx context.Context, id string) error
	DeleteOldestOutboundQueueEntry(ctx context.Context, tenantID, channel string) (string, error)

	CreateCaseRun(ctx context.Context, run domain.CaseRun) error
	UpdateCaseRunStatus(ctx context.Context, tenantID, caseRunID, status string) error
	GetCaseRun(ctx context.Context, tenantID, caseRunID string) (domain.CaseRun, error)

	CreateDecisionPoint(ctx context.Context, point domain.DecisionPoint) error
	UpdateDecisionPointStatus(ctx context.Context, tenantID, decisionPointID, status string) error
	GetDecisionPoint(ctx context.Context, tenantID, decisionPointID string) (domain.DecisionPoint, error)

	CreateArtifact(ctx context.Context, artifact domain.Artifact) error
	ListArtifacts(ctx context.Context, tenantID, caseRunID string) ([]domain.Artifact, error)

	CreateReviewPacket(ctx context.Context, packet domain.ReviewPacket) error
	GetReviewPacket(ctx context.Context, tenantID, packetID string) (domain.ReviewPacket, error)
	MarkReviewPacketDelivered(ctx context.Context, tenantID, packetID string) error
	LatestReviewPacket(ctx context.Context, tenantID string) (domain.ReviewPacket, error)

	CreateAuditEvent(ctx context.Context, event domain.AuditEvent) (bool, error)
	ListAuditEvents(ctx context.Context, tenantID string) ([]domain.AuditEvent, error)
	HasApprovalAudit(ctx context.Context, tenantID, caseRunID, decisionPointID string) (bool, error)

	SeenSignal(ctx context.Context, signalID string) (bool, error)
	MarkSignalSeen(ctx context.Context, signalID string) error

	CreateCorrection(ctx context.Context, correction domain.Correction) (bool, error)
	ListCorrections(ctx context.Context, tenantID string) ([]domain.Correction, error)

	FindCandidate(ctx context.Context, tenantID, workflowSlug, decisionType, conditionsHash string, recommendedAction string) (domain.RuleCandidate, error)
	ListCandidatesByConditions(ctx context.Context, tenantID, workflowSlug, decisionType, conditionsHash string) ([]domain.RuleCandidate, error)
	ListCandidates(ctx context.Context, tenantID string) ([]domain.RuleCandidate, error)
	GetCandidate(ctx context.Context, tenantID, candidateID string) (domain.RuleCandidate, error)
	SaveCandidate(ctx context.Context, candidate domain.RuleCandidate) error

	CreateValidatedRule(ctx context.Context, rule domain.ValidatedRule) error
	DeprecateActiveRule(ctx context.Context, tenantID, workflowSlug, decisionType, conditionsHash string) (string, int, error)
	ListVisibleValidatedRules(ctx context.Context, tenantID, workflowSlug string) ([]domain.ValidatedRule, error)
	ListTenantValidatedRules(ctx context.Context, tenantID string) ([]domain.ValidatedRule, error)

	CreateSkillVersion(ctx context.Context, sv domain.SkillVersion) error
	ActiveSkillVersion(ctx context.Context, tenantID, workflowSlug string) (domain.SkillVersion, error)
	GetSkillVersion(ctx context.Context, tenantID, skillVersionID string) (domain.SkillVersion, error)
}
