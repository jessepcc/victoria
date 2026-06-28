package domain

import "time"

type Mode string

const (
	ModeSandbox Mode = "sandbox"
	ModeLive    Mode = "live"
)

func ParseMode(value string) Mode {
	switch value {
	case string(ModeLive):
		return ModeLive
	default:
		return ModeSandbox
	}
}

func (m Mode) ValidForWorkflowInput() bool {
	return m == ModeSandbox || m == ModeLive
}

type Scope string

const (
	ScopeCase     Scope = "case"
	ScopeTenant   Scope = "tenant"
	ScopeVertical Scope = "vertical"
	ScopeDefault  Scope = "default"
)

type ActionButton string

const (
	ActionApprove              ActionButton = "approve"
	ActionWrongFacts           ActionButton = "wrong_facts"
	ActionWrongAction          ActionButton = "wrong_action"
	ActionMissingCondition     ActionButton = "missing_condition"
	ActionUseDifferentTemplate ActionButton = "use_different_template"
	ActionAddNote              ActionButton = "add_note"
)

func (a ActionButton) Valid() bool {
	switch a {
	case ActionApprove, ActionWrongFacts, ActionWrongAction, ActionMissingCondition, ActionUseDifferentTemplate, ActionAddNote:
		return true
	default:
		return false
	}
}

func (a ActionButton) SignalName() string {
	if a == ActionApprove {
		return "CaseApproved"
	}
	return "CaseRejected"
}

type Tenant struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Vertical    string    `json:"vertical"`
	Status      string    `json:"status"`
	DefaultMode Mode      `json:"default_mode"`
	CreatedAt   time.Time `json:"created_at"`
}

type ProvisioningManifest struct {
	TenantID             string            `json:"tenant_id"`
	HermesVersion        string            `json:"hermes_version"`
	Mode                 Mode              `json:"mode"`
	WorkflowTemplates    []string          `json:"workflow_templates"`
	MCPEndpoints         map[string]string `json:"mcp_endpoints"`
	SkillVersionEndpoint string            `json:"skill_version_endpoint"`
	Vertical             string            `json:"vertical"`
	// WhatsAppCommandSecret is the single-use secret an A1 operator types in
	// `register me as operator <secret>` from their own WhatsApp account
	// (spec §6.2 / OQ-3). Surfaced exactly once at provisioning + on reissue;
	// never returned by routine binding fetches.
	WhatsAppCommandSecret string `json:"whatsapp_command_secret,omitempty"`
}

type WorkflowTemplate struct {
	ID            string   `json:"id"`
	Slug          string   `json:"slug"`
	Vertical      string   `json:"vertical,omitempty"`
	DisplayName   string   `json:"display_name"`
	DecisionTypes []string `json:"decision_types"`
}

