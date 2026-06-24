package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/jessepcc/victoria/internal/domain"
)

type Store struct {
	mu sync.RWMutex

	tenants       map[string]domain.Tenant
	templates     map[string]domain.WorkflowTemplate
	bindings      map[string]domain.ChannelBinding
	caseRuns      map[string]domain.CaseRun
	decisionPts   map[string]domain.DecisionPoint
	artifacts     map[string]domain.Artifact
	packets       map[string]domain.ReviewPacket
	auditEvents   map[string]domain.AuditEvent
	auditByKey    map[string]string
	customerMsgs  map[string]domain.CustomerMessage
	customerByKey map[string]string
	customerOut   map[string]domain.OutboundToCustomer
	outByKey      map[string]string
	signals       map[string]struct{}
	corrections   map[string]domain.Correction
	correctionKey map[string]string
	candidates    map[string]domain.RuleCandidate
	validated     map[string]domain.ValidatedRule
	skillVersions map[string]domain.SkillVersion
	activeSV      map[string]string
	outboundQueue map[string]domain.OutboundQueueEntry
	queueSeq      int
}

func New() *Store {
	return &Store{
		tenants:       map[string]domain.Tenant{},
		templates:     map[string]domain.WorkflowTemplate{},
		bindings:      map[string]domain.ChannelBinding{},
		caseRuns:      map[string]domain.CaseRun{},
		decisionPts:   map[string]domain.DecisionPoint{},
		artifacts:     map[string]domain.Artifact{},
		packets:       map[string]domain.ReviewPacket{},
		auditEvents:   map[string]domain.AuditEvent{},
		auditByKey:    map[string]string{},
		customerMsgs:  map[string]domain.CustomerMessage{},
		customerByKey: map[string]string{},
		customerOut:   map[string]domain.OutboundToCustomer{},
		outByKey:      map[string]string{},
		signals:       map[string]struct{}{},
		corrections:   map[string]domain.Correction{},
		correctionKey: map[string]string{},
		candidates:    map[string]domain.RuleCandidate{},
		validated:     map[string]domain.ValidatedRule{},
		skillVersions: map[string]domain.SkillVersion{},
		activeSV:      map[string]string{},
		outboundQueue: map[string]domain.OutboundQueueEntry{},
	}
}

func (s *Store) CreateTenant(_ context.Context, tenant domain.Tenant, _ domain.ProvisioningManifest, binding domain.ChannelBinding, initialSkillVersion domain.SkillVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tenants[tenant.ID]; ok {
		return domain.ErrDuplicate
	}
	s.tenants[tenant.ID] = tenant
	s.bindings[bindingKey(binding.Channel, binding.ProviderNumber)] = binding
	s.skillVersions[initialSkillVersion.ID] = cloneSkillVersion(initialSkillVersion)
	s.activeSV[svKey(initialSkillVersion.TenantID, initialSkillVersion.WorkflowSlug)] = initialSkillVersion.ID
	return nil
}

func (s *Store) GetTenant(_ context.Context, tenantID string) (domain.Tenant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tenant, ok := s.tenants[tenantID]
	if !ok {
		return domain.Tenant{}, domain.ErrNotFound
	}
	return tenant, nil
}

func (s *Store) ListTenants(_ context.Context) ([]domain.Tenant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.Tenant, 0, len(s.tenants))
	for _, tenant := range s.tenants {
		out = append(out, tenant)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Store) UpsertWorkflowTemplate(_ context.Context, tmpl domain.WorkflowTemplate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.templates[tmpl.Slug] = tmpl
	return nil
}

func (s *Store) GetWorkflowTemplate(_ context.Context, slug string) (domain.WorkflowTemplate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tmpl, ok := s.templates[slug]
	if !ok {
		return domain.WorkflowTemplate{}, domain.ErrNotFound
	}
	return tmpl, nil
}

