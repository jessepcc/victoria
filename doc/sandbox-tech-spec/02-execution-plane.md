# 02 — Execution Plane Technical Specification

**Component:** Execution Plane / Agent Runtime
**Status:** Round 5 — FINAL (development handoff)
**Author:** Execution Plane Architect
**Runtime:** Sonnet 4.6

---

## 0. Document Conventions

- **DECISION:** A position this spec commits to and will defend.
- **ASSUMPTION:** A dependency on a peer architect's component.
- **INVARIANT:** A property that must hold at all times. Each invariant maps to a named contract test in §10.
- **OUT OF SCOPE:** Acknowledged but explicitly excluded; owned by another architect.
- **RESOLVED-Xn:** Conflict resolved against another spec; resolution recorded inline at the relevant section.
- **OPEN-Xn:** Conflict still open; needs further reconciliation.

---

## 0.1 Conflict Resolution Index (R1 → R5)

### Round 2 resolutions (binding)

| ID | Topic | Status | Section(s) |
|---|---|---|---|
| C1 | ValidatedRule storage location | RESOLVED (yield: consume via single `LoadSkillVersion` endpoint; weighed in for learning-loop's split) | §8 |
| C2 | Single name/schema for runtime rule artifact | RESOLVED (`SkillVersion`; `LoadSkillVersion` activity; `skill_version_id` replaces `rule_snapshot_id`) | §3.4, §4.4, §8 |
| C3 | Approval signal transport | RESOLVED (gateway → Temporal SDK direct; `WorkflowID = case_run_id`; Temporal owns the timeout) | §7 |
| C4 | Provisioning manifest delivery | RESOLVED (config-mount at boot; HTTP+HMAC for runtime RPCs) | §2.7, §13 |
| C5 | Replay determinism binding | RESOLVED (4 commitments) | §4.3 |
| C6 | WRITE_EXTERNAL approval check inside MCP server | RESOLVED (synchronous `SELECT` on `audit_events`; missing `mode` defaults to sandbox) | §5.6 |

### Round 3 resolutions (this round)

| ID | Topic | Status | Section(s) |
|---|---|---|---|
| C12 | Unified `validated_rules` DDL/RLS in control-plane DB | RESOLVED (consume; effective-set resolution moves into `LoadSkillVersion`; no per-tenant rule reads from execution DB) | §8.2, §8.5 |
| C13 | `ApprovalSignalEnvelope` payload completeness for `PersistCorrection` | RESOLVED (C13-R5: operator-ux §2.2 delivers the canonical **16-field** envelope; R4's `correction_id` and `parse_status` reverted from wire) | §7.4 |
| C14 | Field-name alignment (`conditions_canonical` / `recommended_action`) | RESOLVED (adopt verbatim; manifest passes through unmodified into Hermes context block) | §8.3 |
| N1 | Sandbox-mode canonical encoding | RESOLVED (single canonical type: `mode: 'sandbox' \| 'live'` enum across workflow input, MCP env var, audit event, manifest) | §0.2, §3.1, §3.4, §4.4, §5, §6.3 |
| N6 | Idempotency key composition rule | RESOLVED (consume ctrl-plane's derivation rule; full key inventory listed) | §10.4 |
| OQ-NEW-3 | Internal auth method | RESOLVED (accepted ctrl-plane's pick; **revised in R4** to mTLS — see R4 below) | §13.1 |
| OPEN-OQ5 → REPLAY-SCHED | Replay scheduler trigger API | RESOLVED (accept ctrl-plane Evaluation Service as scheduler; defined `POST /internal/replay` endpoint shape, idempotency, preconditions) | §13.3 |
| HERMES-VOL | Rule manifests never on Hermes data volume | RESOLVED (asserted explicitly; new INV-H6; contract test added) | §2.3, §10.6 |
| MCP-SNAPSHOT | `mcp_read_snapshot` artifact_type + `mcp_audit_reader` role | RESOLVED (concrete enum/role grant pushed to learning-loop; this spec is the consumer) | §4.3, §5.6 |

### Round 4 resolutions (this round)

| ID | Topic | Status | Section(s) |
|---|---|---|---|
| OQ-NEW-3 (revised) | Service-to-service auth: **mTLS** (ctrl-plane §6.2 reversed R3 HMAC framing) | RESOLVED (accept mTLS; cert SAN `t_<tenant_id>.exec.*`; one mechanism for all internal calls) | §13.1 |
| Audit storage topology | **Single authoritative `audit_events` in control-plane DB**; per-tenant `audit_events_outbox` is a durable drain buffer only — not a read source (RESOLVED-R4-CONFLICT-2). MCP preflight reads control-plane store over mTLS. | RESOLVED (revised R5 single-store) | §5.6, §7.4, §13.4 |
| MCP read scope (former OPEN-N7) | Read via `mcp_approval_events` view (learning-loop §11.5) scoped to `event_type='approval_received' AND tenant_id=current_tenant` | RESOLVED-N7 | §5.6 |
| OPEN-N6-FOLLOWUP | Idempotency derivation: yield to ctrl-plane §17 SHA-256 derivation rule (replaces R3 UUID dedup column-by-column) | RESOLVED-N6 | §15 |
| OPEN-C13-FOLLOWUP | Operator-ux R3 §2.2 envelope adopted; field naming reconciled (`parser_method`/`parser_confidence`, not `parse_method`/`parse_confidence`) | RESOLVED-C13 | §7.4 |
| TDD ordering audit | §6 (sandbox seed) and §13 (RPC surface) re-ordered: invariants/contracts before implementation | RESOLVED | §6.0, §13.0 |
| Cross-component contract test list | New top-level table binding every peer-boundary test to producer/consumer spec sections | RESOLVED | §11 |
| Property/fuzz tests | Replay determinism (N derived); MCP 3-gate fuzz (full Cartesian); sandbox-mode escape | RESOLVED | §10.8 |
| SC-01..SC-08 mapping | Every devils-advocate scenario mapped to named exec-plane contract tests | RESOLVED | §10.7 |
| Storage topology contribution | Per-tenant execution DB tables this spec writes/reads | RESOLVED | §13.4 |

### Round 5 resolutions (this round)

| ID | Topic | Status | Section(s) |
|---|---|---|---|
| R4-CONFLICT-2 (audit topology) | Single authoritative `audit_events` in control plane + per-tenant `audit_events_outbox` drain buffer; MCP preflight reads control-plane store via mTLS over `mcp_audit_reader` + `mcp_approval_events` view | RESOLVED-R5 | §5.6, §14.4 |
| OPEN-Temporal-IAM | Path A (native IAM) default; path B (sidecar) fallback per ctrl-plane §6.8; exec-plane signal handler identical for both | RESOLVED-R5 | §11 (`ct_temporal_signal_iam_path_a`/`b`) |
| AUDIT-OUTBOX | Schema row added to learning-loop §2.0 / §2.10 canonical topology | RESOLVED-R5 | §14.4 |
| AUDIT-DRAIN | Drain semantics live with ctrl-plane §9.4 `/internal/audit/events` + SHA-256 idempotency (§17) | RESOLVED-R5 | §13, §16 |
| Implementation Handoff | Single-page handoff section added | RESOLVED-R5 | §18 |

### Final OPEN list (categorized)

| ID | Category | Status | Gating |
|---|---|---|---|
| OPEN-AUDIT-LATENCY-R4 | OPEN-MEASUREMENT-1 | Acknowledged | Intra-VPC mTLS read on the `write_final` hot path is budgeted at ~5–10ms; needs production measurement to confirm. Tracked at ctrl-plane §9.3. Does NOT block development. |

---

## 0.2 Canonical types adopted across the spec (R3 N1)

To eliminate the four-encoding problem flagged by Devil's Advocate, this spec uses ONE canonical encoding per concept:

| Concept | Canonical type | Wire shape | Forbidden encodings |
|---|---|---|---|
| Sandbox vs. live | `mode: "sandbox" \| "live"` enum string | `"mode": "sandbox"` | boolean `sandbox_mode: true`; integer `mode: 0`; presence/absence of a flag |
| Tenant identifier | `tenant_id: string` (prefix `t_`) | `"tenant_id": "t_abc123"` | bare integers; UUIDs without prefix |
| Case run identifier | `case_run_id: string` (prefix `cr_`) | `"case_run_id": "cr_xyz789"` | UUIDs without prefix |
| Decision point identifier | `decision_point_id: string` (prefix `dp_`) | `"decision_point_id": "dp_a1b2"` | sequence numbers alone |
| Skill version identifier | `skill_version_id: string` (prefix `sv_`) | `"skill_version_id": "sv_r7q1"` | numeric versions; rule_snapshot_id (deprecated) |

**Mode default-deny:** A missing or unrecognized `mode` value is treated as `"sandbox"`. Never `"live"`. (See INV-T5.)

**Migration from R1/R2 boolean:** Any reference to `sandbox_mode: boolean` in earlier rounds is superseded. The R2 `HERMES_SANDBOX_MODE` env var is renamed to `HERMES_MODE` with values `sandbox` | `live`.

---

## 1. Purpose and Scope

The execution plane is the per-client runtime unit responsible for orchestrating agent reasoning, durable workflow state, and sandboxed tool execution on behalf of one tenant. It runs in isolation from every other tenant's execution plane and is the only component that ever touches a tenant's live credentials or persistent agent memory.

### What the execution plane owns

- Hermes container lifecycle: image version, data volume, credential injection, MCP process adjacency.
- Temporal worker process: workflow definitions for all supported workflow templates, activity implementations, signal handlers, retry/timeout policies.
- MCP sidecar processes: sandbox-email, sandbox-drive, sandbox-invoice — capability surface, idempotency, sandbox-vs-live mode enforcement.
- The sandbox case seed pipeline: how dummy fixtures arrive, how they are tagged, and how sandbox isolation is enforced through the tool layer.
- The approval-gate mechanism: how Hermes proposes an action, how Temporal waits for an operator signal, how a response re-enters the workflow.
- ValidatedRule consumption: how the agent receives approved business rules at case execution time.

### Firm boundary with the control plane

The control plane provisions the execution plane but never shares runtime context with it. Specifically:

| Boundary | Control plane side | Execution plane side |
|---|---|---|
| Provisioning | Writes tenant record; kicks off execution plane bootstrap | Reads provisioning manifest once at startup |
| Rules | Stores ValidatedRules (vertical/default in control-plane DB; tenant/case in per-tenant DB per learning-loop §2); generates `SkillVersion` manifests on promotion | Loads the active `SkillVersion` at workflow-start via the `LoadSkillVersion` activity; never writes rules or skill versions back |
| Signals | Messaging gateway holds tenant-bound auth and emits Temporal signals directly via the Temporal SDK; control plane is not on the signal path | Receives signals on `WorkflowID = case_run_id` in the tenant's task queue; signal handler verifies tenant binding |
| Observability | Receives OTEL spans and structured logs | Emits traces; does not pull from observability stack |
| Secrets | Creates and rotates secrets in the tenant's secret scope | Reads its own secret scope at activity-start time |

The execution plane has **no inbound network surface**. It pulls configuration from the control plane at boot; it receives operator signals through Temporal via the messaging gateway's signal-delivery path. No HTTP endpoint on the execution plane is reachable from outside.

OUT OF SCOPE: API gateway, tenant registry, billing, evaluation service, observability backend (Control Plane Architect).

---

## 2. Hermes Runtime Specification

### 2.1 Invariants

- **INV-H1:** Every tenant runs exactly one Hermes container. No two tenants share a container.
- **INV-H2:** The Hermes data volume for tenant T is mounted exclusively to tenant T's container and never reused or shared with another container.
- **INV-H3:** The container's root filesystem is read-only. Only two explicit writable volumes exist: the Hermes data volume and a small `/tmp` workspace cleared on restart.
- **INV-H4:** The `HERMES_TENANT_ID` env var in the container exactly equals the tenant ID on the mounted data volume manifest. A mismatch halts the container.
- **INV-H5:** Network egress from the container is limited to the LLM provider endpoint allowlist and the Temporal cluster. No other outbound connection is permitted.

Contract tests mapping to these invariants are listed in §10.1.

### 2.2 Container image and version pinning

```
Image:   nousresearch/hermes:<MAJOR>.<MINOR>.<PATCH>
Example: nousresearch/hermes:0.3.4
```

**DECISION:** Floating tags (`latest`, `stable`) are prohibited. The pinned digest must be stored in the tenant's provisioning record. Version updates require a new provisioning record entry and a container restart; they are not hot-swapped.

**Rationale:** The product spec (§Security Architecture, point 6) requires reproducibility, rollback, and forensic clarity. Floating tags violate all three.

Update policy: patch bumps may be applied per-tenant with a rolling restart. Minor bumps require regression replay against that tenant's historical CaseRun set before deployment. Major bumps require internal review.

### 2.3 Mounted volumes

| Volume name | Mount path | Purpose | Writable |
|---|---|---|---|
| `hermes-data-{tenant_id}` | `/hermes/data` | Persistent memory, skills, persona file | Yes |
| `tmp-scratch` | `/tmp` | Model output scratch; cleared on restart | Yes |
| `provisioning-manifest` (read-only config map) | `/hermes/config/manifest.json` | Tenant ID, workflow template names, rule endpoint | No |

The Hermes data volume path is `/hermes/data/{tenant_id}/` with subdirectories:
- `/hermes/data/{tenant_id}/memory/` — Hermes long-term memory store
- `/hermes/data/{tenant_id}/skills/` — **persona + starter skills only** (see INV-H6)
- `/hermes/data/{tenant_id}/persona/` — tenant persona configuration

The data volume is created during tenant provisioning and tagged with a `tenant_id` label. The container entrypoint verifies that the manifest's `tenant_id` field matches the volume label before proceeding.

**INV-H6 (RESOLVED-HERMES-VOL).** ValidatedRule manifests, `SkillVersion` JSON, or any rendering of `rule_manifest[]` MUST NEVER be written to the Hermes data volume. The volume holds **only** the tenant persona file and the starter skill files seeded at bootstrap (§2.7). All rules flow through Temporal activity inputs (`LoadSkillVersion` → `InvokeHermesReasoning`) and exist in process memory for the duration of a workflow execution; nothing is persisted to disk.

Rationale:
- A rolled-back rule must take effect on the next workflow start, not after a Hermes container restart.
- Skill files on disk would create a second source of truth and drift from `validated_rules` in the control plane.
- The `skills/` directory is read-only at runtime once the bootstrap step completes; the contract test below (§10.6) snapshots the directory contents at bootstrap and asserts no writes occur during workflow execution.

Contract test (§10.6 `test_no_rule_content_on_hermes_volume`): start a tenant, run a workflow that fetches a non-empty `SkillVersion`, complete the workflow, then enumerate `/hermes/data/{tenant_id}/skills/` and assert (a) the file count is identical to the bootstrap-time count, (b) no file's content has changed (SHA-256 match against the bootstrap snapshot), (c) no file under that subtree contains any `rule_id`, `conditions_canonical`, or `recommended_action` substring.

### 2.4 Environment variables

| Variable | Source | Purpose |
|---|---|---|
| `HERMES_TENANT_ID` | Provisioning manifest | Identifies this runtime; used in all log output and spans |
| `HERMES_DATA_PATH` | Fixed: `/hermes/data` | Root of the persistent data volume |
| `HERMES_MCP_ENDPOINTS` | Provisioning manifest | JSON array of MCP server addresses scoped to this tenant |
| `HERMES_SKILL_VERSION_ENDPOINT` | Provisioning manifest | Control plane URL for `LoadSkillVersion` (read-only, HMAC token injected) |
| `HERMES_DEFAULT_TEMPERATURE` | Provisioning manifest | LLM temperature; default 0.2 for live runs, **forced to 0 on replays** (see §4.3, RESOLVED-C5) |
| `HERMES_LLM_PROVIDER` | Secret scope | LLM provider base URL |
| `HERMES_LLM_MODEL` | Provisioning manifest | Model name string (e.g. `claude-sonnet-4-6`) |
| `HERMES_WORKFLOW_TEMPLATES` | Provisioning manifest | Comma-separated list of enabled workflow types |
| `HERMES_MODE` | Provisioning manifest | Enum: `sandbox` \| `live`. Default-deny: missing or unrecognized value resolves to `sandbox` (RESOLVED-N1). |

No secrets appear in environment variables. All credential material is fetched from the tenant secret scope at activity-start time by the Temporal worker (see §2.5).

**DECISION:** `HERMES_MODE` is a provisioning-time setting, not a runtime toggle. It cannot be flipped without redeployment. This prevents a live-mode accidental fire.

### 2.5 Credential injection

Credentials are not baked into the container image, not passed as environment variables, and not present on the data volume. The Temporal worker fetches them at activity-start using the tenant's workload identity (IAM role or equivalent) to authenticate against the tenant-scoped secret store.

Pattern: `SecretFetcher.get(tenant_id, secret_key)` → plaintext credential, consumed only for the duration of the activity. Credentials are never written to disk or persisted in Temporal workflow history.

**DECISION:** Activities fetch credentials at activity-start time, not workflow-start time. This ensures that credential rotations propagate to running workflows without requiring a workflow restart.

### 2.6 Network egress allowlist (per container)

Outbound connections are restricted by container network policy:

| Destination | Port | Purpose |
|---|---|---|
| Anthropic API endpoint | 443 | LLM inference |
| Temporal cluster (internal) | 7233 | Workflow coordination |
| Internal MCP sidecar addresses | Local network only | Tool invocation |
| Control plane rule endpoint | 443 | ValidatedRule fetch (read-only) |

All other outbound connections are blocked. The MCP sidecars themselves have their own egress policies (see §4).

### 2.7 Bootstrap on tenant provisioning

When a new tenant is provisioned, the following bootstrap sequence executes:

1. **Volume creation:** The `hermes-data-{tenant_id}` volume is created and tagged.
2. **Persona file write:** A starter persona file is written to `/hermes/data/{tenant_id}/persona/persona.json`, populated from the vertical's default persona template.
3. **Skill seed:** Default skills for the tenant's enabled workflow templates are copied to `/hermes/data/{tenant_id}/skills/`. These are read-only starter skills, not learned skills.
4. **Provisioning manifest write:** The manifest config map is written with tenant ID, MCP endpoints, model, and enabled workflow types.
5. **Container start:** The Hermes container starts, verifies the manifest/volume tenant ID match (INV-H4), and enters standby awaiting the first workflow dispatch.
6. **MCP sidecar start:** Sidecar processes start in the same task group, load tenant credentials from the secret scope, and register their endpoints in the manifest.

The provisioning manifest shape:

```json
{
  "tenant_id": "t_123",
  "hermes_version": "0.3.4",
  "llm_model": "claude-sonnet-4-6",
  "mode": "sandbox",
  "workflow_templates": ["enquiry_triage", "quote_drafting", "invoice_handling"],
  "mcp_endpoints": {
    "sandbox_email": "http://localhost:3001",
    "sandbox_drive": "http://localhost:3002",
    "sandbox_invoice": "http://localhost:3003"
  },
  "skill_version_endpoint": "https://control-plane.victoria.internal/api/v1/skill-versions",
  "vertical": "roofing"
}
```

**RESOLVED-C4 (with Control Plane Architect):** The provisioning manifest is delivered as a **read-only config-mount**, not via an HTTP call to the execution plane. The handoff binds to the control plane's `TenantProvisioningWorkflow` (`01-control-plane.md` §5.2) as follows:

| Provisioning step (control plane) | Execution plane effect |
|---|---|
| `ProvisionPostgresDatabase` (step 4) | Writes per-tenant DB connection-string secret ID into the manifest |
| `ProvisionObjectStoreBucket` (step 5) | Writes bucket prefix |
| `RegisterTemporalTaskQueues` (step 6) | Writes task-queue prefix `victoria.tenant.{tenant_id}` |
| `DeployHermesContainer` (step 7) | Writes the manifest JSON to orchestrator config storage; mounts at `/hermes/config/manifest.json`; mounts `hermes-data-{tenant_id}` volume |
| `DeployMCPSidecars` (step 8) | Each MCP sidecar receives `TENANT_ID`, `MODE` (= `sandbox` \| `live`), `DB_URL`, and a per-server allowed-tools list as env vars from the same secret scope |
| `DeployTemporalWorker` (step 9) | Worker process starts pointed at the registered queues; loads HMAC secret for outbound HTTP-RPC calls |
| `HealthCheck` (step 10) | Container entrypoint reads manifest, verifies INV-H4 (tenant_id label match), responds healthy on `/internal/health` |

The manifest is **immutable for the lifetime of the container**. Manifest changes (e.g. version bumps, sandbox-to-shadow promotion) require redeploy.

**Distinction from runtime RPCs:** Control plane → execution plane *runtime* calls (rule push, replay trigger, audit pull-drain) DO use HTTP+HMAC against `/internal/*` endpoints (per `01-control-plane.md` §5.3). Bootstrap is config-mount only; runtime is HTTP+HMAC. These are two separate channels.

**RESOLVED-C1 (consumption side, weighing in):** The manifest carries `skill_version_endpoint`, not a generic `rule_endpoint`. The execution plane does not care whether tenant rules and vertical/default rules live in the same DB or different DBs. It calls `LoadSkillVersion(tenant_id, workflow_slug, as_of?)` on a single endpoint that returns a fully resolved `SkillVersion` manifest (per `03-correction-loop.md` §9.2). The endpoint hides storage layout. Weighing in: learning-loop's split (tenant/case in per-tenant DB; vertical/default in control plane DB; `validated_rules_shared`) is a better isolation posture than ctrl-plane's "all rules in control plane" — it prevents tenant-private corrections from ever sitting alongside cross-tenant data. Learning-loop's resolver is the right side of the C1 split. The execution plane consumes the result either way.

---

## 3. Temporal Layout

### 3.1 Invariants

- **INV-T1:** Every CaseRun maps to exactly one Temporal WorkflowExecution. The Temporal WorkflowID equals the CaseRun ID.
- **INV-T2:** Replays create a new CaseRun and a new WorkflowExecution; they never reset or mutate the original execution's history.
- **INV-T3:** No `write_final`-class activity may execute without (a) a `CaseApproved` signal in the current workflow execution's history AND (b) a corresponding `approval_received` AuditEvent row written by the signal handler. The MCP server independently re-checks (b) (see §5.6, RESOLVED-C6).
- **INV-T4:** A workflow execution in a tenant's task queue is unreachable from any other tenant's task queue, Temporal client, or worker process.
- **INV-T5 (default-deny, R3 N1):** If `mode` is missing from the workflow input, the workflow refuses to start with `MalformedInputError`. If `MODE` is unset or unrecognized at any MCP server, the server defaults to `mode = "sandbox"`. Mode-absent never resolves to `"live"`. The canonical encoding is the enum string `"sandbox"` | `"live"` everywhere; booleans are forbidden (RESOLVED-N1).

### 3.2 Namespace vs. task queue decision

**DECISION:** Phase 1 uses a **single shared Temporal namespace** with **one task queue per tenant**.

Task queue naming convention: `victoria.tenant.{tenant_id}.{workflow_type}` — e.g. `victoria.tenant.t_123.enquiry_triage`.

**Rationale:** The product spec (§Tenant Setup, Phase 1 row) names "task queues per tenant" as the isolation mechanism. Namespace-per-tenant (Phase 3 isolation) is reserved for regulated/enterprise clients. A dedicated namespace requires dedicated Temporal server capacity and elevated operational overhead that Phase 1 does not warrant. Task queues provide the needed isolation: a Temporal worker only polls its own queue; cross-tenant dispatch is impossible unless you deliberately target the wrong queue name, which is checked by INV-T4.

The worker process within the execution plane registers exclusively against its tenant's task queues. It does not poll any shared or default queue.

### 3.3 Workflow type definitions

Three workflow types are supported in Phase 1.

#### EnquiryTriageWorkflow

Handles a new inbound customer enquiry from trigger to disposition.

```
Input:  { case_run_id, tenant_id, mode, trigger: EnquiryTrigger }   // mode: "sandbox" | "live"
Output: { disposition: TriageDisposition, artifacts: ArtifactRef[], approval_chain: ApprovalRecord[] }

Signals accepted:
  - CaseApproved(decision_point_id, approved_by, idempotency_key)
  - CaseRejected(decision_point_id, correction: CorrectionPayload, idempotency_key)
  - CaseAborted(reason)

Queries:
  - GetCurrentState() → WorkflowState
  - GetPendingDecisionPoint() → DecisionPoint | null
```

Steps (high-level): extract facts → classify enquiry type → apply ValidatedRules → draft response artifacts → gate on `CaseApproved` signal → conditionally send draft (if approved) → emit audit events → complete.

#### QuoteDraftingWorkflow

Produces a draft quote/proposal for an operator's review.

```
Input:  { case_run_id, tenant_id, mode, trigger: QuoteTrigger }   // mode: "sandbox" | "live"
Output: { quote_artifact: ArtifactRef, send_decision: SendDecision, approval_chain: ApprovalRecord[] }

Signals accepted:
  - CaseApproved(decision_point_id, approved_by, idempotency_key)
  - CaseRejected(decision_point_id, correction: CorrectionPayload, idempotency_key)
  - QuoteAmended(decision_point_id, amendments: QuoteAmendment[], idempotency_key)
  - CaseAborted(reason)
```

Steps: extract quote context → check completeness (photos, client type) → apply ValidatedRules → draft quote document → draft reply message → gate on approval signal → conditionally finalize → emit audit.

#### InvoiceHandlingWorkflow

Processes an inbound supplier invoice document.

```
Input:  { case_run_id, tenant_id, mode, trigger: InvoiceTrigger }   // mode: "sandbox" | "live"
Output: { invoice_object: InvoiceRef, coding_suggestion: CodingSuggestion, approval_chain: ApprovalRecord[] }

Signals accepted:
  - CaseApproved(decision_point_id, approved_by, idempotency_key)
  - CaseRejected(decision_point_id, correction: CorrectionPayload, idempotency_key)
  - CaseAborted(reason)
```

Steps: parse invoice document → extract supplier, line items, tax fields → apply ValidatedRules (tax treatment, supplier classification) → draft coding suggestion → gate on approval → conditionally write to accounting draft → emit audit.

### 3.4 Activity catalog

Activities are the smallest durable units of work. Each activity is idempotent when called with the same input (ensured by idempotency keys derived at dispatch time).

| Activity | Workflow(s) | Side-effect class | Idempotency key derivation (SHA-256 per ctrl-plane §17) |
|---|---|---|---|
| `ExtractEnquiryFacts` | EnquiryTriage | read | `sha256(tenant_id ‖ case_run_id ‖ "extract_facts")` |
| `ClassifyEnquiryType` | EnquiryTriage | read | `sha256(tenant_id ‖ case_run_id ‖ "classify")` |
| `LoadSkillVersion` | All | read | `sha256(tenant_id ‖ case_run_id ‖ workflow_slug ‖ "load_skill_version")` |
| `InvokeHermesReasoning` | All | read | `sha256(tenant_id ‖ case_run_id ‖ decision_point_id ‖ "reason")` |
| `CreateEmailDraft` | EnquiryTriage, QuoteDrafting | draft | `sha256(tenant_id ‖ case_run_id ‖ decision_point_id ‖ "create_draft_email")` |
| `CreateDriveArtifact` | QuoteDrafting | draft | `sha256(tenant_id ‖ case_run_id ‖ decision_point_id ‖ "create_document")` |
| `CreateInvoiceDraft` | InvoiceHandling | draft | `sha256(tenant_id ‖ case_run_id ‖ decision_point_id ‖ "create_invoice_draft")` |
| `SendApprovalPacket` | All | write_pending_approval | `sha256(tenant_id ‖ case_run_id ‖ decision_point_id ‖ "packet")` per §16 row 2 |
| `SendEmailFinal` | EnquiryTriage, QuoteDrafting | write_final | `sha256(tenant_id ‖ case_run_id ‖ decision_point_id ‖ "send_email_final")` |
| `FinalizeInvoice` | InvoiceHandling | write_final | `sha256(tenant_id ‖ case_run_id ‖ decision_point_id ‖ "finalize_invoice")` |
| `EmitAuditEvent` | All | audit_write | `sha256(tenant_id ‖ event_type ‖ ref_id ‖ sequence_or_timestamp)` per §16 audit event row |
| `PersistCorrection` | All | audit_write | `sha256(tenant_id ‖ decision_point_id ‖ packet_id ‖ action_button)` per §16 correction row |
| `WriteApprovalAuditEvent` | All | audit_write | `sha256(tenant_id ‖ "approval_received" ‖ decision_point_id ‖ signal_id)` per §16 audit event row |

**RESOLVED-C2 — naming:** `LoadSkillVersion` replaces the Round 1 `FetchValidatedRules` name. The activity returns the `SkillVersion` manifest defined in `03-correction-loop.md` §9.2 (id, tenant_id, workflow_slug, version, generated_at, `rule_manifest[]`). The execution plane caches this for the duration of the workflow. `case_runs.skill_version_id` (per `03-correction-loop.md` §2.3) is the time-travel anchor.

**DECISION:** `write_final` activities (`SendEmailFinal`, `FinalizeInvoice`) are only registered in the activity worker when `mode == "live"`. When `mode == "sandbox"`, calling them raises `SandboxViolationError` regardless of signal state. This enforces INV-T3 at the registration layer, not only at runtime logic.

**RESOLVED-C6 — `WriteApprovalAuditEvent`:** When a `CaseApproved` signal arrives, the workflow's signal handler invokes `WriteApprovalAuditEvent` BEFORE invoking any `write_final` activity. The audit event written has `event_type = "approval_received"`, `case_run_id`, `decision_point_id`, and `actor_id` from the signal. The MCP server reads this row synchronously before executing any `write_final` tool (see §5.6). This makes the AuditEvent the system-of-record gate for external side effects, not just a Temporal-history trace.

ASSUMPTION-CONFIRMED (Operator UX Architect): The `SendApprovalPacket` activity hands off to the messaging gateway via the `ReviewPacket` contract (`04-operator-experience.md` §2.1). The execution plane sets `expires_at` in the packet (per A6 in operator-ux); the gateway delivers it; Temporal owns the timeout (see §7).

### 3.5 Retry and timeout policies

Three policy classes cover all activities:

**Class A — Quick read/reasoning (default):**
- Initial interval: 1s
- Backoff coefficient: 2
- Max attempts: 5
- Max interval: 30s
- Non-retryable errors: `InvalidInputError`, `TenantNotFoundError`, `SandboxViolationError`

**Class B — External tool calls (MCP activities):**
- Initial interval: 2s
- Backoff coefficient: 2
- Max attempts: 3
- Max interval: 60s
- Non-retryable errors: `CapabilityDeniedError`, `SandboxViolationError`

**Class C — Operator-awaiting signals (approval gate):**
- No retry policy (signal arrival is async)
- Wait timeout: 72 hours (configurable per workflow type via provisioning manifest)
- On timeout: workflow transitions to `AwaitingCorrectionTimeout` state, emits audit event, notifies operator via a reminder packet

**Per-workflow timeouts:**

| Workflow | Total execution timeout | Signal wait timeout |
|---|---|---|
| EnquiryTriageWorkflow | 24 hours | 12 hours |
| QuoteDraftingWorkflow | 72 hours | 48 hours |
| InvoiceHandlingWorkflow | 72 hours | 48 hours |

These are conservative Phase 1 values. The operator may be slow to respond; workflows must survive multi-day gaps.

---

## 4. CaseRun ↔ Workflow Execution Mapping

### 4.1 Invariants

- **INV-CR1:** `WorkflowID == CaseRun.id` for every execution. This is a unique constraint; no two workflow executions share a WorkflowID in the same namespace.
- **INV-CR2:** A replay of CaseRun `cr_X` creates a new CaseRun `cr_Y` with `replay_of = "cr_X"` and a new WorkflowExecution with `WorkflowID = cr_Y.id`. The original `cr_X` execution is never reset or mutated.
- **INV-CR3:** The `mode` enum on a CaseRun matches the tenant's `mode` provisioning setting and is immutable for the lifecycle of that CaseRun.

### 4.2 Mapping scheme

**DECISION:** 1:1 mapping — one CaseRun produces exactly one Temporal WorkflowExecution.

`WorkflowID = case_run_id` (e.g. `cr_456`). The RunID is assigned by Temporal.

Rationale: This keeps the audit trail clean. The CaseRun record in Postgres is the single source of truth for case state; the Temporal execution history is the durable execution log. They are correlated by WorkflowID = CaseRun.id with no ambiguity.

### 4.3 Replay semantics

When an operator corrects a completed case, or when an internal reviewer triggers a regression replay:

1. A new CaseRun record is created in Postgres: `{ id: "cr_Y", replay_of: "cr_X", trigger: <same trigger as cr_X>, skill_version_id: <pinned at start>, replay_mode: true }`.
2. A new WorkflowExecution is started with `WorkflowID = "cr_Y"`.
3. The workflow runs against the same trigger payload. If `skill_version_id` is explicitly specified (e.g. for "what would v3 of the rules have done?"), it pins that version. Otherwise, the current active SkillVersion is loaded.
4. The replay result is stored alongside `cr_X` and a `replay_diff` artifact (per `03-correction-loop.md` §10.4) is generated.

**DECISION:** Temporal's `ResetWorkflow` API is not used for replays. It mutates workflow history in place, violating INV-CR2 and breaking the audit trail.

**RESOLVED-C5 — Replay determinism, four commitments:**

1. **`skill_version_id` pinned at workflow-start** and immutable for the run. `LoadSkillVersion` is called exactly once per WorkflowExecution; the manifest is cached in workflow state for the lifetime of the run. Retries re-use it via Temporal's activity history. A skill promoted mid-run does NOT take effect for the in-flight run.
2. **LLM `temperature = 0` on replay invocations.** When `replay_mode = true` in the workflow input, `InvokeHermesReasoning` overrides `HERMES_DEFAULT_TEMPERATURE` to 0. Live runs use the default (0.2) per the manifest.
3. **MCP read-tool outputs are replayed from Temporal activity history, not re-executed against live data.** Temporal's durable activity history already records activity outputs (read MCP calls produce structured JSON results stored in event history). On replay, the workflow re-executes; activities that hit the same idempotency key in the MCP idempotency log return the cached result. For activities that depend on time-of-call (e.g. listing inbox threads), the original case stored a snapshot in the artifact store under `cases/{case_run_id}/snapshots/`; replay reads from this snapshot, not from live MCP state. This avoids re-implementing a record/replay layer.
4. **Regression contract is at decision-point outcome level, not artifact bytes.** Per `03-correction-loop.md` §10.2 — accepted. The `replay_diff` artifact compares `agent_output.action_taken`, `agent_output.branch_taken`, and `agent_output.extracted_facts`. LLM-generated draft text bytes are NOT compared.

RESOLVED-C5 (sub-issue): The MCP "snapshot at original run" pattern is aligned with `03-correction-loop.md` artifact storage. Learning-loop §2.5 added `artifact_type = 'mcp_read_snapshot'` (with `mcp_tool_name` and `mcp_idempotency_key` columns) in R4; the precondition "original snapshots exist" is now schema-enforced (see also §13.3 / OPEN-OQ-MCP-Snapshots CLOSED note).

### 4.4 Workflow input shape (base)

```json
{
  "case_run_id": "cr_456",
  "tenant_id": "t_123",
  "mode": "sandbox",
  "replay_of": null,
  "replay_mode": false,
  "skill_version_id": "sv_r7q1",
  "workflow_slug": "quote_drafting",
  "trigger": { ... }
}
```

**RESOLVED-C2 — `skill_version_id` replaces `rule_snapshot_id` everywhere.** This matches `03-correction-loop.md` §2.3 (`case_runs.skill_version_id`) and §9 (SkillVersion as the authoritative manifest). The execution plane does not invent a parallel identifier.

**RESOLVED-N1 — `mode` replaces `sandbox_mode` everywhere.** The R2 boolean `sandbox_mode: true` is superseded by `mode: "sandbox" | "live"` (RESOLVED-N1, §0.2). Default-deny: a missing or unrecognized `mode` value at workflow input rejects start; at the MCP server defaults to `"sandbox"`. The default is "deny external side effects." Reasserted as INV-T5 in §3.1.

---

## 5. MCP Server Specifications

### 5.1 Shared principles

**DECISION:** Sandbox and live tool variants are **separate MCP server processes**. The sandbox servers do not wrap live servers behind a flag. A shared-server with a `mode` flag is one bug away from a real external action. The capability allowlist is enforced at the MCP server binary level, not at the caller. Each server still exposes its `MODE` env var for the §5.6 preflight, but the binary itself is locked to one mode.

All three MCP servers share:
- Tenant credential injection at process start from the tenant secret scope.
- An idempotency store (Postgres table `mcp_idempotency_log`) keyed on `(tool_name, idempotency_key)`. A repeated call with a known key returns the cached response without re-executing.
- A `tenant_id` field on every request, verified against the server's own configured `TENANT_ID` env var (INV-H1 enforcement at the MCP layer).
- OTEL span emission for every tool call.

Idempotency key convention for all tool calls: `{case_run_id}:{decision_point_id}:{tool_name}`. The key is stable across Temporal retries so that a retried activity hitting the same MCP call returns the cached result without re-executing. A new idempotency key is generated only when a decision point is re-entered after a `CaseRejected` correction (a new `decision_point_id` is assigned for the re-run attempt).

### 5.2 Sandbox-email MCP

**Purpose:** Simulates a mailbox environment. Drafts and threads are stored in Postgres; no real SMTP or IMAP connection is made in sandbox mode.

**Invariant INV-E1:** No tool in sandbox-email MCP may emit an SMTP connection attempt. The email transport layer is replaced by a Postgres insert.

#### Tool surface

| Tool name | Parameters | Side-effect class | Notes |
|---|---|---|---|
| `list_inbox_threads` | `{ tenant_id, filter?: ThreadFilter }` | read | Returns seeded or real threads depending on mode |
| `get_thread` | `{ tenant_id, thread_id }` | read | Returns full thread with messages |
| `extract_thread_facts` | `{ tenant_id, thread_id, schema: FactSchema }` | read | LLM-assisted extraction; returns structured facts |
| `create_draft_email` | `{ tenant_id, thread_id?, to, subject, body, attachments?: ArtifactRef[], idempotency_key }` | draft | Writes to `email_drafts` table; returns `draft_id` |
| `update_draft_email` | `{ tenant_id, draft_id, fields: DraftPatch, idempotency_key }` | draft | Patches existing draft |
| `get_draft_email` | `{ tenant_id, draft_id }` | read | Returns draft content |
| `send_draft_email` | `{ tenant_id, draft_id, idempotency_key }` | write_final | **Blocked in sandbox mode.** Raises `SandboxViolationError`. |

`send_draft_email` is registered in the MCP server binary only when `MODE=live`. In `MODE=sandbox` (or unset/unrecognized → defaults to sandbox) it is absent from the tool manifest entirely — it does not appear in `list_tools()` output, so Hermes cannot select it.

#### Credential injection

```json
{
  "TENANT_ID": "t_123",
  "MODE": "sandbox",
  "DB_URL": "<tenant-scoped postgres URL>",
  "OTEL_ENDPOINT": "<internal collector>",
  "SMTP_CREDENTIALS": null
}
```

### 5.3 Sandbox-drive MCP

**Purpose:** Simulates a shared document store. Files are stored in the tenant's object-store prefix; no Google Drive or real cloud storage API is called in sandbox mode.

**Invariant INV-D1:** All file paths are scoped under `/{tenant_id}/`. A path traversal outside this prefix raises `CapabilityDeniedError`.

#### Tool surface

| Tool name | Parameters | Side-effect class | Notes |
|---|---|---|---|
| `list_files` | `{ tenant_id, folder?: string, filter?: FileFilter }` | read | Lists files in tenant prefix |
| `get_file_metadata` | `{ tenant_id, file_id }` | read | Name, type, size, created_at |
| `read_file_content` | `{ tenant_id, file_id }` | read | Returns file bytes or text |
| `create_document` | `{ tenant_id, name, content, folder?: string, idempotency_key }` | draft | Creates file; returns `file_id` and preview URL |
| `update_document` | `{ tenant_id, file_id, content_patch, idempotency_key }` | draft | Updates existing document |
| `get_preview_url` | `{ tenant_id, file_id, expires_in_seconds }` | read | Returns short-lived signed URL for operator preview |
| `publish_document` | `{ tenant_id, file_id, share_target, idempotency_key }` | write_final | **Blocked in sandbox mode.** Raises `SandboxViolationError`. |

`publish_document` is absent from the tool manifest in sandbox mode (same pattern as `send_draft_email`).

#### Credential injection

```json
{
  "TENANT_ID": "t_123",
  "MODE": "sandbox",
  "OBJECT_STORE_BUCKET": "victoria-tenant-t_123",
  "OBJECT_STORE_PREFIX": "/t_123/",
  "DB_URL": "<tenant-scoped postgres URL>",
  "OTEL_ENDPOINT": "<internal collector>"
}
```

### 5.4 Sandbox-invoice MCP

**Purpose:** Simulates invoice parsing, coding suggestion, and accounting draft creation. No connection to Xero, MYOB, or any live accounting system in sandbox mode.

**Invariant INV-I1:** `finalize_invoice` and `submit_to_accounting` are absent from the tool manifest in sandbox mode.

#### Tool surface

| Tool name | Parameters | Side-effect class | Notes |
|---|---|---|---|
| `parse_invoice_document` | `{ tenant_id, file_id, idempotency_key }` | read | OCR + extraction; returns `InvoiceObject` |
| `get_invoice` | `{ tenant_id, invoice_id }` | read | Returns stored invoice object |
| `suggest_invoice_coding` | `{ tenant_id, invoice_id, rules_context: ValidatedRuleSet, idempotency_key }` | read | Returns coding suggestion |
| `create_invoice_draft` | `{ tenant_id, invoice_id, coding: InvoiceCoding, idempotency_key }` | draft | Creates draft record; returns `draft_id` |
| `update_invoice_draft` | `{ tenant_id, draft_id, patch: InvoicePatch, idempotency_key }` | draft | Patches draft |
| `get_invoice_draft_preview` | `{ tenant_id, draft_id }` | read | Returns human-readable preview |
| `finalize_invoice` | `{ tenant_id, draft_id, idempotency_key }` | write_final | **Blocked in sandbox mode.** |
| `submit_to_accounting` | `{ tenant_id, draft_id, system: AccountingSystem, idempotency_key }` | write_final | **Blocked in sandbox mode.** |

#### Credential injection

```json
{
  "TENANT_ID": "t_123",
  "MODE": "sandbox",
  "DB_URL": "<tenant-scoped postgres URL>",
  "OTEL_ENDPOINT": "<internal collector>",
  "ACCOUNTING_API_KEY": null
}
```

### 5.5 Capability allowlist enforcement

Each MCP server exposes only the tools listed above. The server does not accept dynamic tool registration. The `list_tools()` response is static (modulo sandbox mode exclusions) and computed at server startup. Any tool call for an unregistered name returns `CapabilityDeniedError`.

### 5.6 MCP write_final preflight (RESOLVED-C6)

**Position:** ACCEPT Devil's Advocate INV-03. The approval check for any `write_final` tool happens **inside the MCP server**, not in Hermes and not in Temporal workflow code.

**Enforcement point:** MCP server, in the request handler for any tool tagged `WRITE_EXTERNAL` (`send_draft_email`, `publish_document`, `finalize_invoice`, `submit_to_accounting`). Two gates run in order before tool execution:

```
Gate 1 — sandbox check (RESOLVED-N1):
  effective_mode = server.MODE if server.MODE in {"sandbox","live"} else "sandbox"
  if (effective_mode == "sandbox" || request.mode == "sandbox" || request.mode is missing):
    write AuditEvent{event_type: "sandbox_escape_blocked"}
    return error{code: "SANDBOX_MODE", message: "..."}

Gate 2 — approval check (live mode only) [RESOLVED-N7 in R4]:
  # Reads via the mcp_approval_events VIEW (learning-loop §11.5),
  # not the raw audit_events table. The view is RLS-scoped to
  # event_type='approval_received' AND tenant_id=current_tenant.
  row = db.query(mcp_approval_events,
    "WHERE case_run_id = ? AND decision_point_id = ? LIMIT 1",
    request.case_run_id, request.decision_point_id)
  if row is null:
    write AuditEvent{event_type: "blocked_write_attempted"}
    return error{code: "APPROVAL_REQUIRED", message: "..."}

Gate 3 — header binding check (every call, both modes):
  if request.header["X-Victoria-Tenant-Id"] != server.TENANT_ID:
    write AuditEvent{event_type: "security_violation"}
    return error{code: "TENANT_MISMATCH"}, HTTP 403

# ... only then execute the tool ...
```

**Read path (RESOLVED-R4-CONFLICT-2 / R5 single-store):** the MCP server reads via the `mcp_approval_events` VIEW defined over the **single authoritative `audit_events` store in the control-plane DB** (ctrl-plane §9.3, learning-loop §2.0.1, §2.10). The view is filtered to `event_type='approval_received'` and RLS-scoped to `tenant_id = current_setting('app.current_tenant')`. The MCP server's connection to the control-plane DB sets `SET LOCAL app.current_tenant = '<TENANT_ID>'` on every transaction. The connection itself is mTLS-authenticated using the per-tenant exec-plane leaf certificate (SAN `t_<tenant_id>.exec.victoria.internal`, ctrl-plane §6.2). The role has SELECT only on this view — no other table, no other event type, no other tenant. **RESOLVED-N7 (R3 → R4).**

**Latency budget (acknowledged, OPEN-AUDIT-LATENCY-R4 in ctrl-plane).** The Gate 2 read crosses the VPC to the control-plane DB; ~5–10ms intra-VPC is the expected envelope. Acceptable on the `write_final` hot path because (a) every operator-approved external action already costs an LLM call and a Temporal round-trip; (b) the alternative — local-cache reads — was rejected at R5 in favor of single-source-of-truth correctness across all four specs.

**Storage topology (R5 single-store, REVISED from R4 framing):** the R4 description of a per-tenant local `audit_events` "write-through cache + read source" is **superseded**. The team aligned on:

- Control-plane `audit_events` is the single authoritative store and the only read source for MCP preflight.
- Per-tenant `audit_events_outbox` (renamed from R3 "local audit_events", per learning-loop §2.0.1 and ctrl-plane §9.3) is a **transactional durable write-through buffer** for control-plane outage tolerance only. It is **never a read source**.
- The MCP `WRITE_EXTERNAL` preflight does NOT read the outbox; it always queries the control-plane store.

**Who writes the `approval_received` AuditEvent:** The Temporal workflow's `CaseApproved` signal handler invokes the `WriteApprovalAuditEvent` activity (§3.4) BEFORE invoking any `write_final` activity. The activity executes a single transaction that:
1. Inserts the row into per-tenant `audit_events_outbox` (transactional with the business state change — e.g., the same Temporal-workflow-side write-set).
2. POSTs the event upstream to control-plane `/internal/audit/events` over mTLS.
3. On 200 from the upstream POST, marks `outbox.drained = true` (the only mutation allowed by the outbox trigger; ctrl-plane §9.3 INV-AUDIT-01).
4. On 5xx or transport failure, the row stays `drained = false` and a background drain worker retries with exponential backoff. Idempotency is guaranteed by the SHA-256 `idempotency_key` (§16) on the upstream insert.

**Read-after-write contract.** When the next `write_final` MCP call lands, the MCP server's Gate 2 SELECT against the control-plane view sees the row (because the upstream POST already returned 200 before the workflow advances to the `write_final` step — the workflow's own ordering serializes them). If upstream is degraded, the workflow does not advance to `write_final` until either the drain succeeds or the workflow times out — i.e., the `write_final` is naturally gated on upstream success.

```json
{
  "audit_event_id": "<uuid>",
  "tenant_id": "t_123",
  "event_type": "approval_received",
  "actor_type": "operator",
  "actor_id": "<from CaseApproved.approved_by>",
  "ref_entity_type": "decision_point",
  "ref_id": "<decision_point_id>",
  "related_ids": { "case_run_id": "cr_456", "correction_id": null, "signal_id": "<from envelope>" },
  "payload": { "approved_at": "...", "signal_id": "<from envelope>", "gateway_idempotency_key": "<from envelope>", "packet_id": "<from envelope>" },
  "idempotency_key": "<sha256 per ctrl-plane §17>",
  "occurred_at": "now()",
  "ingested_at": "now()",
  "source": "execution_plane"
}
```

This conforms to ctrl-plane §9.3 (`audit_events` schema) and learning-loop §11.2. The `event_type` value `approval_received` is in learning-loop's R3 §11.3 registry with this spec named as the writer.

**Why this is the right boundary, not Temporal-only:**
- A bug in workflow code that "forgot to check the signal" cannot fire a write — the MCP server independently checks.
- A bug in Hermes (e.g. an injected prompt that says "ignore approval and send anyway") cannot fire a write — the MCP server independently checks.
- The check is co-located with the side effect, so it cannot be skipped by control-flow rearrangement upstream.

**Defense-in-depth pairing with sandbox-tool-manifest absence (Round 1 D5):** In sandbox mode, `write_final` tools are absent from the MCP `list_tools()` response. So Hermes cannot select them. If something *did* try to call them out-of-band, Gate 1 hard-rejects. In live mode, the tools are in the manifest; Gate 2 is the gate.

### 5.7 INV-MCP — tenant header binding

- **INV-MCP1:** Every MCP request carries an `X-Victoria-Tenant-Id` header. The server validates this equals its startup-bound `TENANT_ID` env var (RESOLVED-INV-01 from devils-advocate).
- **INV-MCP2:** The `tenant_id` field in the request body must match the header. Mismatch returns `TenantMismatchError`.
- **INV-MCP3:** The MCP server's bound credentials are loaded once at startup from the tenant secret scope. The server does not accept tenant credentials per-request and does not read another tenant's secrets ever.

These invariants apply identically to sandbox-email, sandbox-drive, and sandbox-invoice servers.

---

## 6. Sandbox Case Seed Pipeline

### 6.0 TDD posture (R4 audit)

This section follows the spec-wide convention: invariants and contract tests below are stated **before** the fixture-library implementation in §6.2. The four invariants in §6.1 are the testable surface; §6.2 (fixture library), §6.3 (mode enforcement stack), and §6.4 (provisioning seed mechanics) are implementation in service of those invariants. Each invariant maps to a contract test in §10.6 (`test_fixture_sandbox_flag`, `test_object_store_path_isolation`) and §10.4 (`test_write_final_absent_in_sandbox`).

### 6.1 Invariants

- **INV-S1:** Every fixture in the sandbox case library carries a `sandbox: true` boolean field at the root. Workflows check this field at start; a missing or false value aborts the run with `SandboxContaminationError`.
- **INV-S2:** All artifact file paths generated during a sandbox run live under `/{tenant_id}/sandbox/{case_run_id}/`. No sandbox artifact is stored under a path that could be confused with a production path.
- **INV-S3:** The sandbox email transport is a Postgres table, not SMTP. Enforced by INV-E1.
- **INV-S4:** No `write_final` tool is reachable in sandbox mode (enforced by tool manifest exclusion at MCP startup).

### 6.2 Fixture library

Sandbox fixtures are stored in the control plane's fixture repository (not in the execution plane). For Phase 1, fixtures are managed as a version-controlled YAML/JSON library organized by workflow type and vertical.

```
fixtures/
  enquiry_triage/
    roofing/
      new_customer_residential.json
      commercial_referral.json
    plumbing/
      emergency_callout.json
  quote_drafting/
    roofing/
      incomplete_photos.json
      repeat_client.json
  invoice_handling/
    generic/
      au_supplier_gst.json
      sg_supplier_no_gst.json
```

Each fixture file contains the workflow trigger payload plus metadata:

```json
{
  "fixture_id": "fix_roofing_quote_001",
  "sandbox": true,
  "workflow_type": "quote_drafting",
  "vertical": "roofing",
  "display_name": "New client — incomplete photos",
  "description": "Triggers the photos-incomplete branch for rule testing",
  "trigger": {
    "source": "email",
    "from": "alice@fakeclient.sandbox",
    "subject": "Roof repair quote request",
    "body": "Hi, I need a quote for some roof repairs on my property.",
    "attachments": [
      { "file_id": "fix_file_001", "name": "roof_photo_1.jpg", "sandbox": true }
    ],
    "received_at": "2026-04-27T08:00:00Z",
    "client_type": "new"
  }
}
```

Fixture files are seeded from the control plane into the execution plane's Postgres database at tenant provisioning time (during bootstrap step in §2.7).

ASSUMPTION (Control Plane Architect): The control plane pushes the appropriate fixture set to the execution plane's database during provisioning. The execution plane does not pull fixtures at runtime; they are pre-seeded.

### 6.3 Sandbox mode enforcement stack

Sandbox isolation is enforced at four layers, each independent:

1. **Workflow input:** `mode: "sandbox"` in the workflow input struct (canonical enum, RESOLVED-N1).
2. **MCP tool manifest:** `write_final` tools absent from `list_tools()` response when the MCP server boots with `MODE=sandbox` (or unset → defaults to sandbox).
3. **Activity registration:** `write_final` activities not registered with the Temporal worker when the worker boots with `HERMES_MODE=sandbox`.
4. **Provisioning manifest:** `mode` is an immutable provisioning-time property; changing it requires reprovisioning.

A bug in any single layer does not bypass the other three.

---

## 7. Approval-Gating Mechanism

### 7.1 Invariants

- **INV-A1:** Every `write_final` activity execution must be preceded by a `CaseApproved` signal in the current workflow execution history with a matching `decision_point_id` AND an `approval_received` AuditEvent row. Enforced by workflow code (signal → audit-write → write_final ordering) AND by the MCP server preflight (§5.6). Independent gates.
- **INV-A2:** A `CaseApproved` signal delivered more than once with the same `signal_id` is deduplicated by the workflow signal handler. No double-execution occurs. The `signal_id` is generated by Operator UX (`04-operator-experience.md` §2.3). The workflow stores processed `signal_id` values in workflow state and rejects duplicates.
- **INV-A3:** A `CaseRejected` signal triggers a correction persistence path and terminates the current decision branch without executing the `write_final` activity.
- **INV-A4 (RESOLVED-C3 — transport):** The messaging gateway emits the `ApprovalSignalEnvelope` directly to the Temporal cluster via the Temporal SDK using the gateway's tenant-bound service identity. The control plane is NOT on the signal path. The gateway's WhatsApp-bound auth (phone → tenant binding) is the auth boundary; routing through control-plane API would add a hop with no security gain.

### 7.2 Sequence

```
Hermes reasoning completes → proposes Action A at DecisionPoint dp_X
          │
          ▼
Activity: CreateDraft (draft class; writes artifact to sandbox store)
          │
          ▼
Activity: SendApprovalPacket
  Payload to messaging gateway: ReviewPacket per `04-operator-experience.md` §2.1
  (packet_id, tenant_id, case_run_id, decision_point_id, workflow_type,
   trigger, facts, planned_action, artifact_preview, button_set, expires_at,
   idempotency_key, metadata.run_mode)
          │
          ▼
Workflow: Await on Signal selector (CaseApproved | CaseRejected | CaseAborted)
  — Authoritative timer is owned by Temporal; fires at expires_at + grace
  — On timer fire: emit case_timeout AuditEvent; workflow may send one reminder
    via SendApprovalPacket and resume waiting; final timeout transitions to
    CaseAbandoned terminal state.
          │
     ┌────┴────┐
     │         │
 Approved    Rejected
     │         │
     ▼         ▼
WriteApprovalAuditEvent  PersistCorrection (writes to audit_events with
     │                    event_type='correction_received')
     ▼                    → Learning Architect's pipeline picks this up
write_final activity      → returns to workflow; branch terminates
  → MCP server runs §5.6
    preflight (Gates 1, 2, 3) before executing the tool
```

**RESOLVED-C3 — transport:** The messaging gateway emits signals directly to Temporal using the Temporal Go SDK. Addressing key:

```
namespace:    victoria          (single Phase 1 namespace)
task_queue:   (resolved by WorkflowID; gateway does not target queue)
WorkflowID:   {case_run_id}     (matches INV-CR1)
signal_name:  CaseApproved | CaseRejected | CaseAborted
```

The gateway authenticates to the Temporal cluster with a service identity provisioned per-tenant (HMAC token in the gateway's tenant-bound secret scope). The Temporal cluster validates the identity matches the workflow's bound `tenant_id` namespace tag. The signal envelope (per operator-ux §2.3) carries `tenant_id`, `case_run_id`, `decision_point_id`, `signal_id`. The workflow signal handler verifies `envelope.tenant_id == workflow.input.tenant_id` (defense in depth).

**RESOLVED-C3 — timeout ownership:** Temporal owns the authoritative timeout. The workflow sets a Temporal timer at `expires_at` (carried in the ReviewPacket). When the timer fires, the workflow handles the timeout. The messaging gateway does NOT need to sweep packets and does NOT emit `correction_expired` signals. Stale-reply detection (a reply arriving after `expires_at`) is gateway-side: the gateway writes an audit event but does not deliver a Temporal signal (per operator-ux I-04). This is cleaner than the "either side sweeps" framing in operator-ux OQ-1.

OPEN-OQ (operator-ux OQ-1): Operator UX may still want a `packet_expired` *callback* from Temporal to clean up gateway-side follow-up state (operator-ux §7.2). Resolution: workflow timeout handler can fire one `Activity.SendPacketExpiredNotification` to the gateway as a courtesy, but this is best-effort and the gateway's Postgres TTL on `followup_session` is the authoritative cleanup.

### 7.3 Signal payload schemas

The execution plane accepts the `ApprovalSignalEnvelope` from `04-operator-experience.md` §2.3 verbatim. The signal name is determined by the `action` field:

| operator-ux `action` | Temporal signal | Workflow handler |
|---|---|---|
| `approve` | `CaseApproved` | Run `WriteApprovalAuditEvent`, then `write_final` chain |
| `wrong_facts` / `wrong_action` / `missing_condition` / `use_different_template` / `add_note` | `CaseRejected` | Run `PersistCorrection` (writes `correction_received` AuditEvent + correction row), terminate decision branch |

```json
// CaseApproved (subset of ApprovalSignalEnvelope)
{
  "signal_id": "<UUID v4 from gateway>",
  "tenant_id": "t_123",
  "case_run_id": "cr_456",
  "decision_point_id": "dp_abc",
  "action": "approve",
  "approved_by": "operator:wa:+61412345678",
  "delivered_at": "2026-04-27T10:35:00Z",
  "gateway_idempotency_key": "corr_abc1:signal:1"
}

// CaseRejected
{
  "signal_id": "<UUID v4>",
  "tenant_id": "t_123",
  "case_run_id": "cr_456",
  "decision_point_id": "dp_abc",
  "action": "wrong_action",
  "rejected_by": "operator:wa:+61412345678",
  "scope_hint": "always",
  "condition_hints": [...],
  "free_text": "...",
  "follow_up_answers": [...],
  "delivered_at": "2026-04-27T10:36:00Z",
  "gateway_idempotency_key": "corr_def2:signal:1"
}
```

The `condition_hints`, `scope_hint`, `free_text`, and `follow_up_answers` are passed through unchanged into the `correction_received` AuditEvent payload; the execution plane does NOT interpret them. The Learning Architect's parsing pipeline (per `03-correction-loop.md` §4) consumes them.

OUT OF SCOPE: RuleCandidate generation from the correction event. The execution plane writes the correction row + AuditEvent. Learning Architect's pipeline picks them up via DB read.

ASSUMPTION-CONFIRMED (Learning Architect): The `corrections` table schema (per `03-correction-loop.md` §2.6) is the contract. The execution plane's `PersistCorrection` activity populates this row, including `idempotency_key`, `tenant_id`, `case_run_id`, `decision_point_id`, `action_button`, `free_text`, `follow_up_answer`, `scope_hint`. Learning Architect's parsing pipeline polls or subscribes for `parse_status='pending'` rows.

### 7.4 ApprovalSignalEnvelope payload mapping for `PersistCorrection` (RESOLVED-C13, R4 reconciled)

**Operator-ux delivered the canonical 16-field envelope** in §2.2 (`04-operator-experience.md`). R5 update (C13-R5): R4's `correction_id` and `parse_status` reverted from the wire; envelope locked at 16 fields. This section reconciles field naming with operator-ux's canonical shape and re-states the column mapping. **No additional fields are required from operator-ux.**

**RESOLVED-C13-FOLLOWUP (R4):** Operator-ux's canonical field names are `parser_method` and `parser_confidence` (note the trailing `r`); learning-loop's column names are `corrections.parse_method` and `corrections.parse_confidence` (no `r`). This is a deliberate boundary: the gateway's *parser stage* emits `parser_*`; the row column documents the *parsed result* and is named `parse_*`. The `PersistCorrection` mapping renames `envelope.parser_method → corrections.parse_method` and `envelope.parser_confidence → corrections.parse_confidence`. No silent fork.

**Canonical 16-field envelope (consumed verbatim from operator-ux §2.2, C13-R5):**

| # | Envelope field (operator-ux §2.2) | Type | `PersistCorrection` writes to | Notes |
|---|---|---|---|---|
| 1 | `schema_version` | string `"1"` | (validated; not stored) | Wire schema version; mismatch → `MalformedSignalError` |
| 2 | `signal_id` | UUID v4 | Temporal signal-handler dedup (INV-A2); not a `corrections` column | Workflow-side dedup table; second delivery is no-op |
| 3 | `idempotency_key` | sha256 derivation per ctrl-plane §17 | `corrections.idempotency_key` (UNIQUE constraint) | Per ctrl-plane registry, components `(tenant_id, decision_point_id, packet_id, action_button)` |
| 4 | `packet_id` | string | `audit_events.payload.packet_id` (not a `corrections` column) | Cross-reference to gateway packet record |
| 5 | `case_run_id` | string `cr_*` | `corrections.case_run_id` | Echoed from originating ReviewPacket |
| 6 | `decision_point_id` | string `dp_*` | `corrections.decision_point_id` | Echoed from originating ReviewPacket |
| 7 | `tenant_id` | string `t_*` | `corrections.tenant_id` | Gateway-authenticated from `provider_number → tenant_id`; never from body |
| 8 | `operator_id` | string `op_whatsapp:+E.164` \| `op_telegram:<chat_id>` | `corrections.operator_id`, `audit_events.actor_id` | Opaque messaging-layer identifier |
| 9 | `channel` | `"whatsapp" \| "telegram"` | `audit_events.payload.channel` | Forensic only |
| 10 | `source_message_id` | provider message ID | `audit_events.payload.source_message_id` | Provider-native dedup ref |
| 11 | `raw_inbound_message_id` | provider message ID | `corrections.raw_inbound_message_id` (per learning-loop §3.3) | Often equal to `source_message_id` |
| 12 | `ts` | ISO 8601 UTC | `corrections.created_at` | Time gateway received reply |
| 13 | `action_button` | enum (6 values) | `corrections.action_button` | Canonical button enum |
| 14 | `free_text` | string \| null | `corrections.free_text` | Operator verbatim |
| 15 | `follow_up_answer` | string \| null | `corrections.follow_up_answer` | Multi-turn concatenated |
| 16 | `scope_hint` | `"case" \| "tenant" \| null` | `corrections.scope_hint` | Operator-ux already uses learning-loop's mapping (`always` → `tenant`, `this_case` → `case`); no remap in `PersistCorrection` |
| 17 | `parser_method` | `"button" \| "button_fallback" \| "text_match"` | `corrections.parse_method` (RENAMED at the boundary) | Stage A output; weights Stage B confidence |
| 18 | `parser_confidence` | float `[0.0, 1.0]` | `corrections.parse_confidence` (RENAMED at the boundary) | Stage B gates on `parse_low_confidence_threshold` |

(The envelope has 16 top-level fields per operator-ux §2.2; the table has 18 rows because two of them — `parser_method` / `parser_confidence` — are listed alongside the rename target. The `parse_status` mapping is implicit: only `parse_status: resolved` envelopes reach the Temporal signal channel; `dead_lettered` and `needs_followup` are gateway-side terminal states per operator-ux I-07.)

**Operator identity policy.** The `corrections` row stores `operator_id` per learning-loop's R3 corrections schema (operator-ux §2.2 confirms it as a corrections column). The same value is mirrored to `audit_events.actor_id` for cross-row correlation.

**INV-A5 (RESOLVED-C13).** `PersistCorrection` MUST construct the `corrections` row from the envelope alone. It is forbidden to call back to the **messaging gateway** to enrich the row. The activity DOES write a `correction_received` row to the per-tenant `audit_events_outbox` (learning-loop §1664) and the upstream drain to the control-plane authoritative store runs in parallel — those local writes are the expected audit path, not a remote-enrichment callback. If a required envelope field is missing or malformed, `PersistCorrection` raises `MalformedSignalError` (non-retryable) and the workflow transitions to `CorrectionParseFailed` rather than persisting a partial row.

**Contract test `test_persist_correction_from_signal_alone` (§10.4):** deliver a `CaseRejected` signal carrying the canonical 16-field envelope (C13-R5); with the gateway database unreachable, assert `PersistCorrection` succeeds and the resulting `corrections` row matches the envelope field-by-field per the mapping above. Field-name renames (`parser_method` → `parse_method`, `parser_confidence` → `parse_confidence`, `ts` → `created_at`); `scope_hint` arrives pre-canonicalized as `tenant | case | null` per operator-ux §2.2 OPEN-Envelope-ScopeHint resolution (no remap in `PersistCorrection`).

---

## 8. SkillVersion Consumption at Runtime

### 8.1 Position (RESOLVED-C2)

**DECISION (joint with Learning Architect):** The runtime rule artifact is the **`SkillVersion`** as defined in `03-correction-loop.md` §9. There is one name, one schema, one source of truth.

- The Learning Architect owns: the `skill_versions` table, the `rule_manifest` JSON schema, the lifecycle (created on every promotion/rollback), and the resolver that returns the active SkillVersion for a given `(tenant_id, workflow_slug, as_of?)`.
- The Execution Plane owns: how the SkillVersion is loaded into a workflow execution (`LoadSkillVersion` activity), how it is materialized into a Hermes-readable system-prompt block, and how `case_runs.skill_version_id` is pinned for replay determinism.

The Round 1 invented term `rule_snapshot_id` is dropped. Every reference is now `skill_version_id`.

### 8.2 LoadSkillVersion activity contract

```
Activity: LoadSkillVersion
Input:    { tenant_id, workflow_slug, as_of?: timestamp }
Output:   SkillVersion (per `03-correction-loop.md` §9.2):
          {
            id: "sv_r7q1",
            tenant_id, workflow_slug, version,
            generated_at,
            rule_manifest: [
              { rule_id, scope, version, decision_type,
                conditions_canonical, recommended_action, priority }
            ]
          }
Side-effect class: read
Idempotency key:   {case_run_id}:{workflow_slug}:load_skill_version
Retry policy:      Class A
Endpoint:          HERMES_SKILL_VERSION_ENDPOINT (mTLS; SAN = t_<tenant_id>.exec.victoria.internal per §13.1)
```

`as_of` is omitted on live runs (returns the active SkillVersion). On replay runs of an original case, the workflow passes `as_of = original_case.started_at` to reproduce the exact rule set from the original. Alternatively, the workflow accepts an explicit `skill_version_id` as input and skips re-resolution; this is used for deliberate "what would v3 do?" replays.

### 8.3 Materialization: workflow context block fed to Hermes (RESOLVED-C14)

**RESOLVED-C14 — field-name alignment.** Round 2 of this spec used `if_conditions` / `then_action` for the prompt block while learning-loop's `rule_manifest` (per `03-correction-loop.md` §9.1) uses `conditions_canonical` / `recommended_action`. To avoid translation drift, this spec **adopts learning-loop's field names verbatim**. The manifest passes through into the prompt block unmodified, except for two execution-plane-only additions: a top-level `skill_version_id` echo and an `applicable_rules` filter.

```json
{
  "skill_version_id": "sv_r7q1",
  "applicable_rules": [
    {
      "rule_id": "vr_0042",
      "scope": "tenant",
      "version": 1,
      "decision_type": "send_or_hold",
      "conditions_canonical": [
        {"field": "photos_complete", "operator": "=", "value": false},
        {"field": "client_type", "operator": "=", "value": "new"}
      ],
      "recommended_action": "hold_and_request_more_info",
      "rationale": "Always hold quote for new clients when photos are incomplete.",
      "priority": 10
    }
  ]
}
```

Each entry in `applicable_rules` is a **byte-identical projection** of the corresponding entry in `rule_manifest`. No field is renamed, dropped, or transformed — except that the execution plane omits entries whose `decision_type` is not reachable in the active workflow (avoids polluting the system prompt with irrelevant rules). Field-name parity is enforced by contract test `test_skill_manifest_passthrough_field_parity` (§10.3).

The execution plane defines the **prompt template** that wraps this block (e.g., "You MUST apply rules when their `conditions_canonical` match the current decision context. Cite the `rule_id` of each applied rule in your reasoning trace."). The Learning Architect defines the **data shape**; this spec defines only the **prompt-side framing** that surrounds it.

### 8.4 Pinning and replay

- On live workflow start, `LoadSkillVersion` is called with no `as_of`. The returned `skill_version_id` is written to `case_runs.skill_version_id` and into the workflow input as the immutable pin.
- On replay start, the replay-trigger sets `skill_version_id` directly in the workflow input (no `LoadSkillVersion` resolution); the workflow uses the pinned ID for the entire run.
- A skill promoted mid-run does NOT take effect for in-flight workflows. New runs pick up the new SkillVersion automatically.

This satisfies Devil's Advocate UE-03 (replay determinism) for the rule-set dimension.

ASSUMPTION (Learning Architect): The `skill_version_endpoint` returns 404 if `as_of` predates the earliest SkillVersion for the `(tenant, workflow_slug)` pair. The execution plane treats this as a non-retryable error and fails the run. (No silent fallback to "current.")

### 8.5 Effective-rule-set resolution (RESOLVED-C12)

`00-overview.md` §2 row A1 binds: a single `validated_rules` table — all four scopes — lives in the **control-plane DB** with row-level security. R2 of this spec previously contemplated an exec-plane-side merge of two sources (per-tenant rules + control-plane shared rules). That merge is **withdrawn** in R3.

**RESOLVED-C12.** Effective-rule-set resolution happens **inside `LoadSkillVersion`** at the control-plane resolver, not inside the execution plane. The execution plane:

- Calls `LoadSkillVersion(tenant_id, workflow_slug, as_of?)` once at workflow start.
- Receives a fully resolved `SkillVersion` manifest with all applicable rules already collapsed via the scope-priority resolution from `03-correction-loop.md` §6.3.
- Does NOT issue any local `SELECT` against `validated_rules` in the per-tenant execution DB. There is no per-tenant `validated_rules` table for this spec to read.

This keeps the execution plane out of the rule-precedence business and ensures the same priority resolution is applied uniformly across all tenants. It also means the execution plane carries **no SkillVersion fan-out logic** — the scope resolution is the resolver's problem.

RESOLVED-N7: the `mcp_audit_reader` Postgres role (§5.6) reads `audit_events` only, not `validated_rules`. Confirmed with learning-loop: the role grant is scoped to `audit_events` and excludes `validated_rules` access (see conflict index, R4 resolution of former OPEN-N7).

---

## 9. Failure Modes

### 9.1 Hermes container OOM

**Scenario:** The Hermes container's memory limit is hit during LLM inference.

**Behavior:** The container crashes and restarts (restart policy: `always`, with exponential backoff). Hermes state on disk is preserved (the data volume is external). In-flight `InvokeHermesReasoning` activity attempts fail; Temporal's retry policy (Class A) retries the activity, re-invoking Hermes after restart.

**Risk:** The activity output may have been partially generated before the crash. Because the activity is retried with the same idempotency key and Hermes is stateless within a single reasoning call, the retry produces a new complete output without double-side-effects.

**Mitigation:** Set container memory limits conservatively above P99 observed usage. Alert on OOM events. Do not increase limits blindly.

### 9.2 MCP server crash mid-tool-call

**Scenario:** The sandbox-invoice MCP process crashes while `parse_invoice_document` is executing.

**Behavior:** The activity times out or receives a connection error. Temporal retries the activity (Class B retry policy, up to 3 attempts). The MCP process is restarted by the container supervisor (same task group). The retry re-establishes the connection and re-executes the tool call with the same idempotency key, returning the cached result if the tool had already written to the idempotency log.

**Risk:** If the MCP process crashed before writing the idempotency record, the tool call re-executes. For read-class tools this is harmless. For draft-class tools, idempotency enforcement requires that the underlying Postgres write use an `ON CONFLICT DO NOTHING` on the idempotency key.

### 9.3 Temporal worker death

**Scenario:** The Temporal worker process exits while workflows are in flight.

**Behavior:** Temporal marks the workflow task as failed (sticky execution failure). On worker restart, the workflow is picked up from the last persisted checkpoint. No workflow state is lost. Activities that were in-flight are retried from their last scheduled state.

**Mitigation:** Worker process monitored by the container supervisor. Graceful shutdown drains in-progress activity tasks before exit.

### 9.4 LLM provider outage

**Scenario:** The Anthropic API returns 503 or connection errors during `InvokeHermesReasoning`.

**Behavior:** The activity fails with `LLMProviderError`. Temporal retries (Class A: up to 5 attempts with exponential backoff up to 30s). If all attempts fail, the workflow activity fails and the workflow moves to a `WaitingForProviderRecovery` state via a timer loop: retry after 5 minutes, up to 6 times (30 minutes total). If still unresolved, the workflow emits an `LLMOutageAlert` audit event and pauses at the activity level; a Temporal heartbeat keeps it alive.

**Operator impact:** The operator receives no approval packet until reasoning succeeds. No correction action needed from the operator.

### 9.5 Secret rotation mid-workflow

**Scenario:** A tenant's LLM API key is rotated while a 48-hour QuoteDraftingWorkflow is awaiting an operator signal.

**Behavior:** Because activities fetch credentials at activity-start (§2.5 DECISION), the next activity to execute after the rotation will fetch the new key automatically. The workflow does not need to be restarted. In-progress activities that already fetched the old key complete with it; retried activities fetch the new key.

**Risk:** If the old key is invalidated immediately before an in-flight HTTP call completes, that activity attempt fails and is retried with the new key. Class A/B retry policies handle this cleanly.

---

## 10. Test Strategy (TDD)

The invariants listed in §§2–7 are the source of truth for the test suite. Tests are specified before implementation.

### 10.1 Per-MCP contract tests

For each MCP server (sandbox-email, sandbox-drive, sandbox-invoice):

| Contract test | Invariant | Assertion |
|---|---|---|
| `test_write_final_absent_in_sandbox` | INV-E1, INV-D1, INV-I1, INV-S4 | `list_tools()` response must not contain `send_draft_email`, `publish_document`, `finalize_invoice`, or `submit_to_accounting` when `MODE=sandbox` |
| `test_capability_denied_for_unknown_tool` | INV-E1 | Calling any tool not in `list_tools()` returns `CapabilityDeniedError` |
| `test_idempotency_returns_cached_result` | §5.1 | Two calls with identical idempotency key return identical response; second call does not re-execute |
| `test_path_scoping_enforcement` | INV-D1 | A `read_file_content` call with a path outside `/{tenant_id}/` returns `CapabilityDeniedError` |
| `test_tenant_id_mismatch_rejected` | INV-H1 | A tool call where `request.tenant_id != server.TENANT_ID` returns `TenantMismatchError` |
| `test_sandbox_email_no_smtp` | INV-E1 | Creating and "sending" a draft email in sandbox mode produces a Postgres row, no SMTP connection |

### 10.2 Temporal workflow replay tests

| Contract test | Invariant | Assertion |
|---|---|---|
| `test_no_write_final_without_approved_signal` | INV-T3, INV-A1 | A workflow execution history with no `CaseApproved` signal must not contain a `SendEmailFinal` or `FinalizeInvoice` activity result |
| `test_replay_creates_new_case_run` | INV-CR2 | Triggering a replay of `cr_X` creates `cr_Y` with `replay_of="cr_X"` and a new WorkflowID |
| `test_original_execution_unchanged_after_replay` | INV-CR2 | After a replay, the Temporal history of `cr_X` is identical to its pre-replay state |
| `test_workflow_id_equals_case_run_id` | INV-CR1 | `StartWorkflow` input `WorkflowID` must equal `case_run_id` |
| `test_mode_immutable_within_run` | INV-CR3 | `mode` enum in workflow input cannot be overridden by a mid-workflow signal |
| `test_mode_absent_defaults_deny` | INV-T5 | A workflow input lacking `mode` is rejected with `MalformedInputError`; the workflow does not start |
| `test_mode_canonical_enum_only` | RESOLVED-N1 | A workflow input with `mode: true`, `mode: 0`, or `mode: "anything-else"` is rejected with `MalformedInputError` |
| `test_approved_signal_idempotency` | INV-A2 | Delivering `CaseApproved` twice with the same `signal_id` executes `WriteApprovalAuditEvent` once and `write_final` exactly once |
| `test_rejected_signal_no_write_final` | INV-A3 | A `CaseRejected` signal causes `PersistCorrection` to execute; `write_final` activities do not |
| `test_signal_tenant_mismatch_rejected` | INV-A4 | A signal envelope where `envelope.tenant_id != workflow.input.tenant_id` is rejected by the workflow handler |
| `test_approval_timeout_owned_by_temporal` | §7.2 | When `expires_at` passes with no signal, the Temporal-side timer fires; one reminder is sent; final timeout transitions to `CaseAbandoned` |
| `test_approval_audit_written_before_write_final` | INV-A1 | The `approval_received` AuditEvent is written by `WriteApprovalAuditEvent` BEFORE any `write_final` activity is dispatched (verified via Temporal history ordering) |

### 10.3 SkillVersion / replay determinism tests (RESOLVED-C2, C5)

| Contract test | Invariant | Assertion |
|---|---|---|
| `test_load_skill_version_pinned_at_start` | §8.4 | `LoadSkillVersion` is invoked exactly once per WorkflowExecution; `case_runs.skill_version_id` is set before the first `InvokeHermesReasoning` call |
| `test_skill_version_immutable_within_run` | §8.4 | A new SkillVersion promoted while a run is in flight does NOT replace the run's pinned `skill_version_id` |
| `test_replay_pins_skill_version_via_as_of` | §4.3, §8.2 | A replay of `cr_X` resolves the SkillVersion using `as_of = cr_X.started_at`; replay's `skill_version_id` equals `cr_X.skill_version_id` |
| `test_replay_temperature_zero` | §4.3 commitment 2 | When `replay_mode=true`, every `InvokeHermesReasoning` invocation uses LLM `temperature=0` |
| `test_replay_decision_outcome_determinism` | §4.3 commitment 4 | Two replays of the same `case_run_id` with the same `skill_version_id` produce identical `agent_output.action_taken` for every decision point (artifact bytes are NOT compared) |
| `test_skill_manifest_passthrough_field_parity` | RESOLVED-C14 | For every entry in the `LoadSkillVersion` response's `rule_manifest`, the corresponding entry in the `applicable_rules` block fed to Hermes has byte-identical field names (`rule_id`, `scope`, `version`, `decision_type`, `conditions_canonical`, `recommended_action`, `rationale`, `priority`); no field is renamed (e.g. `if_conditions` is forbidden) |

### 10.4 MCP server preflight tests (RESOLVED-C6)

| Contract test | Invariant | Assertion |
|---|---|---|
| `test_mcp_blocks_write_final_without_approval_audit` | INV-A1 (MCP gate) | Calling `send_draft_email` (live mode) when no `approval_received` row exists for `(case_run_id, decision_point_id)` returns `APPROVAL_REQUIRED`, no SMTP/transport call is made, a `blocked_write_attempted` AuditEvent is written |
| `test_mcp_allows_write_final_after_approval_audit` | §5.6 Gate 2 | Calling `send_draft_email` (live mode) after a matching `approval_received` row exists succeeds; tool executes |
| `test_mcp_sandbox_blocks_even_with_approval` | INV-T3 + Gate 1 | In `MODE=sandbox`, calling `send_draft_email` returns `SANDBOX_MODE` error even when an `approval_received` row exists; a `sandbox_escape_blocked` AuditEvent is written |
| `test_mcp_header_tenant_mismatch_rejected` | INV-MCP1, INV-MCP2 | A request with `X-Victoria-Tenant-Id != server.TENANT_ID` returns 403; a `security_violation` AuditEvent is written |
| `test_mcp_default_deny_when_mode_unset` | INV-T5 | An MCP server started without `MODE` env var defaults to `sandbox`; `write_final` tools absent from manifest |
| `test_persist_correction_from_signal_alone` | INV-A5 (RESOLVED-C13) | A `CaseRejected` signal carrying the canonical 16-field envelope (operator-ux §2.2, C13-R5) produces a complete `corrections` row even when the gateway DB is unreachable; the renames `parser_method` → `parse_method`, `parser_confidence` → `parse_confidence` are validated; missing/malformed fields raise `MalformedSignalError` instead of writing a partial row |

### 10.5 Side-effect classification invariants

All activities in the catalog (§3.4) must be annotated with their `SideEffectClass`. A static analysis check (run in CI) asserts:

- No `SideEffectClass.WRITE_FINAL` activity (defined as **external** side effect — outbound email, document publish, accounting submission) is registered with the Temporal worker when `HERMES_MODE=sandbox`.
- Every `WRITE_FINAL` activity has a corresponding contract test in 10.2 verifying it requires a prior `CaseApproved` signal AND a corresponding `approval_received` AuditEvent (10.4).
- `audit_write`-class activities (`EmitAuditEvent`, `PersistCorrection`, `WriteApprovalAuditEvent`) are internal Postgres writes and are registered in both modes; they are NOT the target of the sandbox or approval gate.

### 10.6 Tenant isolation tests

| Contract test | Assertion |
|---|---|
| `test_hermes_volume_tenant_label_match` | Container boot fails if `manifest.tenant_id != volume.tenant_id_label` |
| `test_hermes_volume_restart_binding` | After a container crash and restart, the Hermes container remounts the same `hermes-data-{tenant_id}` volume (Devil's Advocate INV-02) |
| `test_no_rule_content_on_hermes_volume` | INV-H6 (RESOLVED-HERMES-VOL) | Snapshot `/hermes/data/{tenant_id}/skills/` at bootstrap; run a workflow that loads a non-empty SkillVersion; assert post-run file count, per-file SHA-256, and full-text search for `rule_id`/`conditions_canonical`/`recommended_action` all match the bootstrap snapshot (zero rule content written to volume) |
| `test_task_queue_cross_tenant_unreachable` | A Temporal client configured for `victoria.tenant.t_123.quote_drafting` cannot dispatch to `victoria.tenant.t_999.quote_drafting` |
| `test_mcp_tenant_id_isolation` | MCP tool calls from a worker configured for `t_123` are rejected by an MCP server configured for `t_999` |
| `test_fixture_sandbox_flag` | A workflow started with a fixture where `fixture.sandbox != true` aborts with `SandboxContaminationError` |
| `test_object_store_path_isolation` | A drive MCP `list_files` call returns only files under `/{tenant_id}/`; no files from another tenant prefix are reachable |

### 10.7 End-to-end acceptance scenarios — exec-plane participation (Devil's Advocate §5; R4 expanded to all 8)

For each scenario in `05-architecture-integration-critique.md` §5, the table below names the execution-plane steps, the contract tests that own the exec-plane half, and the invariants the test exercises.

| Scenario | Exec-plane steps | Owning contract tests | Invariants exercised |
|---|---|---|---|
| **SC-01 Golden-path correction** | (1) Temporal workflow starts (`WorkflowID = case_run_id`); (2) `LoadSkillVersion` snapshots `skill_version_id`; (3) `InvokeHermesReasoning` produces decision point; (4) `CreateDraft` (sandbox-email/drive); (5) `SendApprovalPacket` to gateway; (6) `CaseRejected` signal received; (7) `PersistCorrection` writes `corrections` row + local audit + upstream drain; (8) replay triggered → new CaseRun with new `skill_version_id` after promotion | `test_workflow_id_equals_case_run_id`, `test_load_skill_version_pinned_at_start`, `test_skill_manifest_passthrough_field_parity`, `test_persist_correction_from_signal_alone`, `test_replay_creates_new_case_run` | INV-CR1, §8.4 pin, INV-A5, INV-CR2 |
| **SC-02 Multi-correction promotion** | (1) Three independent CaseRuns each emit a `CaseRejected` signal; (2) `PersistCorrection` writes three `corrections` rows; (3) fourth CaseRun starts with new `skill_version_id` after Learning's promotion | `test_persist_correction_from_signal_alone`, `test_skill_version_immutable_within_run` (per-run), `test_load_skill_version_pinned_at_start` (fourth run picks new manifest) | INV-A5, §8.4 pin |
| **SC-03 Contradicting-correction supersession** | (1) CaseRun applies `vr_X v1` via the manifest fetched at workflow start; (2) operator override → `CaseRejected` → `PersistCorrection`; (3) new CaseRun loads new SkillVersion with `vr_X v2` | `test_load_skill_version_pinned_at_start`, `test_skill_version_immutable_within_run`, `test_skill_manifest_passthrough_field_parity` | §8.4 pin, RESOLVED-C14 field parity |
| **SC-04 Abandoned packet** | Temporal timer fires at `expires_at`; one reminder `SendApprovalPacket` sent; second timeout transitions to `CaseAbandoned`; `case_abandoned` audit row written; no `corrections` row written | `test_approval_timeout_owned_by_temporal`, (new) `test_no_persist_correction_on_abandon` | INV-A1 (pre-condition), §7.2 timer ownership |
| **SC-05 Tenant isolation leak attempt** | Signal envelope with mismatched `tenant_id` → workflow handler rejects (INV-A4 defense in depth); MCP request with mismatched `X-Victoria-Tenant-Id` header → 403 + `security_violation` audit row | `test_signal_tenant_mismatch_rejected`, `test_mcp_header_tenant_mismatch_rejected`, `test_task_queue_cross_tenant_unreachable` | INV-A4, INV-MCP1, INV-MCP2, INV-T4 |
| **SC-06 Sandbox-mode escape attempt** | `write_final` tools absent from MCP `list_tools()` in sandbox mode; if invoked out-of-band, Gate 1 hard-rejects regardless of approval; missing `MODE` env var → defaults to sandbox | `test_write_final_absent_in_sandbox`, `test_mcp_sandbox_blocks_even_with_approval`, `test_mcp_default_deny_when_mode_unset`, `test_mode_canonical_enum_only` | INV-T3, INV-T5, INV-S4, RESOLVED-N1 |
| **SC-07 Replay determinism check** | Two replays of `cr_X` at T1 and T2 with same pinned `skill_version_id`, `temperature=0`, snapshot-from-artifact-store reads → identical `agent_output.action_taken`/`branch_taken`/`extracted_facts` per decision point | `test_replay_pins_skill_version_via_as_of`, `test_replay_temperature_zero`, `test_replay_decision_outcome_determinism`, `prop_replay_determinism_n_runs` (§10.8) | §4.3 four commitments, §8.4 pin, INV-CR2 |
| **SC-08 Double-tap approval idempotency** | Two `CaseApproved` signals with same `signal_id` → workflow signal handler dedups (INV-A2); exactly one `WriteApprovalAuditEvent` row; exactly one `write_final` activity dispatched; MCP preflight Gate 2 sees one `approval_received` row | `test_approved_signal_idempotency`, `test_approval_audit_written_before_write_final`, `test_mcp_allows_write_final_after_approval_audit` | INV-A1, INV-A2, §5.6 Gate 2 |

### 10.8 Property and fuzz tests (R4 NEW)

#### `prop_replay_determinism_n_runs` — replay determinism property test

**Property.** For any `case_run_id` whose original run completed, replaying the same case N times with identical fixtures and pinned `skill_version_id` produces identical `agent_output.action_taken`, `agent_output.branch_taken`, and `agent_output.extracted_facts` for every decision point in every run.

**Choice of N (derived, not hardcoded).** Treat the property as a binomial test against the null hypothesis "the determinism contract holds." For a single decision point with binary outcome, a single non-deterministic regression slipping through observed runs has false-negative probability `(1-p)^N` where `p` is the per-run probability of observing the regression. To reach a false-negative rate ≤ 5% at 95% confidence given a per-run regression probability of p ≥ 0.05 (the smallest practically detectable rate), N ≥ ⌈log(0.05) / log(0.95)⌉ = 59. The test runs N = 60 replays per case in CI. The number is documented as a discriminator function — *not* a magic constant — and is recomputed from the discriminator if the per-run probability assumption changes.

**Failure mode.** Any divergence in `action_taken`, `branch_taken`, or `extracted_facts` between two of the N runs is a hard fail. The test reports the divergent decision point and the diff.

**Out of scope.** Artifact byte content (LLM-generated text) is excluded from the determinism contract per §4.3 commitment 4.

#### `fuzz_mcp_write_final_three_gate` — full Cartesian fuzz of the 3-gate model

**Property.** For every combination of (gate-1-input, gate-2-input, gate-3-input), the MCP server's `write_final` preflight responds correctly per §5.6:

| Gate 1 (mode) | Gate 2 (approval) | Gate 3 (header) | Expected response | Audit row written |
|---|---|---|---|---|
| `sandbox` | any | match | `SANDBOX_MODE` | `sandbox_escape_blocked` |
| `sandbox` | any | mismatch | `SANDBOX_MODE` (Gate 1 fires first) | `sandbox_escape_blocked` |
| `live` | missing | match | `APPROVAL_REQUIRED` | `blocked_write_attempted` |
| `live` | wrong `decision_point_id` | match | `APPROVAL_REQUIRED` | `blocked_write_attempted` |
| `live` | wrong `case_run_id` | match | `APPROVAL_REQUIRED` | `blocked_write_attempted` |
| `live` | present + correct | mismatch | `TENANT_MISMATCH` HTTP 403 | `security_violation` |
| `live` | present + correct | match | tool executes | `write_final_executed` |
| missing/unrecognized | any | any | `SANDBOX_MODE` (default-deny per INV-T5) | `sandbox_escape_blocked` |

**Falsifiability.** Every row is a separate test case; collectively they constitute the full Cartesian space (3 mode states × 4 approval states × 2 header states = 24 cases, of which 7 unique outcome rows are listed above). A single passing row in isolation does not satisfy the property; the full table must pass.

**Implementation.** Deliver each combination via a synthetic MCP request with the gate-relevant fields varied; assert the response code, error code, and audit row. Time-of-day randomization (parameterized fuzz) is applied to non-gate fields to ensure the response is determined only by the gate inputs.

#### `prop_sandbox_mode_no_external_invocation` — sandbox-mode escape property test

**Property.** For any input fixture, persona, or seeded message text, no Temporal activity invoked during a sandbox-mode workflow execution can reach an MCP process whose registry contains `write_final` tools.

**Falsifiability.** The test enumerates the workflow's recorded activity history; for every activity invocation, it asserts:
- The activity's MCP target endpoint resolves to a process whose `MODE` env var is `sandbox` (or unset, which defaults to sandbox per INV-T5).
- The MCP target's `list_tools()` response does not include `send_draft_email`, `publish_document`, `finalize_invoice`, or `submit_to_accounting`.
- No DNS lookup or socket connection from the activity worker reaches a `MODE=live` MCP process address.

**Why a property test, not a contract test.** The space of inputs that could cause Hermes to "try to call a live tool" is open-ended (prompt injection, malformed fixtures, base64-encoded persona overrides). A single contract test cannot enumerate them. The property test asserts the structural impossibility — sandbox-mode workflows physically cannot reach live MCP processes regardless of input — and is run with a fuzz generator that produces adversarial fixtures (base64 / Unicode / system-prompt-injection patterns).

---

## 11. Cross-Component Contract Tests (R4 NEW)

This section is the single load-bearing artifact for the exec-plane's peer-boundary test surface. Each row binds a contract test to (a) the producer-side spec section, (b) the consumer-side spec section, (c) the fixture owner, (d) the input shape, (e) the expected output, and (f) what the test would catch if it failed. Tests are owned jointly: producer publishes the fixture; consumer publishes the assertion suite.

| Test name | Producer spec § | Consumer spec § | Fixture owner | Input | Expected output | What fails |
|---|---|---|---|---|---|---|
| `ct_load_skill_version` | ctrl-plane §13.3 (resolver) + learning-loop §9.1 (manifest schema) | exec-plane §8.2 (`LoadSkillVersion`) | learning-loop (fixture: a known SkillVersion row in test-tenant DB) | `(tenant_id, workflow_slug, as_of?)` over mTLS | `SkillVersion` JSON byte-identical to `rule_manifest` field set | Field rename, missing rule, stale snapshot, mTLS handshake failure |
| `ct_skill_manifest_passthrough` | learning-loop §9.1 | exec-plane §8.3 | learning-loop | A `SkillVersion` with `rule_manifest[]` containing 3 rules of mixed scopes | The `applicable_rules` block injected into Hermes's system prompt has byte-identical field names (`conditions_canonical`, `recommended_action`, `priority`, …); no rename to `if_conditions` / `then_action` | C14 regression (renamed at the boundary) |
| `ct_mcp_approval_events_select` | learning-loop §11.5 (view + role grant) | exec-plane §5.6 (Gate 2) | learning-loop (fixture: one `approval_received` row for `t_A`, one for `t_B`) | MCP server connected as `mcp_audit_reader` with `SET LOCAL app.current_tenant='t_A'`; query `mcp_approval_events` | One row returned for `t_A`'s `(case_run_id, decision_point_id)`; zero rows for `t_B`'s | RLS broken; role grant scope leaking; view filter wrong |
| `ct_write_approval_audit_durable_drain` | exec-plane §5.6 / §13.4 | ctrl-plane §9.4 (audit ingest) | exec-plane (fixture: a `CaseApproved` signal with valid envelope) | `WriteApprovalAuditEvent` activity executes; control-plane audit ingest endpoint returns 503 for 30s, then 200 | `audit_events_outbox` row written immediately; upstream drain retried; on-success the drain is idempotent (ctrl-plane §17 SHA-256 key prevents duplicate insert at control plane); no row loss | Buffer drops events on outage; idempotency key collision; cert SAN mismatch on retry |
| `ct_persist_correction_from_signal_alone` | operator-ux §2.2 (16-field envelope, C13-R5) | exec-plane §7.4 | operator-ux (fixture: full canonical 16-field envelope with `parser_method`/`parser_confidence`) | Deliver `CaseRejected` signal with all 16 fields; gateway DB unreachable | `corrections` row written with `parse_method`/`parse_confidence` (renamed at boundary), all other fields per §7.4 mapping | Rename mismatch; missing field; gateway-callback enrichment (forbidden by INV-A5) |
| `ct_mcp_tenant_header_binding` | exec-plane §5.7 / provisioning §2.7 | exec-plane §5.6 (Gate 3) | provisioning (fixture: MCP server bound to `t_A` at startup) | Send tool call with `X-Victoria-Tenant-Id: t_B` body `tenant_id: t_A` | HTTP 403 `TENANT_MISMATCH`; `security_violation` audit row | Header-vs-startup-binding check skipped; audit row missing |
| `ct_provisioning_manifest_config_mount` | ctrl-plane §5.6 (manifest delivery) | exec-plane §2.7 | ctrl-plane (fixture: provisioning workflow run with known `(tenant_id, hermes_version, mode, mcp_endpoints, …)`) | Provisioning workflow runs to step 7 (`DeployHermesContainer`) | `/hermes/config/manifest.json` exists and matches the input; volume label `tenant_id` matches manifest; container starts and reaches healthy state | Manifest written for wrong tenant; tenant_id label mismatch (INV-H4); manifest mutated post-write |
| `ct_replay_trigger_admin_to_internal` | ctrl-plane §8.7 (`POST /admin/replays`) | exec-plane §13.3 (`POST /internal/replay`) | ctrl-plane (fixture: an `original_case_run_id` with completed run + MCP read snapshots) | Reviewer triggers `POST /admin/replays` over JWT; ctrl-plane evaluation service translates to `/internal/replay` over mTLS | 202 from exec-plane with new `case_run_id`; replay workflow starts; `replay_started` audit event written | Idempotency-key collision; missing snapshot precondition; mTLS SAN mismatch |
| `ct_emit_alert_from_exec_plane` | ctrl-plane §9.6.1 (alert sink) | exec-plane (MCP servers; Temporal worker) | exec-plane (fixture: a triggered alert e.g. `LLMOutageAlert`, severity=warning) | MCP server emits alert via mTLS POST to control plane | Alert is recorded; routing rule evaluated; `alerts.idempotency_key` UNIQUE prevents duplicate within bucket | Alert dropped on control-plane outage (covered by durable buffer); idempotency-key collision; cert SAN mismatch |
| `ct_temporal_signal_iam_path_a` (path A) | ctrl-plane §6.8 (per-tenant Temporal cred) | exec-plane signal handler | ctrl-plane (fixture: gateway's signal-only Temporal cred for `t_A`) | Gateway calls `SignalWorkflow` on `victoria.tenant.t_A.*` workflow ID | Signal delivered; workflow advances; gateway cred CANNOT call `StartWorkflow` or `TerminateWorkflow` (returns Temporal authz error) | If Temporal IAM does not support signal-only role on a queue glob, this test fails — escalates to path B |
| `ct_temporal_signal_iam_path_b` (path B fallback) | operator-ux §2.3 (signal-proxy sidecar) | exec-plane signal handler | operator-ux + ctrl-plane jointly | Gateway calls signal-proxy HTTP API; proxy holds the Temporal cred and translates to `SignalWorkflow` | Same observable result as path A; proxy is the only Temporal-authenticated service in the gateway-side trust circle | Proxy outage; mTLS handshake to proxy; proxy fails to dedup on `signal_id` |

**Cross-spec contract test ownership.** The contract tests above run in a dedicated repo owned jointly by the producer and consumer. The producer publishes the fixture set; the consumer publishes the assertion suite. Failures block CI on both sides — that is the property the moderator's R4 brief requires.

---

## 12. Decisions Made and Defended

| # | Decision | Rationale | Will defend against |
|---|---|---|---|
| D1 | Single Temporal namespace; task queues per tenant (Phase 1) | Spec mandates task-queue isolation for Phase 1; namespace-per-tenant is Phase 3 overhead | Peer suggestion to use namespace-per-tenant "for safety" |
| D2 | 1:1 CaseRun ↔ WorkflowExecution; replays create new CaseRun + new execution | Immutable audit trail; `ResetWorkflow` is destructive | Any suggestion to use `ResetWorkflow` for replays |
| D3 | Sandbox and live MCP servers are separate processes, not a flag | One bug away from real side effects; defense-in-depth matches the product spec's "safety before autonomy" principle | Complexity argument for a single MCP with mode flag |
| D4 (revised, RESOLVED-C2) | The runtime artifact is `SkillVersion` (Learning Architect's name and schema). `LoadSkillVersion` activity returns the manifest; the execution plane materializes it into a system-prompt block. `case_runs.skill_version_id` is the time-travel anchor. | Single name, single schema; no parallel identifiers; aligns with `03-correction-loop.md` §9 | Suggestions to keep the Round 1 `rule_snapshot_id` invented term |
| D5 | `write_final` tools absent from MCP `list_tools()` in sandbox mode (not just blocked at call time) | Hermes cannot select a tool it cannot see; no chance of accidental selection | Argument that a runtime block is sufficient |
| D6 | Credentials fetched at activity-start from secret store, never passed as env vars or baked in | Rotation propagates to running workflows; credentials never appear in Temporal history | Argument that env-var injection is simpler |
| D7 | `HERMES_MODE` is immutable at provisioning time; requires reprovisioning to change | Prevents runtime toggle that could be exploited or misconfigured | Argument for a runtime toggle for "quick testing" |
| D8 (NEW, RESOLVED-C6) | MCP server independently checks `audit_events` for `approval_received` before any `write_final` tool executes (live mode); sandbox mode is a separate hard gate | Side-effect gate must be co-located with the side effect; bugs in workflow/Hermes code cannot bypass | Argument that Temporal-side check alone is sufficient |
| D9 (NEW, RESOLVED-C3) | Messaging gateway emits Temporal signals via Temporal SDK directly; control plane is not on the signal path; Temporal owns the timeout | Adding a control-plane hop adds no security gain; gateway's tenant-bound auth is the auth boundary | Argument to centralize all signals through control plane |
| D10 (NEW, RESOLVED-C4) | Provisioning manifest delivered via config-mount at boot; runtime RPCs separately use HTTP+HMAC | Config-mount is reproducible and works without inbound HTTP on the execution plane | Argument to use a single HTTP-RPC channel for both bootstrap and runtime |
| D11 (NEW, INV-T5; revised in R3 N1) | Default-deny: missing `mode` field in workflow input rejects start; missing `MODE` at MCP defaults to `sandbox`. Canonical encoding is the enum string `"sandbox"` \| `"live"`; booleans forbidden. | A bug that drops the mode field must not silently switch to live; default to the safer side | Argument that strict input validation is sufficient and defaults are unnecessary |

---

## 13. Open Questions and Anticipated Conflicts

### Round 1 questions — Round 2 status

| # | Question | Round 2 status |
|---|---|---|
| OQ1 (manifest delivery) | RESOLVED-C4: config-mount, see §2.7 |
| OQ2 (signal transport) | RESOLVED-C3: gateway → Temporal SDK direct, see §7.2 |
| OQ3 (Learning consumption of correction events) | RESOLVED: Learning Architect consumes the `corrections` table per `03-correction-loop.md` §2.6; execution plane writes the row + AuditEvent |
| OQ4 (`/rules` endpoint with `as_of`) | RESOLVED-C2: replaced by `LoadSkillVersion` activity; `as_of` confirmed needed (see §8.2); Learning Architect must support it |
| OQ5 (replay scheduler ownership) | RESOLVED: ctrl-plane Evaluation Service is the replay scheduler; see §13.3 (`POST /internal/replay`) |

### Still open after Round 2

| # | Question | Owner | Impact on this spec |
|---|---|---|---|
| OPEN-OQ5 | Who triggers a replay? Does the control plane call `/internal/replay` on the execution plane (per `01-control-plane.md` §5.3), or does the execution plane subscribe to a control-plane event? Concretely: when a reviewer promotes a candidate that requires regression replay, what is the call shape? | Control Plane Architect | §4.3 replay trigger; affects §10.7 SC-07 test wiring |
| OPEN-OQ-MCP-Snapshots | The replay-determinism commitment 3 (§4.3) requires storing MCP read-tool snapshots as artifacts during the original run. The artifact_type enum in `03-correction-loop.md` §2.5 does not include `mcp_read_snapshot`. Needs Learning Architect schema extension. | Learning Architect | §4.3 commitment 3 |
| OPEN-OQ-Audit-Read-Role | The MCP server preflight (§5.6) requires `SELECT` on the per-tenant `audit_events` table. Learning Architect owns this schema. Needs a `mcp_audit_reader` role grant. Also needs the `approval_received` event_type added to the `03-correction-loop.md` §11.3 registry. (Also tracked as NEW-C9 below.) | Learning Architect | §5.6 cannot be implemented without this |
| OPEN-OQ-Hermes-Skill-Volume | `03-correction-loop.md` §15 CONFLICT-1 flagged that Learning may prefer pre-loading skill files into the Hermes container vs. runtime injection. This spec asserts **runtime context-block injection only**; skill files on the Hermes data volume are the persona/starter skills (Round 1 §2.7), not rule containers. Resolution: ValidatedRule manifests are NEVER written to the Hermes data volume; they pass through Temporal activity inputs only. Learning Architect: confirm acceptance. | Learning Architect | §8 |

### NEW conflicts surfaced in Round 2

**NEW-C9 — MCP server needs `audit_events` read access (Learning Architect owns the table):**
- The Round 2 acceptance of devils-advocate INV-03 (RESOLVED-C6) requires the MCP server to synchronously query `audit_events` before any `write_final` tool executes.
- The `audit_events` schema is owned by `03-correction-loop.md` §11.
- Resolution requires (a) a `mcp_audit_reader` Postgres role granted SELECT on `audit_events` only, (b) addition of `approval_received` to the event-type registry in `03-correction-loop.md` §11.3 with this spec named as the writer (via `WriteApprovalAuditEvent` activity).
- Surface to Learning Architect for Round 3 schema update.

**NEW-C10 — Approval AuditEvent writer responsibility:**
- Round 1 of `03-correction-loop.md` §11.3 lists Operator UX as the writer of `correction_received` and `correction_approve`. This Round 2 spec moves the writer of `approval_received` (a NEW event type) to the **execution plane** (the Temporal workflow's signal handler via `WriteApprovalAuditEvent` activity). This avoids requiring the gateway to write to the per-tenant DB.
- Operator UX still writes `correction_received` per their own §10.1 step 1 (gateway-side audit log). The execution plane's `PersistCorrection` activity may write a *second* audit event in the per-tenant DB if that DB is the durable store; needs alignment with operator-ux on whether the gateway's audit log and the per-tenant `audit_events` table are the same store or two stores.
- Surface for Round 3.

### Anticipated remaining conflicts with peer architects

| Conflict | My position | Likely counter-position |
|---|---|---|
| Hermes skill volume contents | Persona + starter-skill files only; ValidatedRule manifests never land on the volume | Learning Architect may want skill files updated on promotion |
| Fixture injection | Control plane pushes fixtures to execution plane Postgres at provisioning | Control plane may prefer pull-on-demand |
| `InvokeHermesReasoning` as a black box | The execution plane calls Hermes with a structured prompt + rules context; Hermes reasoning internals are opaque to this spec | Devil's Advocate may demand more observability into the reasoning step |
| Replay snapshot artifact type | MCP read-tool snapshots stored as a new `artifact_type = 'mcp_read_snapshot'` | Learning Architect may want them stored separately, not in `artifacts` table |

---

## Appendix A: Side-Effect Classification Reference

| Class | Description | Approval required | Sandbox behavior |
|---|---|---|---|
| `read` | Queries data; no state change | No | Allowed |
| `draft` | Creates or updates a local artifact (email draft, file, invoice draft); no external action | No | Allowed |
| `audit_write` | Internal Postgres write to `audit_events`, `corrections`, or other internal tables; no external visibility | No | Allowed (the correction loop depends on these in sandbox) |
| `write_pending_approval` | Sends a packet to the operator; externally visible but not irreversible | No (is itself the approval request) | Allowed |
| `write_final` | Irreversible **external** action: sends email, publishes document, finalizes invoice, submits to accounting | Yes — `CaseApproved` signal AND matching `approval_received` AuditEvent required | Blocked; tool absent from MCP manifest |

---

## Appendix B: Provisioning Manifest Schema (Reference)

```json
{
  "$schema": "https://victoria.internal/schemas/provisioning-manifest/v1.json",
  "tenant_id": "string (required)",
  "hermes_version": "string (semver, required)",
  "llm_model": "string (required)",
  "mode": "enum 'sandbox' | 'live' (required; canonical per RESOLVED-N1)",
  "workflow_templates": ["enquiry_triage | quote_drafting | invoice_handling"],
  "mcp_endpoints": {
    "sandbox_email": "string (URL)",
    "sandbox_drive": "string (URL)",
    "sandbox_invoice": "string (URL)"
  },
  "skill_version_endpoint": "string (URL)",
  "vertical": "string (e.g. roofing, plumbing, landscaping)",
  "signal_wait_timeouts_hours": {
    "enquiry_triage": 12,
    "quote_drafting": 48,
    "invoice_handling": 48
  }
}
```

---

## 14. Runtime RPC Surface and Storage Topology Contribution (RESOLVED-C4, OQ-NEW-3, REPLAY-SCHED)

### 13.0 Invariants for internal RPC

- **INV-RPC1:** All execution-plane ↔ control-plane traffic terminates mTLS; non-mTLS traffic is rejected at the listener.
- **INV-RPC2:** Every inbound `/internal/*` request validates the client cert SAN against the expected service identity; mismatch → 403 + `security_violation` audit row.
- **INV-RPC3:** Every outbound call from the execution plane to the control plane carries the tenant-scoped exec-plane leaf cert (SAN matches `t_<tenant_id>.exec.*`); a cert intended for tenant A cannot be used to issue calls on behalf of tenant B.
- **INV-RPC4:** Bootstrap config-mount is the only way the execution plane learns its provisioning manifest; runtime RPCs cannot rewrite the manifest.

Contract tests for these invariants are in §10.4 / §11.

### 13.1 Service-to-service auth: mTLS (RESOLVED-OQ-NEW-3, REVISED in R4)

**Accept ctrl-plane's pick** (`01-control-plane.md` §6.2): all execution-plane ↔ control-plane internal RPCs use **mutual TLS** with certificates issued by an internal CA. Ctrl-plane R3 §6.2 explicitly reverses the R2 HMAC framing in favor of mTLS; this spec accepts the reversal and propagates it through every inbound and outbound channel.

**Why mTLS** (per ctrl-plane §6.2): mutual identity, transport security, and forward secrecy in a single mechanism. Cert rotation is online via short-lived leaves issued at provisioning (`IssueServiceCertificates`, ctrl-plane §5.2 step 6). HMAC + workload identity is explicitly NOT used; bearer tokens at the application layer are explicitly NOT used (exception: `/internal/replay` uses HMAC bearer for faster setup — see §13.3).

| Direction | Endpoint | Cert / SAN |
|---|---|---|
| exec-plane → ctrl-plane | `LoadSkillVersion` (§13.3), audit ingest (§13.4), `emit-alert` (§13.5) | Exec-plane leaf, SAN = `t_<tenant_id>.exec.victoria.internal` |
| ctrl-plane → exec-plane | `/internal/health`, `/internal/replay` (§13.6), `/internal/audit/events/drain` | Ctrl-plane service cert (e.g. `ctrl.evaluation.*`, `ctrl.provisioning.*`); execution plane validates `ctrl.*` SAN prefix |
| Gateway → Temporal cluster (signal-only) | not an `/internal/*` call | Gateway's per-tenant Temporal credential per ctrl-plane §6.8; mTLS at transport, Temporal authorization at the application layer |

**No bearer tokens** (exception: `/internal/replay`, see §13.3). The R3 references in this spec to "HMAC-signed bearer tokens" are superseded by mTLS for all endpoints except `/internal/replay`, which retains HMAC bearer auth for faster setup. The `service_cert_fingerprint` in the deployment record (ctrl-plane §5.3) is the trust anchor for all mTLS-authenticated endpoints.

**Rotation.** Leaf certificates have short TTL (per ctrl-plane §6.2). The execution plane re-fetches its leaf at activity-start (D6 from R1: credentials read at activity-start, never workflow-start). Rotation propagates to in-flight workflows without restart.

**Mode-default-deny:** A failed mTLS handshake (expired cert, untrusted CA, SAN mismatch) is a non-retryable error. The execution plane does NOT fall back to plaintext, bearer tokens, or any other mechanism.

### 13.2 Endpoint catalog

The execution plane exposes the following internal HTTP endpoints, accessible only from the control-plane internal network and authenticated per §13.1. These are **runtime** interfaces, distinct from the bootstrap config-mount (§2.7).

| Endpoint | Method | Caller | Purpose |
|---|---|---|---|
| `/internal/health` | GET | Control plane | Liveness/readiness probe (used during provisioning HealthCheck) |
| `/internal/replay` | POST | Control plane (Evaluation Service) | Trigger a replay of a CaseRun; see §13.3 for body shape and idempotency |
| `/internal/audit/events/drain` | POST | Control plane | Pull-drain buffered audit events when control-plane audit ingest was down |
| `/internal/skill-versions/invalidate` | POST | Control plane | Optional cache-invalidation hint when a new SkillVersion is promoted; non-authoritative (RESOLVED-A10) |

The execution plane does NOT expose `/internal/rules/push`. Rules are pulled via `LoadSkillVersion` at workflow-start (RESOLVED-A10 / R2-C11). The R3 binding consensus in `00-overview.md` §2 confirms pull-at-start authoritative; control-plane push is downgraded to a non-authoritative cache hint.

### 13.3 `POST /internal/replay` — replay trigger API (RESOLVED-REPLAY-SCHED / RESOLVED-OQ5)

**RESOLVED-REPLAY-SCHED:** Control plane's Evaluation Service is the replay scheduler. The execution plane is a stateless executor of replay requests; it does not poll, schedule, or initiate replays itself.

**Auth exception (HMAC bearer):** This endpoint uses HMAC bearer auth (`Authorization: Bearer <hmac-signed token>`) rather than mTLS client-cert auth. This is an acknowledged exception to the mTLS-only mandate in §13.1, chosen for faster initial setup of the replay trigger path. The HMAC secret is injected via the provisioning manifest (§2.7). All other `/internal/*` endpoints use mTLS per §13.1.

**Request:**

```
POST /internal/replay
Authorization: Bearer <hmac-signed token>
Content-Type: application/json

{
  "replay_run_id": "rpl_9f2a",         // ctrl-plane-assigned, idempotency key
  "case_run_id": "cr_456",             // original CaseRun being replayed
  "skill_version_id": "sv_r7q1",       // optional; if omitted, exec-plane uses
                                       //   as_of = original_case.started_at
                                       //   to look up the historical version
  "triggered_by": "reviewer:alice@victoria.app",
  "replay_reason": "candidate_promotion_check"
                                       //   "candidate_promotion_check" | "regression_suite" | "manual"
}
```

**Response (202 Accepted):**

```json
{
  "replay_run_id": "rpl_9f2a",
  "replay_case_run_id": "cr_612",      // new CaseRun ID for the replay execution
  "workflow_id": "cr_612",             // = replay_case_run_id (INV-CR1)
  "status": "started"
}
```

**Required pre-existing state (preconditions checked before 202):**

| Precondition | If missing |
|---|---|
| `case_run_id` exists in this tenant's per-tenant DB | 404 `CaseRunNotFound` |
| Original case has `input_payload` and `decision_points` populated | 409 `OriginalCaseIncomplete` |
| If `skill_version_id` provided: a SkillVersion with that ID exists at the control plane resolver | 404 `SkillVersionNotFound` |
| Tenant `mode` allows replay (sandbox tenants always allowed; live tenants per future config) | 403 `ReplayForbidden` |
| The MCP read-tool snapshots from the original run exist in the artifact store under `cases/{case_run_id}/snapshots/` | 409 `OriginalSnapshotsMissing` (replay determinism cannot be guaranteed) |

**Idempotency:** Keyed on `replay_run_id`. A duplicate POST with the same `replay_run_id`:
- Returns 200 with the existing `replay_case_run_id` if a replay was previously started.
- Returns 409 `ReplayInTerminalState` if the replay has completed or failed (caller must use a new `replay_run_id` for a fresh attempt).

**Side effects on accept:**
1. New `case_runs` row created with `id = <fresh cr_*>`, `replayed_from_id = case_run_id`, `mode = original.mode`, `replay_mode = true`, `skill_version_id = <pinned>`.
2. New Temporal `WorkflowExecution` started with `WorkflowID = replay_case_run_id`, input includes `replay_run_id`, `replay_mode = true`.
3. `replay_started` audit event written.

**No correction packet is sent during a replay.** Approval-gating is bypassed for replay runs (the workflow uses the original run's recorded approvals from Temporal history if needed, but `write_final` activities are not registered for replay workers — replay is always a sandbox-equivalent execution).

**Failure mode:** If the workflow fails to start for any reason, the endpoint returns 500 with `replay_run_id` echoed; ctrl-plane's Evaluation Service retries with the same `replay_run_id` (idempotent).

OPEN-OQ-MCP-Snapshots: CLOSED in R4 — learning-loop §2.5 added `artifact_type = 'mcp_read_snapshot'` (with `mcp_tool_name` and `mcp_idempotency_key` columns); the precondition "original snapshots exist" is now schema-enforced.

### 14.4 Storage topology contribution (R4 NEW — Task 6)

This subsection extends ctrl-plane's storage-topology table (`01-control-plane.md` §13) with the per-tenant execution-plane DB tables this spec writes/reads. It does NOT redefine ctrl-plane's authoritative tables.

**R5 alignment.** Learning-loop §2.0 is the canonical Storage Topology table. This subsection contributes only the **per-tenant exec-plane** rows; it does not redefine ctrl-plane's authoritative `audit_events` or any control-plane table. Per-tenant DBs have RLS enabled with `app.current_tenant` SET LOCAL on every transaction.

| Table | Owner / Schema | Writer (role) | Reader (role) | RLS predicate | Notes |
|---|---|---|---|---|---|
| `case_runs` | learning-loop §2.3 | exec-plane Temporal worker (default app role) | exec-plane Temporal worker; learning-loop replay-diff job (cross-DB read) | `tenant_id = current_setting('app.current_tenant')` | INV-CR1 enforces `id = workflow_id`; `replayed_from_id` set on replay |
| `decision_points` | learning-loop §2.4 | exec-plane Temporal worker | exec-plane Temporal worker; learning-loop Stage B parser via cross-DB read | same RLS | `applied_rule_ids[]` snapshot per `skill_version_id` |
| `artifacts` | learning-loop §2.5 (R3 added `mcp_read_snapshot` type) | exec-plane Temporal worker; MCP servers (snapshot rows) | exec-plane (replay reads); learning-loop (replay diff) | same RLS | `mcp_read_snapshot` rows enable replay determinism (§4.3 commitment 3) |
| `corrections` | learning-loop §2.6 | exec-plane `PersistCorrection` activity (sole writer, RESOLVED-C10) | learning-loop Stage B parser; reviewer console via cross-DB read | same RLS | `idempotency_key` UNIQUE; SHA-256 derivation per ctrl-plane §17 |
| `rule_candidates` | learning-loop §2.7 | learning-loop Stage B parser (local pipeline) | learning-loop; ctrl-plane reviewer console via stripped replica `rule_candidates_control` | same RLS | NOT written by exec-plane; listed for completeness only |
| `audit_events_outbox` (R5: rename from R3 "local audit_events") | learning-loop §2.0 + §2.10; durable transactional buffer for outage tolerance only | exec-plane Temporal worker; MCP servers (each writes its own outbox row in the same transaction as the business state change) | exec-plane drain worker only — **not a read source** for application logic | same RLS; BEFORE-mutation trigger allows only `drained` flag flip from `false → true` (ctrl-plane §9.3 INV-AUDIT-01) | Single allowed mutation: `drained = true` after successful upstream POST. 24h post-drain retention then deleted. |
| `mcp_idempotency_log` | exec-plane (this spec §5.1) | MCP servers (default app role) | MCP servers | `tenant_id = current_setting('app.current_tenant')` | UNIQUE on `(tool_name, idempotency_key)`; TTL = case_run lifetime |

**Audit storage topology summary (R5 single-store):**

```
Exec-plane Temporal worker / MCP servers
  │
  │  one transaction:
  │    1. business INSERT (e.g. corrections row) into per-tenant DB
  │    2. audit_events_outbox INSERT into per-tenant DB (drained=false)
  │  commit
  │
  │  AuditDrainer activity (same workflow turn, then background retry on failure)
  │    POST /internal/audit/events to control plane over mTLS
  │    on 200 → UPDATE outbox SET drained=true
  ▼
control-plane audit_events  (SINGLE AUTHORITATIVE STORE; ctrl-plane §9.3)
  ▲
  │
  │  mTLS over mcp_audit_reader role with mcp_approval_events view
  │  (RLS-scoped to event_type='approval_received' AND tenant_id=current_tenant)
  │
MCP server WRITE_EXTERNAL preflight (§5.6 Gate 2 — Read-After-Write contract)
  │
  │  also queried by reviewer console via control-plane audit query API
  ▼
Reviewers
```

The MCP `WRITE_EXTERNAL` preflight (Gate 2, §5.6) reads the **control-plane** store directly via mTLS — there is no read against the outbox. Workflow-side ordering ensures the upstream POST has returned 200 before the workflow advances to the `write_final` step, so Gate 2 always sees the row. Latency overhead (~5–10ms intra-VPC) is acknowledged in OPEN-AUDIT-LATENCY-R4 (ctrl-plane §9.3); R5 accepted this in favor of single-source-of-truth correctness.

**Closed in R5:** the R4-NEW item AUDIT-OUTBOX (asking learning-loop to acknowledge the outbox table) is closed by learning-loop §2.0 / §2.10 naming `audit_events_outbox` as the canonical row in their topology. AUDIT-DRAIN batch semantics live with ctrl-plane §9.4 `/internal/audit/events` definition.

---

## 15. Round 2 Audit (Devil's Advocate Cross-Reference)

This section maps the Devil's Advocate critique items in `05-architecture-integration-critique.md` to their status against this spec after Round 2.

| Item | Status | Resolution location |
|---|---|---|
| §1.1 tenant_id propagation chain (MCP header name) | RESOLVED | §5.7 INV-MCP1: `X-Victoria-Tenant-Id` |
| §1.2 case_run_id propagation chain | RESOLVED | §4 (1:1 with WorkflowID), §5.1 (idempotency keys carry it) |
| §1.3 RuleCandidate physical location | RESOLVED (consumption side); ctrl-plane and learning-loop own the storage decision | §8 |
| §1.4 How Hermes consumes ValidatedRules (model A/B/C/D) | RESOLVED — model C (system-prompt context block from SkillVersion manifest) | §8 |
| §1.5 ApprovalSignal contract with idempotency | RESOLVED | §7.3 (signal_id), INV-A2 |
| §1.6 Sandbox-vs-live mode flag hierarchy | RESOLVED — three layers + default-deny | §3.1 INV-T5, §6.3 |
| §1.7 Audit chain (write_final preflight inside MCP) | RESOLVED | §5.6 |
| §1.8 SkillVersion boundary | RESOLVED-C2 | §8 |
| INV-01 (no MCP cross-tenant credentials) | RESOLVED | §5.7 INV-MCP1, MCP test in §10.4 |
| INV-02 (Hermes data volume isolation) | RESOLVED | §2.3, §10.6 `test_hermes_volume_restart_binding` |
| INV-03 (no WRITE_EXTERNAL without approved AuditEvent) | RESOLVED | §5.6, §10.4 `test_mcp_blocks_write_final_without_approval_audit` |
| INV-04 (sandbox escape impossible at MCP) | RESOLVED | §5.6 Gate 1, §10.4 `test_mcp_sandbox_blocks_even_with_approval` |
| INV-05 (cross-tenant vertical promotion data stripping) | OUT OF SCOPE for this spec; owned by Learning Architect; this spec does not store source corrections in vertical-scope SkillVersions |
| INV-06 (approval signal idempotency) | RESOLVED | §7.3 signal_id dedup, INV-A2 |
| UE-02 (idempotency on Temporal retry) | RESOLVED | §3.4 idempotency keys, §5.1 idempotency log |
| UE-03 (replay determinism) | RESOLVED | §4.3 four commitments, §10.3 |
| UE-04 (AuditEvent mutability) | OUT OF SCOPE for this spec; Learning Architect owns the table; this spec writes via `EmitAuditEvent` and `WriteApprovalAuditEvent` activities and trusts the DB-level enforcement (per `03-correction-loop.md` §11.5) |
| UE-07 (abandoned case definition) | RESOLVED | §7.2 `CaseAbandoned` terminal state, §10.7 SC-04 |
| Standing veto items (Section 6.4) | All five resolved or escalated to NEW-C9/C10 for cross-spec alignment |

---

## 16. Idempotency Key Inventory (RESOLVED-N6, R4 reconciled to ctrl-plane §17 SHA-256 derivation)

**RESOLVED-N6 (R4 yield):** Ctrl-plane R3 §17 made the SHA-256 derivation rule binding cross-team. This spec yields and adopts the SHA-256 form for keys whose dedup target is content-derivable, while preserving UUID v4 generation for keys whose dedup is identity-based (per ctrl-plane §17.1: `signal_id` is a UUID; `correction.idempotency_key` is a SHA-256 of components). The R3 section's framing ("UUID + UNIQUE suffices") is superseded.

**Reconciled inventory (column-by-column with ctrl-plane §17.2):**

| Layer | Key name | Derivation (per ctrl-plane §17) | Dedup target | Owner / Writer |
|---|---|---|---|---|
| Gateway → Temporal signal | `signal_id` | `sha256(tenant_id ‖ packet_id ‖ action_button ‖ raw_inbound_message_id)` per ctrl-plane §17.2 — replaces R3 "UUID v4" | Workflow-side `processed_signal_ids` set; second delivery is no-op (INV-A2) | Operator UX gateway derives; exec-plane workflow handler dedupes |
| Outbound packet | `packet_id` | `sha256(tenant_id ‖ case_run_id ‖ decision_point_id ‖ "packet")` per ctrl-plane §17.2 — derived by exec-plane's `SendApprovalPacket` activity | Gateway-side delivery dedup; UNIQUE on `outbound_queue` | Exec-plane `SendApprovalPacket` (R4 correction: ctrl-plane §17 names exec-plane as the deriver, not the gateway) |
| Gateway → Temporal signal idempotency | `gateway_idempotency_key` | `sha256(tenant_id ‖ signal_id ‖ "signal" ‖ attempt_number)` per ctrl-plane §17.2 | Redis with 24h TTL; passed through in envelope | Operator UX gateway |
| Provider-level inbound | `source_message_id` | Provider-supplied (not Victoria-derived); per ctrl-plane §17.2 | Gateway `inbound_dedup` 24h TTL | Operator UX gateway |
| Correction row | `corrections.idempotency_key` | `sha256(tenant_id ‖ decision_point_id ‖ packet_id ‖ action_button)` per ctrl-plane §17.2 — equals envelope.idempotency_key | `corrections.idempotency_key` UNIQUE in per-tenant DB; duplicate INSERT acknowledged as success | Exec-plane `PersistCorrection` activity (RESOLVED-C10 sole writer) |
| MCP tool call | `mcp_tool.idempotency_key` | `sha256(tenant_id ‖ case_run_id ‖ decision_point_id ‖ tool_name)` per ctrl-plane §17.2 — replaces R3 ad-hoc `{case_run_id}:{decision_point_id}:{tool_name}` form | MCP server `mcp_idempotency_log` row UNIQUE; TTL = case_run lifetime | Exec-plane Temporal worker (per activity); MCP server enforces |
| Audit event | `audit_event.idempotency_key` | `sha256(tenant_id ‖ event_type ‖ ref_id ‖ sequence_or_timestamp)` per ctrl-plane §17.2 | `audit_events.idempotency_key` UNIQUE (both local cache and control-plane authoritative store; ctrl-plane §9.3) | Exec-plane `EmitAuditEvent` / `WriteApprovalAuditEvent` activities |
| Replay trigger | `replay_run.idempotency_key` | `sha256(tenant_id ‖ original_case_run_id ‖ candidate_id_or_skill_version_id ‖ "replay")` per ctrl-plane §17.2 | Control-plane `replay_runs.idempotency_key` UNIQUE | Ctrl-plane Evaluation Service derives; exec-plane consumes via `POST /internal/replay` |
| Alert | `alert.idempotency_key` | `sha256(tenant_id ‖ code ‖ time_bucket)` per ctrl-plane §17.2 | Per-bucket UNIQUE (rolling) | Each alert emitter (exec-plane MCP servers, Temporal worker) |

**Migration from R3 keys.** The R3-era colon-delimited key forms in §5.1 ad-hoc references are superseded by the SHA-256 derivation above. §3.4's activity catalog now uses SHA-256 derivations consistent with this inventory. The component values are also logged alongside the hash in audit event payloads (per ctrl-plane §17.4 IDEMP_04) so the hash remains debuggable.

**Cross-system continuity (R4 reconciled):**
- The SHA-256 of `(tenant_id, decision_point_id, packet_id, action_button)` appears in: `corrections.idempotency_key` (UNIQUE), `audit_events.payload.idempotency_key` (for `correction_received` event), and the envelope's `idempotency_key` field (operator-ux §2.2 field 3). Three sites, one value.
- Components are logged alongside hashes per ctrl-plane IDEMP_04 — investigators can reconstruct the key without reverse-engineering.

**TDD invariants.** This spec consumes ctrl-plane's `IDEMP_01..04` invariants (§17.4); `ct_idempotency_cross_service_match` (§11) is the boundary contract test that asserts identical components produce identical keys regardless of which service computes them.

---

## 17. Pushed-Upstream Requirements (cumulative R3 + R4)

This section consolidates everything this spec requires from peer architects. Each row names the consumer's requirement, the producer who must adopt it, and where it lands.

### 17.1 To Learning Architect (`03-correction-loop.md`)

| # | Status | Requirement | Lands at |
|---|---|---|---|
| MCP-SNAPSHOT-1 | DELIVERED in R3 | `mcp_read_snapshot` added to `artifacts.artifact_type` enum | `03-correction-loop.md` §2.5 |
| MCP-SNAPSHOT-2 | DELIVERED in R3 | `mcp_audit_reader` role + `mcp_approval_events` view scoped to `event_type='approval_received' AND tenant_id=current_tenant` | `03-correction-loop.md` §11.5 (lines 1934-1942) |
| AUDIT-CANON-1 | DELIVERED in R3 | `approval_received`, `blocked_write_attempted`, `sandbox_escape_blocked`, `security_violation` added to event_type registry | `03-correction-loop.md` §11.3 |
| AUDIT-CANON-2 | DELIVERED in R3 | `audit_events.payload` for `approval_received` includes `signal_id`, `gateway_idempotency_key`, `packet_id`, `approved_at`, `approved_by` | `03-correction-loop.md` §11.3 payload schema |
| ~~AUDIT-OUTBOX~~ (R4 NEW) | RESOLVED in R5 | Closed: learning-loop §2.0 / §2.10 names `audit_events_outbox` as the canonical row in their topology with the trigger that allows only `drained = false → true`. | `03-correction-loop.md` §2.0, §2.10 |

### 17.2 To Operator UX Architect (`04-operator-experience.md`)

| # | Status | Requirement | Lands at |
|---|---|---|---|
| C13-FOLLOWUP-1 | DELIVERED (C13-R5) | Canonical 16-field `ApprovalSignalEnvelope` published in operator-ux §2.2 with `parser_method`/`parser_confidence` naming; R4's `correction_id`+`parse_status` reverted | `04-operator-experience.md` §2.2 |
| C13-FOLLOWUP-2 | DELIVERED in R3 | Tenant_id derived from `provider_number → tenant_id` binding only | `04-operator-experience.md` §4.9 |
| C13-RECONCILE (R4) | RESOLVED in this spec | `parser_*` envelope fields renamed at the boundary to `parse_*` columns by `PersistCorrection`; no upstream change needed | exec-plane §7.4 |

### 17.3 To Control Plane Architect (`01-control-plane.md`)

| # | Status | Requirement | Lands at |
|---|---|---|---|
| REPLAY-SCHED-1 | DELIVERED in R3 | `POST /admin/replays` → `POST /internal/replay` over mTLS | `01-control-plane.md` §8.7 |
| REPLAY-SCHED-2 | DELIVERED in R3 | Preconditions checked; ctrl-plane surfaces 4xx to reviewer | `01-control-plane.md` §8.7 |
| OQ8-FOLLOWUP | DELIVERED in R3 | Pull-at-start authoritative; push downgraded to non-authoritative cache hint | `00-overview.md` §2 A10 binding consensus |
| AUTH-MTLS (R4) | DELIVERED in R3 | All exec ↔ ctrl traffic over mTLS; cert SAN-based identity | `01-control-plane.md` §6.2 |
| ~~AUDIT-DRAIN~~ (R4) | RESOLVED in R5 | Closed by ctrl-plane §9.4 `/internal/audit/events` definition: drain semantics, batch acceptance, idempotent insert via SHA-256 keys (§17). | `01-control-plane.md` §9.4 |
| ~~TEMPORAL-IAM~~ (R4) | RESOLVED in R5 | Closed by ctrl-plane §6.8: path A (native IAM signal-only role) is default; path B (signal-proxy sidecar per operator-ux §2.3) is fallback. Both contract tests retained in §11. | `01-control-plane.md` §6.8 |

---

## 18. Implementation Handoff

This is the single-page handoff for engineering. Everything above is the contract; this section is the build map.

### 18.1 Build order (dependency-first)

Components within a tier may be built in parallel. Each tier depends on all previous tiers being deployable.

**Tier 1 — Provisioning prerequisites (consumed from peers; nothing exec-plane-side to build first).**
- Confirm ctrl-plane §5.6 manifest delivery contract is implemented and emits to `/hermes/config/manifest.json`.
- Confirm ctrl-plane §6.2 mTLS internal CA is operational; leaf certs issuable via `IssueServiceCertificates`.
- Confirm learning-loop §2.0 per-tenant DB migrations include `audit_events_outbox`, `mcp_idempotency_log`, `mcp_approval_events` view, and the `mcp_audit_reader` role grant.

**Tier 2 — Hermes container + MCP sidecars (independent processes per tenant).**
1. Hermes container image, version-pinned (§2.2), with the entrypoint that validates `manifest.tenant_id == volume.tenant_id_label` (INV-H4).
2. Sandbox MCP servers (`sandbox-email`, `sandbox-drive`, `sandbox-invoice`) — separate binaries per mode (§5 D3); tool manifests omit `write_final` tools when `MODE=sandbox` (or unset; INV-T5).
3. The `mcp_approval_events` Gate 2 read path against control-plane DB over mTLS (§5.6).

**Tier 3 — Temporal worker.**
1. Activity catalog implementation (§3.4): `ExtractEnquiryFacts`, `ClassifyEnquiryType`, `LoadSkillVersion`, `InvokeHermesReasoning`, `CreateEmailDraft`, `CreateDriveArtifact`, `CreateInvoiceDraft`, `SendApprovalPacket`, `SendEmailFinal`, `FinalizeInvoice`, `EmitAuditEvent`, `PersistCorrection`, `WriteApprovalAuditEvent`.
2. Workflow types (§3.3): `EnquiryTriageWorkflow`, `QuoteDraftingWorkflow`, `InvoiceHandlingWorkflow`.
3. Signal handler with `signal_id` dedup (INV-A2) and tenant-binding check (INV-A4).
4. `AuditDrainer` activity that drains `audit_events_outbox` to control-plane `/internal/audit/events` over mTLS (§14.4 sequence).

**Tier 4 — Internal RPC surface (§13.2).**
- `/internal/health`, `/internal/replay`, `/internal/audit/events/drain`, `/internal/skill-versions/invalidate` — all mTLS-only, SAN-validated.

### 18.2 Integration points with peers (one line each)

| Boundary | Peer | Spec ref |
|---|---|---|
| Provisioning manifest config-mount | Control plane | `01-control-plane.md` §5.6 |
| `LoadSkillVersion` resolver call | Control plane (resolver wraps learning-loop §2.8 store) | `01-control-plane.md` §13; `03-correction-loop.md` §9 |
| `mcp_approval_events` view + `mcp_audit_reader` role | Learning loop (schema) | `03-correction-loop.md` §11.5 |
| Audit upstream drain `/internal/audit/events` | Control plane | `01-control-plane.md` §9.4 |
| `ApprovalSignalEnvelope` ingestion (16 fields, C13-R5) | Operator UX (gateway) | `04-operator-experience.md` §2.2 |
| `SendApprovalPacket` outbound to gateway | Operator UX (gateway) | `04-operator-experience.md` §2.1 (ReviewPacket) |
| `POST /admin/replays` → `/internal/replay` | Control plane (Evaluation Service) | `01-control-plane.md` §8.7; this spec §13.3 |
| `emit-alert` from MCP / worker | Control plane (alert sink) | `01-control-plane.md` §9.6.1 |
| Temporal signal transport (path A / path B) | Operator UX (gateway) + Control plane (cred provisioning) | `01-control-plane.md` §6.8; `04-operator-experience.md` §2.3 |

### 18.3 Test fixtures owned by exec-plane

- **Sandbox case fixture set** (§6.2): `fixtures/{workflow_type}/{vertical}/*.json` — every fixture has `sandbox: true` (INV-S1). Seeded into per-tenant DB at provisioning.
- **Replay snapshot fixtures** (§4.3 commitment 3): MCP read-tool outputs stored on the original run as `artifact_type='mcp_read_snapshot'` rows; replay loads from these.
- **Property-test seed corpus** (§10.8): adversarial fixtures for `prop_sandbox_mode_no_external_invocation` (base64 / Unicode / system-prompt-injection patterns).

Test fixtures **consumed from peers** (not owned by exec-plane):
- Operator-ux R4 §2.2 — canonical `ApprovalSignalEnvelope` test fixtures.
- Learning-loop §9.1 — `SkillVersion` test manifests with mixed-scope rules.
- Ctrl-plane §5.6 — provisioning manifest test fixtures.

### 18.4 Pre-commit invariants the build pipeline MUST enforce

| Invariant | CI check |
|---|---|
| INV-H6 | Snapshot `/hermes/data/{tenant_id}/skills/` at bootstrap; assert no rule content written post-run (`test_no_rule_content_on_hermes_volume`) |
| INV-T5 | Workflow input lacking `mode` rejects start (`test_mode_absent_defaults_deny`); `mode: true` / `mode: 0` / unrecognized values rejected (`test_mode_canonical_enum_only`) |
| INV-T3 + §5.6 | Static analysis: no `SideEffectClass.WRITE_FINAL` activity registered when `HERMES_MODE=sandbox`; every `write_final` MCP tool has the 3-gate preflight in its handler |
| INV-A1 + INV-A5 | `WriteApprovalAuditEvent` precedes `write_final` activity in workflow code (Temporal history ordering test); `PersistCorrection` cannot fetch from gateway DB (no client import) |
| INV-MCP1..3 | MCP server boots with `TENANT_ID` env var; every request validates `X-Victoria-Tenant-Id == TENANT_ID`; `tenant_id` body field matches header |
| INV-RPC1..4 | All `/internal/*` listeners require mTLS; cert SAN matches expected service identity per direction (§13.1 table); no plaintext fallback |
| INV-CR1..3 | `WorkflowID == case_run_id` on every `StartWorkflow`; replays create new CaseRun (never `ResetWorkflow`); `mode` immutable within a run |
| Idempotency-key derivation | All idempotency keys conform to ctrl-plane §17.1 SHA-256 derivation; component values logged alongside hash (IDEMP_04) |
| Field-name parity (C14) | `applicable_rules[]` block fed to Hermes preserves `conditions_canonical` / `recommended_action` field names byte-identical to `LoadSkillVersion` response (`test_skill_manifest_passthrough_field_parity`) |
| Audit-event registry conformance | Every `audit_events.event_type` written by exec-plane is in learning-loop §11.3 registry with this spec named as writer; no out-of-registry events emitted |

### 18.5 Components NOT to build (out of scope, owned by peers)

- `validated_rules` storage and resolver — **control plane** (`01-control-plane.md` §13).
- `corrections` Stage B parser (canonicalization, candidate match) — **learning loop** (`03-correction-loop.md` §3.6, §4).
- `audit_events` authoritative store, immutability triggers, query API — **learning loop / control plane** (`03-correction-loop.md` §11; `01-control-plane.md` §9.3).
- `RuleCandidate` lifecycle, confidence scoring, promotion pipeline — **learning loop** + **control plane Rule Review Console**.
- Operator messaging gateway (WhatsApp bridge, parser, follow-up state) — **operator UX** (`04-operator-experience.md`).
- OTP / JWT issuance for operators, gateway Temporal credential provisioning — **control plane** (`01-control-plane.md` §6.7, §6.8).