type CaseRun struct {
	ID                string         `json:"id"`
	TenantID          string         `json:"tenant_id"`
	WorkflowSlug      string         `json:"workflow_slug"`
	Mode              Mode           `json:"mode"`
	InputPayload      map[string]any `json:"input_payload"`
	InputHash         string         `json:"input_hash"`
	SkillVersionID    string         `json:"skill_version_id"`
	ReplayedFromID    string         `json:"replayed_from_id,omitempty"`
	Status            string         `json:"status"`
	DecisionPointID   string         `json:"decision_point_id,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
	CompletedAt       *time.Time     `json:"completed_at,omitempty"`
	ReplayTemperature float64        `json:"replay_temperature"`
}

type DecisionPoint struct {
	ID             string         `json:"id"`
	TenantID       string         `json:"tenant_id"`
	CaseRunID      string         `json:"case_run_id"`
	DecisionType   string         `json:"decision_type"`
	AgentInput     map[string]any `json:"agent_input"`
	ProposedAction string         `json:"proposed_action"`
	Status         string         `json:"status"`
	CreatedAt      time.Time      `json:"created_at"`
}

type Artifact struct {
	ID              string         `json:"id"`
	TenantID        string         `json:"tenant_id"`
	CaseRunID       string         `json:"case_run_id"`
	DecisionPointID string         `json:"decision_point_id"`
	ArtifactType    string         `json:"artifact_type"`
	StoragePath     string         `json:"storage_path"`
	Content         map[string]any `json:"content"`
	CreatedAt       time.Time      `json:"created_at"`
}

type ReviewPacket struct {
	PacketID        string          `json:"packet_id"`
	SchemaVersion   string          `json:"schema_version"`
	TenantID        string          `json:"tenant_id"`
	CaseRunID       string          `json:"case_run_id"`
	DecisionPointID string          `json:"decision_point_id"`
	WorkflowType    string          `json:"workflow_type"`
	Mode            Mode            `json:"mode"`
	Trigger         map[string]any  `json:"trigger"`
	Facts           []Fact          `json:"facts"`
	PlannedAction   PlannedAction   `json:"planned_action"`
	DraftReply      string          `json:"draft_reply,omitempty"`
	ArtifactPreview ArtifactPreview `json:"artifact_preview"`
	ButtonSet       []ActionButton  `json:"button_set"`
	ExpiresAt       time.Time       `json:"expires_at"`
	IdempotencyKey  string          `json:"idempotency_key"`
	Delivered       bool            `json:"delivered"`
}

type Fact struct {
	Label      string  `json:"label"`
	Value      string  `json:"value"`
	Confidence float64 `json:"confidence"`
}

type PlannedAction struct {
	Type             string `json:"type"`
	Description      string `json:"description"`
	IsDestructive    bool   `json:"is_destructive"`
	RequiresApproval bool   `json:"requires_approval"`
}

type ArtifactPreview struct {
	Type               string    `json:"type"`
	PreviewURL         string    `json:"preview_url"`
	InlineThumbnailURL string    `json:"inline_thumbnail_url,omitempty"`
	ExpiresAt          time.Time `json:"expires_at"`
}

type ApprovalSignalEnvelope struct {
	SchemaVersion       string       `json:"schema_version"`
	SignalID            string       `json:"signal_id"`
	IdempotencyKey      string       `json:"idempotency_key"`
	PacketID            string       `json:"packet_id"`
	CaseRunID           string       `json:"case_run_id"`
	DecisionPointID     string       `json:"decision_point_id"`
	TenantID            string       `json:"tenant_id"`
	OperatorID          string       `json:"operator_id"`
	Channel             string       `json:"channel"`
	SourceMessageID     string       `json:"source_message_id"`
	RawInboundMessageID string       `json:"raw_inbound_message_id"`
	TS                  time.Time    `json:"ts"`
	ActionButton        ActionButton `json:"action_button"`
	FreeText            string       `json:"free_text,omitempty"`
	FollowUpAnswer      string       `json:"follow_up_answer,omitempty"`
	ScopeHint           *Scope       `json:"scope_hint,omitempty"`
	ParserMethod        string       `json:"parser_method"`
	ParserConfidence    float64      `json:"parser_confidence"`
}

type Correction struct {
	ID                  string       `json:"id"`
	SchemaVersion       string       `json:"schema_version"`
	IdempotencyKey      string       `json:"idempotency_key"`
	PacketID            string       `json:"packet_id"`
	CaseRunID           string       `json:"case_run_id"`
	DecisionPointID     string       `json:"decision_point_id"`
	TenantID            string       `json:"tenant_id"`
	OperatorID          string       `json:"operator_id"`
	Channel             string       `json:"channel"`
	SourceMessageID     string       `json:"source_message_id"`
	RawInboundMessageID string       `json:"raw_inbound_message_id"`
	ActionButton        ActionButton `json:"action_button"`
	FreeText            string       `json:"free_text,omitempty"`
	FollowUpAnswer      string       `json:"follow_up_answer,omitempty"`
	ScopeHint           *Scope       `json:"scope_hint,omitempty"`
	ParseMethod         string       `json:"parse_method"`
	ParseConfidence     float64      `json:"parse_confidence"`
	CreatedAt           time.Time    `json:"created_at"`
}

type Condition struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    any    `json:"value"`
}

type RuleCandidate struct {
	ID                  string      `json:"id"`
	TenantID            string      `json:"tenant_id"`
	WorkflowSlug        string      `json:"workflow_slug"`
	DecisionType        string      `json:"decision_type"`
	ConditionsHash      string      `json:"conditions_hash"`
	ConditionsCanonical []Condition `json:"conditions_canonical"`
	RecommendedAction   string      `json:"recommended_action"`
	Scope               Scope       `json:"scope"`
	Confidence          float64     `json:"confidence"`
	EvidenceCount       int         `json:"evidence_count"`
	ContradictingCount  int         `json:"contradicting_count"`
	SourceCorrectionIDs []string    `json:"source_correction_ids"`
	SourceCaseRunIDs    []string    `json:"source_case_run_ids"`
	Status              string      `json:"status"`
	CreatedAt           time.Time   `json:"created_at"`
	LastSeenAt          time.Time   `json:"last_seen_at"`
	UnderReviewAt       *time.Time  `json:"under_review_at,omitempty"`
}

type ValidatedRule struct {
	ID                      string      `json:"id"`
	TenantID                string      `json:"tenant_id,omitempty"`
	WorkflowSlug            string      `json:"workflow_slug"`
	DecisionType            string      `json:"decision_type"`
	ConditionsHash          string      `json:"conditions_hash"`
	ConditionsCanonical     []Condition `json:"conditions_canonical"`
	RecommendedAction       string      `json:"recommended_action"`
	Scope                   Scope       `json:"scope"`
	Vertical                string      `json:"vertical,omitempty"`
	Version                 int         `json:"version"`
	Supersedes              string      `json:"supersedes,omitempty"`
	PromotedFromCandidateID string      `json:"promoted_from_candidate_id,omitempty"`
	PromotedBy              string      `json:"promoted_by"`
	PromotedAt              time.Time   `json:"promoted_at"`
	Status                  string      `json:"status"`
	Rationale               string      `json:"rationale,omitempty"`
	RollbackOf              string      `json:"rollback_of,omitempty"`
}

type SkillVersion struct {
	ID           string         `json:"id"`
	TenantID     string         `json:"tenant_id"`
	WorkflowSlug string         `json:"workflow_slug"`
	Version      int            `json:"version"`
	RuleManifest []RuleManifest `json:"rule_manifest"`
	Status       string         `json:"status"`
	CreatedAt    time.Time      `json:"created_at"`
}

type RuleManifest struct {
	RuleID              string      `json:"rule_id"`
	Scope               Scope       `json:"scope"`
	Version             int         `json:"version"`
	DecisionType        string      `json:"decision_type"`
	ConditionsCanonical []Condition `json:"conditions_canonical"`
	RecommendedAction   string      `json:"recommended_action"`
	Priority            int         `json:"priority"`
}

type AuditEvent struct {
	ID             string         `json:"id"`
	TenantID       string         `json:"tenant_id"`
	EventType      string         `json:"event_type"`
	ActorType      string         `json:"actor_type"`
	ActorID        string         `json:"actor_id"`
	RefType        string         `json:"ref_type"`
	RefID          string         `json:"ref_id"`
	RelatedIDs     map[string]any `json:"related_ids,omitempty"`
	Payload        map[string]any `json:"payload,omitempty"`
	IdempotencyKey string         `json:"idempotency_key"`
	OccurredAt     time.Time      `json:"occurred_at"`
}

type InboundMode string

const (
	InboundModeReadOnly    InboundMode = "read_only"
	InboundModeFullControl InboundMode = "full_control"
)

func ParseInboundMode(value string) InboundMode {
	switch value {
	case string(InboundModeFullControl):
		return InboundModeFullControl
	default:
		return InboundModeReadOnly
	}
}

func (m InboundMode) Valid() bool {
	return m == InboundModeReadOnly || m == InboundModeFullControl
}

type ChannelBinding struct {
	TenantID                    string        `json:"tenant_id"`
	Channel                     string        `json:"channel"`
	ProviderNumber              string        `json:"provider_number"`
	OperatorID                  string        `json:"operator_id"`
	SessionStatus               SessionStatus `json:"session_status"`
	SessionUpdated              time.Time     `json:"session_updated_at"`
	InboundMode                 InboundMode   `json:"inbound_mode,omitempty"`
	CommandIdentities           []string      `json:"command_identities,omitempty"`
	CustomerAllowlist           []string      `json:"customer_allowlist,omitempty"`
	ConsentAcknowledgedAt       *time.Time    `json:"consent_acknowledged_at,omitempty"`
	CustomerIntakePausedUntil   *time.Time    `json:"customer_intake_paused_until,omitempty"`
	RetentionMinutes            int           `json:"retention_minutes,omitempty"`
	DraftDeliveryJID            string        `json:"draft_delivery_jid,omitempty"`
	OperatorJID                 string        `json:"operator_jid,omitempty"`
	TelegramCustomerChats       []string      `json:"telegram_customer_chats,omitempty"`
	CommandRegistrationSecret   string        `json:"command_registration_secret,omitempty"`
	CommandSecretConsumedAt     *time.Time    `json:"command_secret_consumed_at,omitempty"`
	LastRepairAt                *time.Time    `json:"last_repair_at,omitempty"`
	BanGraceHours               int           `json:"ban_grace_hours,omitempty"`
	CustomerOutboundPausedUntil *time.Time    `json:"customer_outbound_paused_until,omitempty"`
}

type CustomerMessage struct {
	ID                 string         `json:"id"`
	TenantID           string         `json:"tenant_id"`
	Channel            string         `json:"channel"`
	SourceMessageID    string         `json:"source_message_id"`
	CustomerIdentifier string         `json:"customer_identifier"`
	ReceivedAt         time.Time      `json:"received_at"`
	Subject            string         `json:"subject,omitempty"`
	BodyText           string         `json:"body_text"`
	Metadata           map[string]any `json:"metadata,omitempty"`
	CaseRunID          string         `json:"case_run_id,omitempty"`
	Status             string         `json:"status"`
}

type OutboundToCustomer struct {
	ID                  string     `json:"id"`
	TenantID            string     `json:"tenant_id"`
	CaseRunID           string     `json:"case_run_id"`
	Channel             string     `json:"channel"`
	RecipientIdentifier string     `json:"recipient_identifier"`
	BodyHash            string     `json:"body_hash"`
	MCPApprovalAuditID  string     `json:"mcp_approval_audit_id"`
	ProviderMessageID   string     `json:"provider_message_id,omitempty"`
	SentAt              *time.Time `json:"sent_at,omitempty"`
	Status              string     `json:"status"`
}

// SessionStatus tracks the WhatsApp/Telegram session lifecycle per
// operator-ux §4.7.1 state machine.
type SessionStatus string

const (
	SessionUnknown      SessionStatus = ""
	SessionQRNeeded     SessionStatus = "qr_needed"
	SessionConnecting   SessionStatus = "connecting"
	SessionActive       SessionStatus = "active"
	SessionDisconnected SessionStatus = "disconnected"
	SessionSuspended    SessionStatus = "suspended"
)

func (s SessionStatus) IsConnected() bool {
	return s == SessionActive
}

// OutboundQueueEntry is a packet awaiting delivery while the session is
// not Active. Persisted across process restarts (operator-ux §4.7.4 / WA-INV-3).
type OutboundQueueEntry struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	Channel        string    `json:"channel"`
	PacketID       string    `json:"packet_id"`
	IdempotencyKey string    `json:"idempotency_key"`
	EnqueuedAt     time.Time `json:"enqueued_at"`
}

type MCPRequest struct {
	TenantHeader    string `json:"tenant_header"`
	CaseRunID       string `json:"case_run_id"`
	DecisionPointID string `json:"decision_point_id"`
	Mode            Mode   `json:"mode"`
	ToolName        string `json:"tool_name"`
	IdempotencyKey  string `json:"idempotency_key"`
}

type MCPResult struct {
	OK    bool   `json:"ok"`
	Code  string `json:"code,omitempty"`
	Error string `json:"error,omitempty"`
}