func (s *Store) UpsertChannelBinding(_ context.Context, binding domain.ChannelBinding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bindings[bindingKey(binding.Channel, binding.ProviderNumber)] = binding
	return nil
}

func (s *Store) ChannelBindingByProvider(_ context.Context, channel, providerNumber string) (domain.ChannelBinding, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	binding, ok := s.bindings[bindingKey(channel, providerNumber)]
	if !ok {
		return domain.ChannelBinding{}, domain.ErrNotFound
	}
	return binding, nil
}

func (s *Store) ChannelBindingByTenant(_ context.Context, tenantID, channel string) (domain.ChannelBinding, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, binding := range s.bindings {
		if binding.TenantID == tenantID && binding.Channel == channel {
			return binding, nil
		}
	}
	return domain.ChannelBinding{}, domain.ErrNotFound
}

func (s *Store) UpdateChannelSessionStatus(_ context.Context, tenantID, channel string, status domain.SessionStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, binding := range s.bindings {
		if binding.TenantID == tenantID && binding.Channel == channel {
			binding.SessionStatus = status
			binding.SessionUpdated = time.Now().UTC()
			s.bindings[key] = binding
			return nil
		}
	}
	return domain.ErrNotFound
}

func (s *Store) ListChannelBindingsByChannel(_ context.Context, channel string) ([]domain.ChannelBinding, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []domain.ChannelBinding
	for _, binding := range s.bindings {
		if binding.Channel == channel {
			out = append(out, binding)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TenantID < out[j].TenantID })
	return out, nil
}

func (s *Store) EnqueueOutbound(_ context.Context, entry domain.OutboundQueueEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry.ID == "" {
		s.queueSeq++
		entry.ID = fmt.Sprintf("oq_%d", s.queueSeq)
	}
	s.outboundQueue[entry.ID] = entry
	return nil
}

func (s *Store) ListOutboundQueue(_ context.Context, tenantID, channel string) ([]domain.OutboundQueueEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []domain.OutboundQueueEntry
	for _, entry := range s.outboundQueue {
		if entry.TenantID == tenantID && entry.Channel == channel {
			out = append(out, entry)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EnqueuedAt.Before(out[j].EnqueuedAt) })
	return out, nil
}

func (s *Store) OutboundQueueDepth(_ context.Context, tenantID, channel string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, entry := range s.outboundQueue {
		if entry.TenantID == tenantID && entry.Channel == channel {
			count++
		}
	}
	return count, nil
}

func (s *Store) DeleteOutboundQueueEntry(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.outboundQueue, id)
	return nil
}

func (s *Store) DeleteOldestOutboundQueueEntry(_ context.Context, tenantID, channel string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var oldestID string
	var oldestAt time.Time
	for id, entry := range s.outboundQueue {
		if entry.TenantID != tenantID || entry.Channel != channel {
			continue
		}
		if oldestID == "" || entry.EnqueuedAt.Before(oldestAt) {
			oldestID = id
			oldestAt = entry.EnqueuedAt
		}
	}
	if oldestID == "" {
		return "", nil
	}
	delete(s.outboundQueue, oldestID)
	return oldestID, nil
}

func (s *Store) CreateCaseRun(_ context.Context, run domain.CaseRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.caseRuns[run.ID] = cloneCaseRun(run)
	return nil
}

func (s *Store) UpdateCaseRunStatus(_ context.Context, tenantID, caseRunID, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.caseRuns[caseRunID]
	if !ok || run.TenantID != tenantID {
		return domain.ErrNotFound
	}
	run.Status = status
	s.caseRuns[caseRunID] = run
	return nil
}

func (s *Store) GetCaseRun(_ context.Context, tenantID, caseRunID string) (domain.CaseRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.caseRuns[caseRunID]
	if !ok || run.TenantID != tenantID {
		return domain.CaseRun{}, domain.ErrNotFound
	}
	return cloneCaseRun(run), nil
}

func (s *Store) CreateDecisionPoint(_ context.Context, point domain.DecisionPoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.decisionPts[point.ID] = cloneDecisionPoint(point)
	return nil
}

func (s *Store) UpdateDecisionPointStatus(_ context.Context, tenantID, decisionPointID, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	point, ok := s.decisionPts[decisionPointID]
	if !ok || point.TenantID != tenantID {
		return domain.ErrNotFound
	}
	point.Status = status
	s.decisionPts[decisionPointID] = point
	return nil
}

func (s *Store) GetDecisionPoint(_ context.Context, tenantID, decisionPointID string) (domain.DecisionPoint, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	point, ok := s.decisionPts[decisionPointID]
	if !ok || point.TenantID != tenantID {
		return domain.DecisionPoint{}, domain.ErrNotFound
	}
	return cloneDecisionPoint(point), nil
}

func (s *Store) CreateArtifact(_ context.Context, artifact domain.Artifact) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.artifacts[artifact.ID] = cloneArtifact(artifact)
	return nil
}

func (s *Store) ListArtifacts(_ context.Context, tenantID, caseRunID string) ([]domain.Artifact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []domain.Artifact
	for _, artifact := range s.artifacts {
		if artifact.TenantID == tenantID && artifact.CaseRunID == caseRunID {
			out = append(out, cloneArtifact(artifact))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Store) CreateReviewPacket(_ context.Context, packet domain.ReviewPacket) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.packets[packet.PacketID] = packet
	return nil
}

func (s *Store) GetReviewPacket(_ context.Context, tenantID, packetID string) (domain.ReviewPacket, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	packet, ok := s.packets[packetID]
	if !ok || packet.TenantID != tenantID {
		return domain.ReviewPacket{}, domain.ErrNotFound
	}
	return packet, nil
}

func (s *Store) LatestReviewPacket(_ context.Context, tenantID string) (domain.ReviewPacket, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var latest domain.ReviewPacket
	for _, packet := range s.packets {
		if packet.TenantID != tenantID {
			continue
		}
		if latest.PacketID == "" || packet.ExpiresAt.After(latest.ExpiresAt) {
			latest = packet
		}
	}
	if latest.PacketID == "" {
		return domain.ReviewPacket{}, domain.ErrNotFound
	}
	return latest, nil
}

func (s *Store) MarkReviewPacketDelivered(_ context.Context, tenantID, packetID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	packet, ok := s.packets[packetID]
	if !ok || packet.TenantID != tenantID {
		return domain.ErrNotFound
	}
	packet.Delivered = true
	s.packets[packetID] = packet
	return nil
}

func (s *Store) CreateAuditEvent(_ context.Context, event domain.AuditEvent) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.auditByKey[event.IdempotencyKey]; ok {
		_ = id
		return false, nil
	}
	s.auditEvents[event.ID] = cloneAuditEvent(event)
	s.auditByKey[event.IdempotencyKey] = event.ID
	return true, nil
}

func (s *Store) ListAuditEvents(_ context.Context, tenantID string) ([]domain.AuditEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []domain.AuditEvent
	for _, event := range s.auditEvents {
		if tenantID == "" || event.TenantID == tenantID {
			out = append(out, cloneAuditEvent(event))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].OccurredAt.Equal(out[j].OccurredAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].OccurredAt.Before(out[j].OccurredAt)
	})
	return out, nil
}

func (s *Store) HasApprovalAudit(_ context.Context, tenantID, caseRunID, decisionPointID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, event := range s.auditEvents {
		if event.TenantID == tenantID && event.EventType == "approval_received" && event.RefID == decisionPointID {
			if event.RelatedIDs["case_run_id"] == caseRunID {
				return true, nil
			}
		}
	}
	return false, nil
}

func (s *Store) ApprovalAuditID(_ context.Context, tenantID, caseRunID, decisionPointID string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, event := range s.auditEvents {
		if event.TenantID == tenantID && event.EventType == "approval_received" && event.RefID == decisionPointID {
			if event.RelatedIDs["case_run_id"] == caseRunID {
				return event.ID, nil
			}
		}
	}
	return "", domain.ErrNotFound
}

func (s *Store) CreateCustomerMessage(_ context.Context, msg domain.CustomerMessage) (bool, domain.CustomerMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := customerMessageKey(msg.TenantID, msg.Channel, msg.SourceMessageID)
	if id, ok := s.customerByKey[key]; ok {
		return false, cloneCustomerMessage(s.customerMsgs[id]), nil
	}
	s.customerMsgs[msg.ID] = cloneCustomerMessage(msg)
	s.customerByKey[key] = msg.ID
	return true, cloneCustomerMessage(msg), nil
}

func (s *Store) CustomerMessageBySource(_ context.Context, tenantID, channel, sourceMessageID string) (domain.CustomerMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.customerByKey[customerMessageKey(tenantID, channel, sourceMessageID)]
	if !ok {
		return domain.CustomerMessage{}, domain.ErrNotFound
	}
	return cloneCustomerMessage(s.customerMsgs[id]), nil
}

func (s *Store) UpdateCustomerMessageCase(_ context.Context, tenantID, channel, sourceMessageID, caseRunID, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.customerByKey[customerMessageKey(tenantID, channel, sourceMessageID)]
	if !ok {
		return domain.ErrNotFound
	}
	msg := s.customerMsgs[id]
	msg.CaseRunID = caseRunID
	msg.Status = status
	s.customerMsgs[id] = cloneCustomerMessage(msg)
	return nil
}

func (s *Store) UpsertOutboundToCustomer(_ context.Context, out domain.OutboundToCustomer) (bool, domain.OutboundToCustomer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := outboundCustomerKey(out.TenantID, out.CaseRunID, out.BodyHash)
	if id, ok := s.outByKey[key]; ok {
		existing := s.customerOut[id]
		if existing.Status != "sent" && out.Status == "sent" {
			out.ID = existing.ID
			s.customerOut[id] = cloneOutboundToCustomer(out)
			return false, cloneOutboundToCustomer(out), nil
		}
		return false, cloneOutboundToCustomer(existing), nil
	}
	s.customerOut[out.ID] = cloneOutboundToCustomer(out)
	s.outByKey[key] = out.ID
	return true, cloneOutboundToCustomer(out), nil
}

func (s *Store) OutboundToCustomerByCaseAndHash(_ context.Context, tenantID, caseRunID, bodyHash string) (domain.OutboundToCustomer, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.outByKey[outboundCustomerKey(tenantID, caseRunID, bodyHash)]
	if !ok {
		return domain.OutboundToCustomer{}, domain.ErrNotFound
	}
	return cloneOutboundToCustomer(s.customerOut[id]), nil
}

func (s *Store) SeenSignal(_ context.Context, signalID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.signals[signalID]
	return ok, nil
}

func (s *Store) MarkSignalSeen(_ context.Context, signalID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.signals[signalID] = struct{}{}
	return nil
}

func (s *Store) CreateCorrection(_ context.Context, correction domain.Correction) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.correctionKey[correction.IdempotencyKey]; ok {
		return false, nil
	}
	s.corrections[correction.ID] = cloneCorrection(correction)
	s.correctionKey[correction.IdempotencyKey] = correction.ID
	return true, nil
}

func (s *Store) ListCorrections(_ context.Context, tenantID string) ([]domain.Correction, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []domain.Correction
	for _, correction := range s.corrections {
		if correction.TenantID == tenantID {
			out = append(out, cloneCorrection(correction))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) FindCandidate(_ context.Context, tenantID, workflowSlug, decisionType, conditionsHash string, recommendedAction string) (domain.RuleCandidate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, candidate := range s.candidates {
		if candidate.TenantID == tenantID &&
			candidate.WorkflowSlug == workflowSlug &&
			candidate.DecisionType == decisionType &&
			candidate.ConditionsHash == conditionsHash &&
			candidate.RecommendedAction == recommendedAction &&
			candidate.Status != "rejected" {
			return cloneCandidate(candidate), nil
		}
	}
	return domain.RuleCandidate{}, domain.ErrNotFound
}

func (s *Store) ListCandidatesByConditions(_ context.Context, tenantID, workflowSlug, decisionType, conditionsHash string) ([]domain.RuleCandidate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []domain.RuleCandidate
	for _, candidate := range s.candidates {
		if candidate.TenantID == tenantID &&
			candidate.WorkflowSlug == workflowSlug &&
			candidate.DecisionType == decisionType &&
			candidate.ConditionsHash == conditionsHash &&
			candidate.Status != "rejected" {
			out = append(out, cloneCandidate(candidate))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Store) ListCandidates(_ context.Context, tenantID string) ([]domain.RuleCandidate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []domain.RuleCandidate
	for _, candidate := range s.candidates {
		if candidate.TenantID == tenantID {
			out = append(out, cloneCandidate(candidate))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Store) GetCandidate(_ context.Context, tenantID, candidateID string) (domain.RuleCandidate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	candidate, ok := s.candidates[candidateID]
	if !ok || candidate.TenantID != tenantID {
		return domain.RuleCandidate{}, domain.ErrNotFound
	}
	return cloneCandidate(candidate), nil
}

func (s *Store) SaveCandidate(_ context.Context, candidate domain.RuleCandidate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.candidates[candidate.ID] = cloneCandidate(candidate)
	return nil
}

func (s *Store) CreateValidatedRule(_ context.Context, rule domain.ValidatedRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.validated[rule.ID] = cloneRule(rule)
	return nil
}

func (s *Store) DeprecateActiveRule(_ context.Context, tenantID, workflowSlug, decisionType, conditionsHash string) (string, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	version := 1
	var supersedes string
	for id, rule := range s.validated {
		if rule.TenantID == tenantID && rule.WorkflowSlug == workflowSlug && rule.DecisionType == decisionType && rule.ConditionsHash == conditionsHash && rule.Status == "active" {
			rule.Status = "deprecated"
			s.validated[id] = rule
			supersedes = rule.ID
			version = rule.Version + 1
		}
	}
	return supersedes, version, nil
}

func (s *Store) ListVisibleValidatedRules(_ context.Context, tenantID, workflowSlug string) ([]domain.ValidatedRule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tenant, ok := s.tenants[tenantID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	var out []domain.ValidatedRule
	for _, rule := range s.validated {
		if rule.WorkflowSlug != workflowSlug || rule.Status != "active" {
			continue
		}
		if rule.Scope == domain.ScopeDefault ||
			(rule.Scope == domain.ScopeVertical && rule.Vertical == tenant.Vertical) ||
			(rule.TenantID == tenantID) {
			out = append(out, cloneRule(rule))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Scope != out[j].Scope {
			return scopeRank(out[i].Scope) > scopeRank(out[j].Scope)
		}
		return out[i].PromotedAt.After(out[j].PromotedAt)
	})
	return out, nil
}

func (s *Store) ListTenantValidatedRules(_ context.Context, tenantID string) ([]domain.ValidatedRule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []domain.ValidatedRule
	for _, rule := range s.validated {
		if rule.TenantID == tenantID || rule.Scope == domain.ScopeDefault {
			out = append(out, cloneRule(rule))
		}
	}
	return out, nil
}

func (s *Store) CreateSkillVersion(_ context.Context, sv domain.SkillVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, existing := range s.skillVersions {
		if existing.TenantID == sv.TenantID && existing.WorkflowSlug == sv.WorkflowSlug && existing.Status == "active" {
			existing.Status = "deprecated"
			s.skillVersions[id] = existing
		}
	}
	s.skillVersions[sv.ID] = cloneSkillVersion(sv)
	s.activeSV[svKey(sv.TenantID, sv.WorkflowSlug)] = sv.ID
	return nil
}

func (s *Store) ActiveSkillVersion(_ context.Context, tenantID, workflowSlug string) (domain.SkillVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.activeSV[svKey(tenantID, workflowSlug)]
	if !ok && workflowSlug != "quote_drafting" {
		id, ok = s.activeSV[svKey(tenantID, "quote_drafting")]
	}
	if !ok {
		return domain.SkillVersion{}, domain.ErrNotFound
	}
	sv, ok := s.skillVersions[id]
	if !ok {
		return domain.SkillVersion{}, domain.ErrNotFound
	}
	out := cloneSkillVersion(sv)
	out.WorkflowSlug = workflowSlug
	return out, nil
}

func (s *Store) GetSkillVersion(_ context.Context, tenantID, skillVersionID string) (domain.SkillVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sv, ok := s.skillVersions[skillVersionID]
	if !ok || sv.TenantID != tenantID {
		return domain.SkillVersion{}, domain.ErrNotFound
	}
	return cloneSkillVersion(sv), nil
}

func bindingKey(channel, providerNumber string) string {
	return channel + "\x00" + providerNumber
}

func customerMessageKey(tenantID, channel, sourceMessageID string) string {
	return tenantID + "\x00" + channel + "\x00" + sourceMessageID
}

func outboundCustomerKey(tenantID, caseRunID, bodyHash string) string {
	return tenantID + "\x00" + caseRunID + "\x00" + bodyHash
}

func svKey(tenantID, workflowSlug string) string {
	return tenantID + "\x00" + workflowSlug
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

func cloneCaseRun(in domain.CaseRun) domain.CaseRun {
	in.InputPayload = cloneMap(in.InputPayload)
	return in
}

func cloneDecisionPoint(in domain.DecisionPoint) domain.DecisionPoint {
	in.AgentInput = cloneMap(in.AgentInput)
	return in
}

func cloneArtifact(in domain.Artifact) domain.Artifact {
	in.Content = cloneMap(in.Content)
	return in
}

func cloneAuditEvent(in domain.AuditEvent) domain.AuditEvent {
	in.RelatedIDs = cloneMap(in.RelatedIDs)
	in.Payload = cloneMap(in.Payload)
	return in
}

func cloneCustomerMessage(in domain.CustomerMessage) domain.CustomerMessage {
	in.Metadata = cloneMap(in.Metadata)
	return in
}

func cloneOutboundToCustomer(in domain.OutboundToCustomer) domain.OutboundToCustomer {
	if in.SentAt != nil {
		t := *in.SentAt
		in.SentAt = &t
	}
	return in
}

func cloneCorrection(in domain.Correction) domain.Correction {
	if in.ScopeHint != nil {
		scope := *in.ScopeHint
		in.ScopeHint = &scope
	}
	return in
}

func cloneCandidate(in domain.RuleCandidate) domain.RuleCandidate {
	in.ConditionsCanonical = append([]domain.Condition(nil), in.ConditionsCanonical...)
	in.SourceCorrectionIDs = append([]string(nil), in.SourceCorrectionIDs...)
	in.SourceCaseRunIDs = append([]string(nil), in.SourceCaseRunIDs...)
	if in.UnderReviewAt != nil {
		t := *in.UnderReviewAt
		in.UnderReviewAt = &t
	}
	return in
}

func cloneRule(in domain.ValidatedRule) domain.ValidatedRule {
	in.ConditionsCanonical = append([]domain.Condition(nil), in.ConditionsCanonical...)
	return in
}

func cloneSkillVersion(in domain.SkillVersion) domain.SkillVersion {
	in.RuleManifest = append([]domain.RuleManifest(nil), in.RuleManifest...)
	for i := range in.RuleManifest {
		in.RuleManifest[i].ConditionsCanonical = append([]domain.Condition(nil), in.RuleManifest[i].ConditionsCanonical...)
	}
	return in
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
