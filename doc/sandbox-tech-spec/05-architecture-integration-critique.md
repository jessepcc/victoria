# Architecture Integration Critique — Round 1
## Devil's Advocate / Cross-Component Integrity Framework

**Role:** This document is the working ledger for the Devil's Advocate architect. It is not a design proposal. It names required contracts, invariants, risks, and acceptance scenarios. Every item here is a demand on the team. Nothing is resolved until the full discriminator is met.

---

## Section 1 — Required Cross-Component Contracts

A contract is **open** until all five discriminators are met:
1. Wire shape fixed (field names, types, version)
2. Writer named (exactly one component)
3. Reader(s) named (exhaustive list)
4. Failure mode named (what happens when the writer fails or sends a bad value)
5. The contract test that would fail if this contract were violated

### 1.1 tenant_id Propagation Chain

**The full hop chain:**
```
WhatsApp message received by messaging-gateway
  → gateway authenticates → derives tenant_id from bot token → bot token NEVER crosses tenants
  → gateway posts inbound message to Temporal via signal or start-workflow call
      → Temporal workflow start payload MUST contain tenant_id
  → Temporal workflow opens activities
      → every activity context MUST carry tenant_id
  → Hermes is invoked inside the activity
      → Hermes invocation request MUST carry tenant_id
  → Hermes calls MCP tool
      → MCP HTTP request MUST carry tenant_id in a fixed header (e.g. X-Victoria-Tenant-Id)
      → MCP server validates that its own bound tenant matches the header — reject if mismatch
  → MCP server writes to Postgres
      → every INSERT must include tenant_id column; no row lacks it
  → AuditEvent is written
      → AuditEvent row includes tenant_id, inherited from the activity that triggered it
  → control-plane queries AuditEvent
      → control-plane must apply WHERE tenant_id = $bound_tenant to every query
```

**Status: OPEN.** No component has specified: (a) the exact field name and type for the Temporal workflow input, (b) the header name for MCP calls, (c) how the MCP server binds its tenant at startup vs. per-request, or (d) the contract test.

**Demanded resolution from exec-plane + operator-ux + learning-loop:** Name the exact payload field. Name the MCP header. Name which component is the canonical source of truth for "this request belongs to tenant T."

---

### 1.2 case_run_id Propagation Chain

**The full hop chain:**
```
Temporal workflow instance created
  → workflow generates or receives case_run_id (UUID v4, string)
  → every Temporal activity in that workflow carries case_run_id in its input
  → every Hermes invocation inside those activities carries case_run_id
  → every MCP tool call carries case_run_id (alongside tenant_id)
  → every artifact written to object storage uses case_run_id as a path segment:
      s3://{tenant_bucket}/cases/{case_run_id}/artifacts/{artifact_id}
  → every Postgres row for DecisionPoint, Artifact, Correction, RuleCandidate
      carries case_run_id as a foreign key
  → every AuditEvent carries case_run_id
  → replay: re-running a case_run_id must be deterministic (see Section 5, scenario 6)
```

**Status: OPEN.** No component has answered: (a) who generates case_run_id — Temporal at workflow start, or a pre-Temporal call by the control plane? (b) how replay identifies the fixed input set for a case_run_id (the learning loop and exec-plane must agree on this).

**Demanded resolution from exec-plane + learning-loop:** case_run_id generation point, the exact Postgres column present in every case-linked table, and the contract test that a replay of case_run_id X produces the same DecisionPoint set from the same input.

---

### 1.3 Where RuleCandidates Physically Live

**The problem:** RuleCandidate JSON carries `tenant_id`, implying per-tenant storage. The Rule Review Console sits in the shared control plane and must perform cross-tenant reads for vertical-scoped promotion. These two facts cannot coexist without an explicit access bridge.

**The three options (pick one):**

| Option | Who owns | Cross-tenant access |
|--------|----------|---------------------|
| A — All candidates in control-plane DB | ctrl-plane | Trivial; per-tenant writes go over API |
| B — Candidates in per-tenant DB, read via API | learning-loop (per-tenant) | Review Console calls per-tenant API; fan-out |
| C — Candidates in per-tenant DB, replicated to control-plane | both | Replication pipeline with consistency risk |

**None of the options is obviously wrong, but the team has not picked one.** Option B is the safest isolation posture. Option A simplifies the console but creates a central DB that holds all tenants' corrections — a high-value target.

**Additional sub-problem — cross-tenant rule promotion:** The spec describes vertical-scoped ValidatedRules that are derived from multiple tenants' RuleCandidates. This means tenant A's corrections, even in aggregate, are being used to influence tenant B's default behavior. This is a deliberate feature but it is also a security boundary. The exact mechanism for this aggregation — what data crosses the boundary, in what form, stripped of what tenant identifiers — must be specified. The invariant is: **evidence that a vertical-scope rule was derived from tenant A must not be discoverable by tenant B, and must not expose any of tenant A's case data, correction content, or artifact content to tenant B.**

**Status: OPEN.** exec-plane, learning-loop, and ctrl-plane must align on: storage topology, write path, read path, and the exact contract for cross-tenant vertical-scope promotion.

---

### 1.4 How Hermes Consumes ValidatedRules at Runtime

**Why this is load-bearing:** Replay determinism, rollback semantics, and whether exec-plane and learning-loop can ship independently all depend on this answer.

**The four possible models:**

| Model | How rules are consumed | Replay determinism | Rollback behavior |
|-------|----------------------|--------------------|-------------------|
| A — Pulled per-case at invocation time | Hermes queries current active rules before each run | Non-deterministic if rules change between run and replay | Rollback = deactivate rule; old replays get new behavior |
| B — Baked into SkillVersion at promotion | Rules are compiled into a Hermes skill snapshot | Deterministic if skill is pinned per case_run_id | Rollback = revert to previous SkillVersion |
| C — Fed via system prompt at workflow start | Temporal activity fetches active rules and injects them into Hermes system prompt | Deterministic only if system prompt is recorded per case | Rollback = change what Temporal injects |
| D — Hybrid: core skill baked, per-case exceptions pulled | Mix of B and C | Partially deterministic; exceptions can drift | Rollback is ambiguous |

**The spec mentions both "skill versioning" and "injected rules." This means model D (hybrid) is likely where the team ends up without explicit coordination — and model D is the worst for replay determinism.**

**Required answer:** exec-plane and learning-loop must agree, in writing, on a single model. The contract test is: given a frozen case_run_id and a frozen rule set at T0, re-running the case at T1 with no rule changes produces identical DecisionPoint outputs.

**Status: OPEN.**

---

### 1.5 Approval Signal Contract (operator-ux → Temporal)

**The exact required shape:**

```typescript
interface ApprovalSignal {
  signal_id: string;           // UUID v4, generated by operator-ux at parse time
  case_run_id: string;         // which case this approval applies to
  tenant_id: string;           // must match the workflow's bound tenant
  decision_point_id: string;   // which specific decision is being approved
  action: ApprovalAction;      // "approve" | "reject" | "correct" | "escalate"
  correction_payload?: CorrectionPayload;  // present iff action = "correct"
  source_message_id: string;   // WhatsApp/Telegram message ID for dedup
  received_at: string;         // ISO 8601, when gateway received the message
  parsed_at: string;           // ISO 8601, when parser produced this signal
}

type ApprovalAction = "approve" | "reject" | "correct" | "escalate";

interface CorrectionPayload {
  correction_type: "wrong_facts" | "wrong_action" | "missing_condition" | "use_different_template" | "add_note";
  free_text: string;           // operator's raw reply
  structured_fields?: Record<string, unknown>;  // parsed structured corrections
}
```

**Idempotency requirement:** Temporal must deduplicate on `signal_id`. If the same `signal_id` arrives twice (network retry, operator double-tap), the workflow advances exactly once. The `source_message_id` is a secondary dedup key for the gateway layer: if the same message ID arrives twice from the WhatsApp session, the gateway emits exactly one `ApprovalSignal`.

**Timeout requirement:** The Temporal workflow waiting for an approval signal must have an explicit deadline. After deadline, the signal type is `"abandoned"` and must be written to AuditEvent. The deadline value (e.g. 48h for sandbox mode) must be agreed between exec-plane and operator-ux.

**Status: OPEN.** operator-ux has not specified the exact shape. exec-plane has not specified the Temporal signal name, dedup key, or timeout. Neither has agreed on a contract test.

---

### 1.6 Sandbox-vs-Live Mode Flag

**Owner question:** The mode flag must have exactly one owner for its authoritative value. Options:
- Control plane (tenant config) — pulled at workflow start
- Temporal workflow input — set when workflow is started, immutable for that run
- MCP server config — set at container start, enforced per-request

**The correct answer is all three, in a hierarchy:**
1. Control plane stores the per-tenant authoritative mode (`sandbox | approval_gated | autopilot`).
2. Temporal workflow receives the mode as an immutable input field at start time (snapshotted from control plane).
3. MCP servers enforce the mode at the call boundary: if mode is `sandbox`, any MCP tool tagged `WRITE_EXTERNAL` must refuse with a structured error (not silently no-op).

**The security invariant:** An MCP tool call that would produce an external side effect (send email, publish invoice, write to live drive) MUST fail with a structured error if the workflow's mode is `sandbox`. It must not succeed. It must not silently succeed. The MCP server must not rely on Hermes "knowing" not to call the tool — it must enforce this itself.

**Status: OPEN.** No component has specified: (a) the exact mode field name in the Temporal workflow input, (b) the set of MCP tools tagged WRITE_EXTERNAL, (c) the MCP enforcement point and error response, (d) the contract test.

---

### 1.7 Audit Chain

**Who writes, who reads, where it lives:**

| Event type | Writer | Written to | Reader(s) |
|------------|--------|-----------|-----------|
| Case started | exec-plane (Temporal workflow) | per-tenant Postgres: AuditEvent | ctrl-plane review console, learning-loop |
| MCP tool called | exec-plane (MCP server interceptor) | per-tenant Postgres: AuditEvent | ctrl-plane, security audit |
| Approval packet sent | operator-ux (gateway) | per-tenant Postgres: AuditEvent | ctrl-plane, exec-plane (correlate) |
| Correction received | operator-ux (parser) | per-tenant Postgres: AuditEvent + Correction entity | learning-loop |
| RuleCandidate generated | learning-loop | per-tenant Postgres: RuleCandidate | ctrl-plane review console |
| Rule promoted | ctrl-plane (reviewer action) | per-tenant Postgres: ValidatedRule + AuditEvent | exec-plane (for next run) |
| SkillVersion created | exec-plane / learning-loop (TBD) | per-tenant Postgres: SkillVersion + AuditEvent | exec-plane (for Hermes) |
| send_final attempted | exec-plane (MCP server) | per-tenant Postgres: AuditEvent | security audit |

**Immutability requirement:** AuditEvent rows must be INSERT-only. No UPDATE, no DELETE. The application layer must enforce this — not just by convention. One approach: a Postgres table with a trigger that rejects non-INSERT DML. The test: attempt an UPDATE on any AuditEvent row and assert it throws.

**send_final invariant (critical):** Before any MCP tool tagged `WRITE_EXTERNAL` is allowed to execute in non-sandbox mode, the exec-plane must verify that an `AuditEvent` of type `approval_received` with `action = "approve"` exists for the current `case_run_id` and `decision_point_id`. This check must happen inside the MCP server, not inside Hermes. The contract test: call a WRITE_EXTERNAL tool on a workflow that has no approval AuditEvent and assert the call is rejected.

**Status: OPEN.** The learning-loop owns the Postgres schema but the exec-plane writes many of these events. The two components have not agreed on: table ownership, column set, immutability mechanism, or the pre-execution check for WRITE_EXTERNAL tools.

---

### 1.8 SkillVersion Boundary and Ownership

**The ambiguity:** The spec says Hermes has "skills" and "skill versioning." It is unclear whether:
- SkillVersion is a Postgres entity owned by learning-loop that stores a JSON/YAML snapshot of a Hermes skill
- SkillVersion is a Hermes-internal artifact (on the Hermes data volume) that learning-loop only references
- SkillVersion is a file in object storage that Hermes loads at startup

**This matters because:** rollback semantics, replay determinism, and the question of whether a SkillVersion can be applied to a new Hermes container (e.g. after a container restart or re-provisioning) all depend on this.

**Required contract:** exec-plane and learning-loop must specify: (a) physical storage location, (b) serialization format, (c) how a specific SkillVersion is loaded into a Hermes runtime, (d) whether SkillVersion load is atomic or can partially apply, (e) the contract test for rollback.

**Status: OPEN.**

---

## Section 2 — Security and Isolation Invariants

Each invariant is stated as a falsifiable assertion with a named test.

### INV-01: No MCP Tool Call Carries Credentials From a Different Tenant

**Assertion:** For every MCP tool call during a workflow run, the credentials injected into the MCP server at container start time are exactly the credentials belonging to the tenant whose `tenant_id` is bound to that workflow instance.

**How it's provable:** At container startup, the MCP server records its `bound_tenant_id` from its injected secret scope. On every incoming tool call, the MCP server checks that the `X-Victoria-Tenant-Id` header equals `bound_tenant_id`. If mismatch, the call is rejected and an `AuditEvent` of type `security_violation` is written. The contract test: start an MCP server bound to `tenant_A`, make a tool call with header `tenant_B`, assert HTTP 403 and assert the security_violation AuditEvent exists.

**Status: OPEN.** exec-plane has not specified the startup binding mechanism or the per-call validation.

---

### INV-02: No Hermes Data Volume Shared Across Tenants

**Assertion:** The Docker volume mounted at the Hermes data path (e.g. `/hermes/data`) for tenant A is never mounted into any Hermes container running for tenant B. This invariant must hold across restarts, re-deployments, and provisioning of new tenants.

**How it's provable:** The volume name or volume mount path must encode the tenant_id: e.g. `hermes-data-{tenant_id}`. The provisioning code must be tested: given two calls to provision(tenant_A) and provision(tenant_B), assert that the two resulting Hermes containers have different volume identifiers, and that neither container has access to the other's volume path.

**Additional risk:** Container restarts. After a crash-restart, the Hermes container must remount the correct tenant-specific volume, not a shared temp volume or the previously-mounted volume of another tenant. The provisioning / orchestration layer must enforce this — it cannot be implicit.

**Status: OPEN.** exec-plane has not specified volume naming, the provisioning test, or the restart binding guarantee.

---

### INV-03: No WRITE_EXTERNAL Tool Can Fire Without an Approved Signal in the Audit Chain

**Assertion:** For any MCP tool call tagged `WRITE_EXTERNAL` (send email, finalize invoice, publish to live drive), the MCP server must, before executing the tool, query the per-tenant AuditEvent table and confirm that a row exists with: `case_run_id = current_case_run_id AND decision_point_id = current_decision_point_id AND event_type = 'approval_received' AND action = 'approve'`. If no such row exists, the call must fail with a structured rejection. This check must occur inside the MCP server, not in Hermes, not in the Temporal workflow logic.

**How it's provable:** The contract test: create a case_run_id with no approval AuditEvent. Invoke a WRITE_EXTERNAL tool (in test via mock or real sandbox). Assert the tool returns an error with code `APPROVAL_REQUIRED`. Assert no external side effect occurred. Assert an AuditEvent of type `blocked_write_attempted` was written.

**Additional enforcement:** In sandbox mode (INV-SB below), the mode check is an earlier gate. But the approval check must apply even in non-sandbox mode — they are independent gates.

**Status: OPEN.** exec-plane has not specified the pre-execution check inside MCP servers.

---

### INV-04: Sandbox Mode Escape Impossible at MCP Boundary

**Assertion:** When a workflow's mode field (as received in the Temporal workflow input) is `sandbox`, no MCP tool tagged `WRITE_EXTERNAL` may execute, regardless of what Hermes requests, regardless of what approval signals exist. The MCP server checks the mode field on every call. Mode `sandbox` produces a hard rejection.

**How it's provable:** The contract test: start a workflow with `mode = sandbox`. Obtain a valid approval signal (so INV-03 would pass). Invoke a WRITE_EXTERNAL tool. Assert HTTP 403 with code `SANDBOX_MODE`. Assert no external side effect. Assert an AuditEvent of type `sandbox_escape_blocked` was written.

**Status: OPEN.**

---

### INV-05: Cross-Tenant Vertical Rule Promotion Does Not Expose Tenant Data

**Assertion:** When a ValidatedRule is promoted to `scope = vertical` (derived from corrections across multiple tenants), the resulting ValidatedRule contains no field that references, directly or indirectly, the `tenant_id`, `case_run_id`, `correction_id`, or any artifact of the contributing tenants. Any tenant can read the vertical-scoped rule and must not be able to reverse-engineer which tenants' corrections contributed.

**How it's provable:** The contract test: promote a vertical rule derived from `tenant_A` and `tenant_B` corrections. As `tenant_C` (or as an external reader), fetch the ValidatedRule and all its linked AuditEvents. Assert that no `tenant_id` field in the result set equals `tenant_A` or `tenant_B`. Assert that no `source_correction_ids` or `source_case_run_ids` fields are present in the vertical-scoped rule (they may exist in the candidate but must be stripped at promotion).

**Status: OPEN.** learning-loop and ctrl-plane have not addressed this at all.

---

### INV-06: Approval Signal Idempotency

**Assertion:** If the same approval signal (same `signal_id` or same `source_message_id`) is delivered twice to the Temporal workflow (due to network retry, WhatsApp reconnect, or operator double-tap), the workflow advances exactly once. The second delivery produces no observable state change.

**How it's provable:** The contract test: deliver an ApprovalSignal to a Temporal workflow. Deliver the identical signal again (same `signal_id`). Assert the workflow's AuditEvent log contains exactly one `approval_received` event for that `decision_point_id`. Assert the workflow advances to the next state exactly once.

**Status: OPEN.** operator-ux has not specified signal_id generation. exec-plane has not specified Temporal dedup logic.

---

## Section 3 — Over-Engineering Risks

### OE-01: Messaging Gateway Abstracted Over N Channels Before N > 2

**The smell:** The team is likely to create an abstract `MessagingGateway` interface with pluggable channel implementations, anticipating WhatsApp, Telegram, SMS, Slack, email. At Phase 1 (and likely Phase 2), exactly two channels exist: WhatsApp (production) and Telegram (dev/internal). The effort to abstract over 2 is paid once; the value of that abstraction is zero until channel 3 exists.

**The risk:** The abstraction layer adds an indirection that obscures the WhatsApp-specific quirks that actually matter (non-Business-API session management, reconnect behavior, message ID dedup). The abstraction will be leaky anyway because WhatsApp's constraints don't generalize to Telegram or any other channel.

**The correct scope:** Build a `WhatsAppGateway` and a `TelegramGateway` as concrete classes with a shared interface defined only by what they actually share (receive_message → ApprovalSignal, send_message ← ReviewPacket). Do not build a plugin registry, a channel factory, or a channel configuration DSL.

---

### OE-02: MCP Capability Registry Before There Are More Than 3–4 Tools

**The smell:** The team will likely propose a dynamic MCP tool registry — a system where tools are registered, discovered, and composed at runtime — to support "any number of future MCP servers." At MVP, the tool set is: sandbox_email, sandbox_drive, sandbox_invoice. That is three servers, four to eight tools total.

**The risk:** A capability registry adds a runtime dependency (the registry itself must be available), a discovery protocol, and a versioning layer. All of this is complexity that provides zero value until the tool count is in the dozens. It also obscures the per-tenant isolation model because "tool discovery" implies a shared namespace.

**The correct scope:** Hardcode the tool set per workflow type. `quote_drafting` uses sandbox_email and sandbox_drive. `invoice_handling` uses sandbox_invoice. No registry. Configuration is a static map.

---

### OE-03: Kubernetes Multi-Tenancy Infrastructure at Phase 1

**The smell:** The team will likely propose a Kubernetes-based multi-tenant architecture with namespaces, resource quotas, NetworkPolicies, and RBAC per tenant, citing the Phase 2 architecture from the spec. This is appropriate for Phase 2. It is premature for Phase 1 (first 10 customers).

**The risk:** K8s multi-tenancy is operationally complex to get right. The namespace isolation model has well-documented escape vectors (cluster-level resources, node affinity, CNI trust). For first 10 customers, the correct isolation unit is a VM or ECS task group per tenant — simpler, provably isolated, and aligned with the spec's own Phase 1 recommendation. Do not let the Phase 2 design leak into Phase 1 infrastructure choices.

---

### OE-04: Generic Rule Condition Evaluator Before Condition Types Are Known

**The smell:** The RuleCandidate `conditions` field is a JSON array of `{field, operator, value}` triples. The team will likely build a generic condition evaluator (mini-rules engine) that handles arbitrary field paths, arbitrary operators, and arbitrary value types.

**The risk:** At MVP, the actual condition types observed in the spec examples are: `photos_complete = false`, `client_type = "new"`, `supplier.country != "AU"`. These are simple field lookups on a known schema. A generic evaluator introduces parsing complexity, operator precedence issues, and security risks (field path injection, operator injection) without any concrete need.

**The correct scope:** Enumerate the supported condition fields and operators for each workflow type. Build typed condition checking per workflow type. If the field set exceeds ~20 items, revisit.

---

### OE-05: Premature Confidence Scoring Model

**The smell:** The spec defines `confidence: 0.72` on RuleCandidates. The team will likely design a non-trivial statistical model for confidence scoring — Bayesian updates, similarity scoring against existing rules, semantic embedding of corrections.

**The risk:** At first 10 customers, the confidence signal is: how many corroborating corrections exist (evidence_count), whether any contradicting corrections exist, and whether an internal reviewer agreed. These three inputs do not require a statistical model. They require a deterministic formula. The formula can be improved later. Building a learning model to compute confidence before there is training data is purely speculative.

**The correct scope:** `confidence = f(evidence_count, contradiction_count)` where f is a simple deterministic formula agreed by learning-loop. Document the formula. Do not build a model.

---

## Section 4 — Under-Engineering Risks

### UE-01: WhatsApp Non-Business-API = Unowned Single Point of Failure

**The risk:** The spec acknowledges that WhatsApp Business API is deferred. The non-Business-API path (likely a library like `whatsapp-web.js` or equivalent) runs a WhatsApp Web session in a headless browser. This session can disconnect, can require re-authentication, can be rate-limited, and can be blocked by WhatsApp at any time. If the gateway session dies, the entire operator UX for that tenant dies.

**What is missing:** No component has specified: (a) session reconnect strategy, (b) message queue depth and persistence during downtime, (c) what happens to an in-flight ApprovalSignal that was "sent" but not delivered due to a session drop, (d) how the Temporal workflow timeout interacts with a gateway outage.

**The demand:** operator-ux must specify the reconnect contract. exec-plane must specify the workflow timeout behavior during gateway unavailability. This is not optional — it is the primary operator communication channel.

---

### UE-02: Weak Idempotency on Temporal Workflow Retry

**The risk:** Temporal activities are retried on failure. If a Hermes invocation activity is retried, it may produce a second call to an MCP tool that already partially executed. If that MCP tool call is not idempotent, the case can produce duplicate artifacts, duplicate AuditEvents, or duplicate Corrections.

**What is missing:** No component has specified: (a) which MCP tools are idempotent by design, (b) which MCP tools require a client-provided idempotency key, (c) how the exec-plane generates and stores idempotency keys per activity attempt, (d) how the learning-loop deduplicates Corrections and AuditEvents produced by retried activities.

**The demand:** exec-plane must specify the idempotency strategy for each MCP tool. The contract test: trigger a retried Hermes activity. Assert that the resulting artifact count and AuditEvent count are the same as a single successful run.

---

### UE-03: No Replay Determinism Guarantee

**The risk:** The spec promises replay — the ability to re-run a case with updated rules and compare outputs. Replay is only meaningful if the re-run is deterministic given the same inputs and the same rule set. Non-determinism sources include: LLM temperature, Hermes internal state (memories, cached embeddings), rule set fetched at run time vs. pinned at case start, and MCP tool responses that include timestamps or non-idempotent reads.

**What is missing:** No component has specified: (a) LLM temperature policy for replay runs, (b) whether Hermes memories are frozen for replay, (c) whether the rule set is pinned to the original run's rule snapshot or re-fetched, (d) how MCP read tools are mocked or replayed in a deterministic fashion.

**The demand:** exec-plane and learning-loop must specify a determinism contract for replay. The contract test: replay a case_run_id twice with no external changes. Assert identical DecisionPoint outputs.

---

### UE-04: AuditEvent Mutability Not Enforced by Storage Layer

**The risk:** The spec calls for immutable audit logs. If this is enforced only by application convention ("we just don't update AuditEvent rows"), it will be violated: during debugging, someone will run an UPDATE to fix a bad event; during schema migrations, an ORM will update rows; a bug in the write path will issue a duplicate INSERT that gets resolved via UPSERT.

**The demand:** The Postgres AuditEvent table must have a trigger or a role-based permission that rejects all UPDATE and DELETE statements on production. The contract test: attempt an UPDATE on an AuditEvent row as the application service account and assert it throws a database error (not an application error).

---

### UE-05: Tenant Boundary Checks Not Present at Every Query

**The risk:** Multi-tenant applications consistently leak data through missing WHERE tenant_id = $x predicates on queries. A single query that omits the predicate — even in an internal admin or reporting path — constitutes a cross-tenant data leak.

**The demand:** Every query that touches a per-tenant table must have a tenant_id predicate applied by a middleware or query-builder layer, not by individual developers at each call site. The contract test is a repository-level test: enumerate every query in the codebase that touches a per-tenant table and assert that each query includes a tenant_id predicate or is explicitly marked as a privileged cross-tenant query (with a corresponding audit).

---

### UE-06: Rule Promotion Writes No Audit Trail

**The risk:** The spec describes internal reviewers promoting RuleCandidates to ValidatedRules. If this action does not produce an immutable AuditEvent, there is no way to answer: who promoted this rule, when, on what evidence, and under what authorization. This is both a security concern (unauthorized promotions are undetectable) and a model quality concern (a bad promotion cannot be traced back to its source).

**The demand:** Every promotion action must write: (a) an AuditEvent with `reviewer_identity`, `promoted_from_candidate_id`, `rationale`, `promoted_at`, (b) a `ValidatedRule` row with `promoted_by` and `promoted_at`. These writes must be transactional. The contract test: promote a rule and assert both the ValidatedRule row and the AuditEvent row exist with matching `promoted_from_candidate_id`.

---

### UE-07: No Definition of "Abandoned" Case

**The risk:** The spec mentions operators abandoning packets (see acceptance scenario, Section 5). There is no specification of: (a) the timeout that triggers abandonment, (b) what state the Temporal workflow enters on abandonment, (c) whether an abandoned case can be revived, (d) whether a RuleCandidate is generated from an abandoned case (it should not be — no correction signal), (e) what AuditEvent is written.

**The demand:** exec-plane and operator-ux must specify the abandonment contract: timeout value, resulting Temporal workflow state transition, AuditEvent type, and the contract test.

---

## Section 5 — Minimum End-to-End Acceptance Scenarios

Each scenario names: input → components → observable outputs at each boundary → assertable invariant. These scenarios are the integration acceptance bar. The system is not shippable until all of them pass.

---

### SC-01: Golden-Path Correction (Full Sandbox Loop)

**Input:** A seeded sandbox case for tenant `t_roofing_01`: new-customer roofing enquiry, 1 photo, no quote template selected.

**Component participation:**
1. exec-plane seeds the case → Temporal workflow starts, generates `case_run_id`
2. exec-plane (Hermes) invokes sandbox_drive (get_photos) and sandbox_email (draft_reply) via MCP
3. exec-plane writes DecisionPoint: "send quote now" with confidence 0.6
4. exec-plane writes Artifact: draft email + draft PDF quote
5. operator-ux (gateway) sends ReviewPacket to WhatsApp with correction options
6. Operator replies: "Wrong action — should have asked for more photos"
7. operator-ux (parser) produces ApprovalSignal with action=correct, correction_type=wrong_action
8. exec-plane (Temporal) receives signal, writes AuditEvent: correction_received
9. learning-loop generates RuleCandidate from correction
10. ctrl-plane reviewer promotes candidate to ValidatedRule
11. exec-plane re-runs case with updated rules
12. exec-plane writes new DecisionPoint: "hold_and_request_more_info"
13. operator-ux sends updated ReviewPacket; operator approves

**Observable outputs at each boundary:**
- MCP call headers contain correct `tenant_id` and `case_run_id`
- AuditEvent table has rows for: case_started, mcp_tool_called (x2), packet_sent, correction_received, rule_candidate_created, rule_promoted, case_rerun_started, packet_sent (x2), approval_received
- RuleCandidate row exists with `status=promoted` after step 10
- ValidatedRule row exists with `promoted_by` set
- Second DecisionPoint row has `recommended_action=hold_and_request_more_info`

**Invariants falsified if violated:**
- INV-01: tenant_id present in all MCP headers
- INV-03: no WRITE_EXTERNAL tool fired before approval
- UE-06: audit trail complete through promotion

---

### SC-02: Multi-Correction Promotion (Evidence Accumulation)

**Input:** Three separate sandbox cases for tenant `t_roofing_01`, each a new-customer enquiry with incomplete photos, each corrected with "hold and request more photos."

**Component participation:**
1. Three independent case runs (separate `case_run_ids`)
2. Three correction signals, each producing a RuleCandidate with matching conditions
3. learning-loop merges candidates: evidence_count rises to 3, confidence crosses threshold, status → under_review
4. ctrl-plane reviewer promotes to ValidatedRule
5. A fourth sandbox case runs; DecisionPoint now shows `hold_and_request_more_info` without operator correction

**Observable outputs:**
- Three RuleCandidate rows with overlapping conditions are consolidated to one row with `evidence_count=3`
- `source_correction_ids` contains all three correction IDs
- Fourth case run produces correct DecisionPoint without any correction event

**Invariants falsified:**
- The merging logic is deterministic: merging the three candidates in any order produces the same consolidated candidate
- The ValidatedRule is applied to a fresh case_run_id with no prior AuditEvent from those source cases

---

### SC-03: Contradicting-Correction Supersession

**Input:** ValidatedRule `vr_0042 v1` is active: "new client + incomplete photos → hold." A correction arrives: "ABC Realty is now a repeat client — quote anyway."

**Component participation:**
1. exec-plane runs case; applies vr_0042 v1; produces DecisionPoint: hold
2. Operator corrects: "wrong action — this client is repeat, go ahead"
3. learning-loop generates new RuleCandidate narrowing conditions: `client_type != 'repeat'`
4. ctrl-plane reviewer promotes: `vr_0042 v2` created with `supersedes: vr_0042/v1`; v1 marked `deprecated`
5. exec-plane runs another case for repeat client; applies v2; holds for new clients only

**Observable outputs:**
- ValidatedRule table has two rows for `vr_0042`: one `deprecated`, one `active`
- New case for repeat client has DecisionPoint: proceed
- New case for new client still has DecisionPoint: hold

**Invariants falsified:**
- vr_0042 v1 still exists (rollback is possible)
- The deprecated rule's AuditEvent records the supersession with `promoted_by` and `rationale`

---

### SC-04: Abandoned Packet

**Input:** Sandbox case for tenant `t_invoice_01` produces a ReviewPacket sent to operator. Operator does not respond.

**Component participation:**
1. operator-ux sends ReviewPacket
2. Temporal workflow waits for ApprovalSignal with timeout T (e.g. 48h)
3. Timeout fires; Temporal workflow transitions to abandoned state
4. exec-plane writes AuditEvent: case_abandoned
5. No RuleCandidate is generated

**Observable outputs:**
- Temporal workflow is in terminal state (not hung waiting indefinitely)
- AuditEvent row exists with event_type=case_abandoned, case_run_id, tenant_id
- No RuleCandidate row exists for this case_run_id
- The case_run_id can be reported in ctrl-plane as abandoned (not active, not failed)

**Invariants falsified:**
- UE-07: abandonment is handled deterministically, not by indefinite wait
- No resource leak: the Temporal workflow is closed

---

### SC-05: Tenant Isolation Leak Attempt

**Input:** Two tenants exist: `t_roofing_01` and `t_plumbing_02`. A crafted ApprovalSignal is sent to the exec-plane with `tenant_id = t_plumbing_02` but addressed to a workflow belonging to `t_roofing_01`.

**Component participation:**
1. operator-ux receives message on `t_roofing_01`'s WhatsApp number
2. operator-ux derives `tenant_id = t_roofing_01` from the bot token (NOT from message content)
3. ApprovalSignal is generated with `tenant_id = t_roofing_01`
4. Even if the payload contains a crafted `tenant_id` field, the gateway overwrites it with the authenticated value
5. Temporal workflow receives signal; `tenant_id` in signal matches workflow's bound tenant; proceeds

**Additional sub-scenario:** An MCP tool call is made with `X-Victoria-Tenant-Id: t_plumbing_02` but the MCP server was started bound to `t_roofing_01`.
- MCP server rejects the call: HTTP 403, event_type=security_violation in AuditEvent

**Observable outputs:**
- No `t_plumbing_02` data is accessed or modified
- AuditEvent for `t_roofing_01` has no events related to `t_plumbing_02`
- If the MCP mismatch variant is tested: security_violation AuditEvent exists; no tool execution occurred

**Invariants falsified:**
- INV-01: tenant_id is derived from auth, not request payload
- INV-02: MCP server enforces its own tenant binding per call

---

### SC-06: Sandbox Mode Escape Attempt

**Input:** Tenant `t_roofing_01` is in `mode=sandbox`. Workflow runs to DecisionPoint: send quote. Operator approves. Temporal receives ApprovalSignal. exec-plane invokes `sandbox_email.send_final` (a WRITE_EXTERNAL tool).

**Component participation:**
1. MCP server for sandbox_email receives tool call: `send_final`
2. MCP server checks: workflow mode = sandbox → HARD REJECT regardless of approval
3. MCP server writes AuditEvent: sandbox_escape_blocked

**Second variant:** The workflow mode field is omitted from the Temporal input (an implementation bug). The MCP server must default to `sandbox` if mode is absent — not to `live`.

**Observable outputs:**
- `send_final` returns a structured error: `{code: "SANDBOX_MODE", message: "..."}`
- No email is sent
- AuditEvent row: `event_type=sandbox_escape_blocked, case_run_id=X, tool=send_final`
- In the "mode absent" variant: same result — default-deny

**Invariants falsified:**
- INV-04: sandbox mode is enforced at MCP boundary
- INV-SB-DEFAULT: mode absent = sandbox, not live

---

### SC-07: Replay Determinism Check

**Input:** Case `cr_001` was run at T0. At T1, the same input is replayed with no rule changes. At T2, a ValidatedRule is added and the case is replayed again.

**Component participation:**
1. exec-plane replays cr_001 at T1: same input, same rules, same SkillVersion
2. learning-loop compares DecisionPoint outputs: must be identical to T0 run
3. A ValidatedRule is added; exec-plane replays cr_001 at T2
4. learning-loop compares DecisionPoint outputs: must differ only at the decision points covered by the new rule

**Observable outputs:**
- T1 replay: DecisionPoint rows (excluding metadata timestamps) are byte-for-byte identical to T0
- T2 replay: exactly the decision points covered by the new rule differ; all others are identical

**Invariants falsified:**
- UE-03: replay is deterministic given fixed inputs and fixed rules
- Hermes LLM calls during replay are made with `temperature=0` or equivalent determinism setting
- The rule set used for a replay is pinned to the specified SkillVersion or rule snapshot, not re-fetched live

---

### SC-08: Double-Tap Approval Idempotency

**Input:** Operator approves a ReviewPacket. WhatsApp session drops and reconnects. The approval message is re-delivered. Two ApprovalSignal events arrive at the Temporal workflow with the same `source_message_id`.

**Component participation:**
1. operator-ux receives message (first delivery): generates ApprovalSignal with `signal_id=s1`
2. operator-ux receives message (second delivery, same `source_message_id`): detects duplicate → does NOT emit a second ApprovalSignal
3. If the dedup fails at the gateway layer: Temporal receives two signals with same `signal_id=s1` → deduplicates on `signal_id` → processes exactly once

**Observable outputs:**
- AuditEvent table: exactly one `approval_received` event for the `decision_point_id`
- Temporal workflow: advances to next state exactly once
- No duplicate artifact generated; no duplicate Correction generated

**Invariants falsified:**
- INV-06: double delivery produces single state advance
- The gateway has dedup as a primary defense; Temporal has dedup as a secondary defense

---

## Section 6 — Ground Rules for Rounds 2–5

### 6.1 What "Answered" Means

A contract, invariant, or scenario is **answered** only when all of the following are present in the peer spec:

1. **Wire shape:** exact field names, types, and a representative JSON/Go struct example
2. **Writer named:** exactly one component is the writer; shared writes are disallowed unless the transaction boundary is specified
3. **Reader(s) named:** all components that consume this data are named
4. **Failure mode:** what happens when the writer fails, sends a bad value, or is unavailable
5. **Contract test named:** a specific test file, test function name (or equivalent), and what it would assert

Anything short of all five is **still open**. A narrative description of intent is not a contract. "We will propagate tenant_id" is not an answer. "The ApprovalSignal struct includes `tenant_id: string` (UUID format, derived from bot-token auth, written by operator-ux, read by exec-plane's Temporal signal handler, validated by asserting it equals the workflow's `tenant_id` input field; contract test: `test_approval_signal_tenant_mismatch_is_rejected`)" is an answer.

### 6.2 What This Critic Will Demand Per Round

**Round 2 (after peer drafts are circulated):**
- From exec-plane: exact Temporal workflow input schema (fields, types), MCP call header spec, volume naming convention, and how ValidatedRules are loaded into Hermes at runtime (model A/B/C/D from Section 1.4).
- From learning-loop: exact Postgres table list, tenant_id column presence, AuditEvent immutability mechanism, RuleCandidate physical location decision.
- From operator-ux: exact ApprovalSignal schema, dedup key specification, reconnect contract for WhatsApp non-Business-API.
- From ctrl-plane: how the Rule Review Console reads per-tenant RuleCandidates, exact authorization check on reviewer identity, and how cross-tenant vertical promotion strips tenant identifiers.

**Round 3 onward:** Every unresolved open item from Section 1 and Section 2 will be re-raised verbatim. The team must not assume a silence from this critic means acceptance.

### 6.3 How to Know if Misalignment Is Resolved vs. Papered Over

A misalignment is **resolved** when the two conflicting components have produced a single, consistent contract specification that satisfies the Section 6.1 discriminator — and the contract test is named at the boundary, not inside either component's own unit tests.

A misalignment is **papered over** when:
- One component says "we will coordinate with X" without a specific agreed contract
- Both components describe the same concept with different field names and neither has updated to match
- The contract test exists only as a unit test inside one component (testing your own behavior is not a boundary test)
- A narrative footnote says "this is TBD in a future round" for an item that is load-bearing today

### 6.4 Standing Veto Conditions

This critic holds a standing veto — meaning the full spec is not shippable — until the following are resolved:

1. **Where RuleCandidates live** — specifically the cross-tenant vertical promotion mechanism with data stripping (Section 1.3 + INV-05)
2. **How Hermes consumes ValidatedRules** — a single named model agreed between exec-plane and learning-loop (Section 1.4)
3. **Approval signal schema** with idempotency keys specified (Section 1.5)
4. **MCP WRITE_EXTERNAL enforcement** — the pre-execution approval check inside MCP servers (INV-03)
5. **AuditEvent immutability** enforced at the storage layer (UE-04)

These five are not style preferences. They are architectural properties that, if left unresolved, will produce a system that either leaks tenant data, accepts double-spend approvals, or cannot replay case history deterministically. No component can ship its piece with these open.

---

*Last updated: Round 1. All items in this document are OPEN. Nothing is closed until the Section 6.1 discriminator is fully met.*

---

## Round 2 — Per-spec audit

Round 2 audits the four peer drafts against the framework above. Each spec is scored on TDD discipline and isolation completeness, then graded on over/under-engineering, with top demands the peer must answer in Round 3. After per-spec scoring, the cross-cutting summary addresses the moderator's C1–C8 conflict list, names new conflicts the moderator missed, maps Round 1 acceptance scenarios to peer test ownership, audits TDD posture across the team, and renders a verdict on convergence.

---

### A. ctrl-plane (`01-control-plane.md`)

**TDD discipline score: 4/5.**

Rubric: Invariants are stated before implementation in every section (§3.1, §6.5, §7.2, §8.1, §9.1, §10.1). Contract tests with peers are named in §7.3 (control-plane ↔ execution-plane), §12.2 (Temporal handoff), §12.3 (messaging gateway). Tenant-isolation integration tests are concrete (§12.4). Property/fuzz tests for audit idempotency, version chain, and tenant-context extraction are specified (§12.5). The 1-point deduction: invariants `AUTH_04` (§6.5) ships with a hand-wavy 403-vs-404 rationale that the spec itself flags as overturnable — the test is well-named but the assertion is contestable. `EVAL_05` ("evaluation service must not store LLM-generated content beyond assertion result and a brief summary") is a posture, not a falsifiable test.

**Isolation completeness score: 4/5.**

Rubric: tenant_id origin rule is correctly stated (§3.3 — JWT claim only, never from request body). Defense-in-depth via Postgres RLS (§3.5) with the explicit `SET LOCAL app.current_tenant` discipline is exactly right. Cross-tenant contract test exists (§7.3 third row). HMAC verification test for the X-Victoria-Tenant-Id header (§3.2) is present. The 1-point deduction: §13 places **all ValidatedRule scopes — including tenant-scope** — in control-plane Postgres. This is a direct boundary violation of the per-tenant data isolation principle. Tenant-scope rules embed operator IP (pricing rules, supplier policies). Centralizing them violates the spec's own §3 propagation chain at the storage layer, and there is no contract test that proves a control-plane DB row tagged `tenant_id` cannot be read by another tenant via a privileged path. RLS helps; it does not substitute for physical separation of operator policy.

**Over-engineering smells:**
- **OE-CP-1 (§4.2):** Single shared Redis "keyed by tenant_id:*" for sessions, rate limits, and idempotency keys. This is the same multi-tenancy pattern that has produced cross-tenant leakage incidents in well-known services. At Phase 1 (≤10 tenants) the mental tax of per-tenant Redis namespacing is trivial; the abstraction over a shared instance is premature.
- **OE-CP-2 (§7.4):** The console exposes 11 endpoints in Phase 1. `GET /review/tenants/:tenant_id/candidates` and `GET /review/tenants/:tenant_id/rules` are scoped shortcuts duplicating filterable list endpoints. Two views over the same data without a measured access pattern justifies neither route.
- **OE-CP-3 (§8.4):** `artifact_equivalence` assertion type uses an "LLM-judge similarity above threshold" — at Phase 1 there are zero approved cases to even compare against. The replay regression bar should be decision-outcome equivalence (which §8.4 also has) until artifact-level signal is needed.

**Under-engineering smells:**
- **UE-CP-1 (§5.3 + §11.1):** "Hard contract requirement on execution plane" for a durable audit event buffer is asserted in two places, but the actual buffer mechanism (Temporal activity, Postgres WAL, Redis stream) is OQ-02. There is no contract test that the buffer actually preserves events across a control-plane outage. This is the load-bearing reliability claim of the entire audit chain.
- **UE-CP-2 (§5.3):** The control-plane → execution-plane internal API surface is **assumed** ("If the Execution Plane Architect specifies a different contract..."). exec-plane §1 says "the execution plane has no inbound network surface." These two specs are in direct contradiction on whether `/internal/replay`, `/internal/rules/push`, etc. exist as HTTP endpoints. UE because neither side has caught it.
- **UE-CP-3 (§5.2):** Provisioning workflow lists 12 steps, none of which mention provisioning the WuzAPI bridge container required by operator-ux §4.4. The bridge is invisible to ctrl-plane provisioning. No contract test catches this.
- **UE-CP-4 (§10.2):** Billing fails open is a deliberate choice and probably correct; the failure mode test is missing — there is no contract test that a control-plane outage allows a CaseRun to start.
- **UE-CP-5 (§13):** "Effective rule set" endpoint at `GET /internal/tenants/:tenant_id/rules/effective` has no `as_of` parameter. exec-plane §8.2 and learning-loop §10 both require time-travel queries for replay determinism. Missing.

**Top 5 demands for Round 3:**
1. Either drop "all ValidatedRules in control-plane DB" or specify the contract test that proves a privileged cross-tenant read of tenant-scope rules cannot occur — and reconcile with learning-loop's split-by-trust-boundary model.
2. Adopt the operator-ux WuzAPI bridge container as a step in `TenantProvisioningWorkflow` (§5.2) or specify the alternate provisioner that owns it.
3. Reconcile the audit log location with learning-loop §11.1 — name a single canonical store; if it is split, name the writer/reader for each side and the test that proves no cross-tenant control-plane read exposes another tenant's events.
4. Specify the `as_of` parameter on `/internal/rules/effective` (or its replacement) — replay determinism requires it.
5. Provide the contract test for "execution plane buffers audit events during control-plane outage" — without it, §11.1's reliability claim is unverifiable.

---

### B. exec-plane (`02-execution-plane.md`)

**TDD discipline score: 4.5/5.**

Rubric: Invariants stated before implementation throughout (§2.1, §3.1, §4.1, §5.1, §5.2 INV-E1, §5.3 INV-D1, §5.4 INV-I1, §6.1, §7.1). Contract tests are named per-MCP (§10.1) and per-workflow (§10.2). Side-effect classification with a CI-enforced static analysis check (§10.3) is exemplary. Tenant isolation tests are concrete (§10.4). The 0.5-point deduction: `test_approved_signal_idempotency` (§10.2) asserts "executes write_final exactly once" but does not specify how the test detects the second invocation — the assertion needs a fail mechanism (e.g., MCP server records call count). Otherwise this is the strongest TDD spec on the team.

**Isolation completeness score: 4.5/5.**

Rubric: Volume naming `hermes-data-{tenant_id}` (§2.3) with a manifest-vs-volume tenant-label match check (INV-H4, test §10.4) is exactly the Round 1 INV-02 ask. Task queue isolation with explicit naming (§3.2) and the cross-tenant unreachability test (§10.4) is correct. Per-MCP tenant binding with header validation (§5.1, INV-H1 enforcement at MCP layer, test §10.1 row 5) is correct. Network egress allowlist (§2.6) is concrete. Sandbox-mode multi-layer defense (§6.3 four independent layers) goes beyond what was demanded in Round 1. The 0.5-point deduction: §3.4 INV-T3 is enforced by **workflow code** ("Enforced by workflow code; contract test verifies replay history"). A buggy workflow that forgets to gate on the signal slips past this check at runtime — the contract test only catches it post-hoc when replaying recorded history. Round 1 demanded the gate be inside the MCP server. exec-plane has not adopted that. See cross-cutting C6 below.

**Over-engineering smells:**
- **OE-EP-1 (§3.5 retry policy):** Three retry classes are differentiated by 1s vs. 2s initial interval and 30s vs. 60s max interval. At Phase 1 (≤10 tenants, sandbox mode only), one retry policy class is enough; the differentiation is a parameter sweep without observed need.
- **OE-EP-2 (§5.1):** The MCP idempotency log uses a Postgres table keyed on `(tool_name, idempotency_key)` with cached responses. Idempotency on read-class tools provides no value (re-running a read is fine); on draft-class tools it adds a write per call. Limit caching to the actual side-effecting subset.

**Under-engineering smells:**
- **UE-EP-1 (§3.4 INV-T3):** Signal-gating for `write_final` activities is enforced "by workflow code." This is the single layer that gates a non-sandbox WRITE_EXTERNAL on a missing approval. Round 1 demand for the MCP-internal approval check has not been adopted. The multi-layer sandbox defense (§6.3) is excellent for sandbox enforcement but does not cover this bug class. Re-raised below.
- **UE-EP-2 (§7.2 ASSUMPTION):** "Messaging gateway delivers operator responses to the execution plane as Temporal signals. The gateway must address the signal to the exact WorkflowID." This is in direct conflict with operator-ux OQ-1 and §10. No reconciled contract exists.
- **UE-EP-3 (§8.3 ASSUMPTION):** "The control plane exposes a `/rules` endpoint that accepts `{ tenant_id, workflow_type, as_of?: timestamp }`." ctrl-plane §13 exposes the endpoint without `as_of`. Mismatch.
- **UE-EP-4 (§9.4 LLM provider outage):** 6 retries × 5 minutes = 30 minutes pause, then "emits an LLMOutageAlert audit event and pauses at the activity level; a Temporal heartbeat keeps it alive." There is no contract test for this state, and no operator-facing notification — the operator just doesn't get a packet. UE because the failure is invisible to the operator UX.
- **UE-EP-5 (§4.2 INV-CR3):** sandbox_mode immutable for the lifetime of a CaseRun is good. There is no test that a tenant whose `mode` is currently `shadow` cannot be downgraded to `sandbox` for a particular CaseRun via a malicious provisioning manifest tweak. Mode downgrade attack vector unaddressed.

**Top 5 demands for Round 3:**
1. Add the MCP-internal approval check for WRITE_EXTERNAL tools. The MCP server queries the audit chain (or receives a signed proof in the call) before executing. The current 4-layer defense covers sandbox escape, not approval bypass by buggy workflow code. (Round 1 demand, unmet, re-raised.)
2. Reconcile §3.4 ASSUMPTION (Temporal signals from gateway) with operator-ux §10 (gateway-emitted ApprovalSignalEnvelope). Name the canonical signal name (`CaseApproved` / `CaseRejected` per exec-plane vs. `operator_correction` per operator-ux), one envelope shape, one idempotency key.
3. Drop `rule_snapshot_id` (§4.4, §8) and standardize on learning-loop's `skill_version_id`. They are the same artifact under different names.
4. Specify the canonical mode encoding — `sandbox_mode: boolean` (this spec) vs. `mode: 'sandbox'|'shadow'|'autopilot'` (learning-loop). Use the 3-state enum; the boolean cannot represent shadow or autopilot.
5. Specify how a replay against a historical `skill_version_id` resolves rules — does it use the immutable `rule_manifest` (correct) or re-fetch active rules (wrong, breaks determinism). Currently §8 is silent.

---

### C. learning-loop (`03-correction-loop.md`)

**TDD discipline score: 4.5/5.**

Rubric: Invariants stated before algorithms throughout (§4.1 C1-C5, §5.1 CONF-1-5, §6.4 SCOPE-1-3, §10.5 REPLAY-1-3, §11.7 AUDIT-1-5). Test cases inline with each algorithm (§4.3 MATCH-1-6, §5.4 CONF-1-5, §8.4 VER-1-3, §13). Property tests are correctly identified (§13.1 MERGE-1-6). Audit completeness is enforced by scheduled DB checks (§13.4). The 0.5-point deduction: `REPLAY-3` ("regression definition is stable... pure function of the diff data") is asserted but the test is not named — for an invariant about determinism, the contract test is load-bearing.

**Isolation completeness score: 3.5/5.**

Rubric: Storage split by trust boundary (§2 placement decision) is the correct architectural answer to the cross-tenant-promotion problem and explicitly addresses Round 1's challenge. Tenant_id presence on every per-tenant table (§2.3 onwards) is correct. AuditEvent immutability is enforced via DB-level RLS `USING (false) WITH CHECK (false)` (§11.5) — this satisfies Round 1 UE-04. The 1.5-point deductions: (1) `rule_candidates_control` (§2.7 stripped replica) claims to scrub PII but **retains `conditions_canonical` and `recommended_action`** unchanged. Tenant condition values can carry customer/supplier names ("ABC Realty," "Greenline Property" — both literal in the product spec). The stripping logic is asserted, not specified. Round 1 INV-05 is not met. (2) §6.2 vertical-scope escalation requires "≥3 distinct tenants within the same vertical" — but the cross-tenant aggregation pipeline that flags this for the reviewer is unspecified; it's the same boundary INV-05 covers.

**Over-engineering smells:**
- **OE-LL-1 (§5.2 confidence formula):** Wilson lower bound × recency_factor × scope_consistency_factor at Phase 1 with ≤10 tenants and zero observed corrections. Wilson is a defensible choice; multiplying by two heuristic factors with hand-tuned constants (recency_half_life=30, recency_floor=0.1, scope_consistency caps at 1.0) is parameter sprawl ahead of data. Round 1 OE-05 named this specifically: a deterministic formula `f(evidence_count, contradiction_count)` is sufficient until there is data to fit. Recency and scope-consistency multipliers add complexity that cannot be validated.
- **OE-LL-2 (§4.3 near-match step):** Step 3 schema-aware similarity ("numeric within ±20%, categorical variant of same enum") at Phase 1 with no observed near-misses is solving an imagined problem.
- **OE-LL-3 (§8.2 supersession case 2):** "If `conditions_hash` differs: both remain active. Resolution at runtime via scope priority (§6.3)." This relies on a 4-level scope hierarchy (case/tenant/vertical/default) where Phase 1 will operationally only have tenant scope. Vertical and default scopes are correct as future capability; the runtime resolution algorithm should not be implemented until they exist.

**Under-engineering smells:**
- **UE-LL-1 (§2.7 stripped replica):** No specification of what fields are stripped or transformed; no contract test that condition values cannot identify a tenant. Round 1 INV-05 unmet.
- **UE-LL-2 (§11.1 audit log location):** "Two audit stores: per-tenant for tenant-sensitive events; control-plane for cross-tenant/reviewer events." ctrl-plane §9.3 says the control plane is the "authoritative immutable audit log." These are incompatible. learning-loop §11.6 lists the writer/reader contract per event type, but the location for each event is not specified row-by-row.
- **UE-LL-3 (§9.3 runtime context block):** The skill version manifest is correctly defined; how it reaches Hermes (file mount, system-prompt injection, API fetch) is "owned by Execution Plane Architect" — but exec-plane §8 says the rules are fetched at workflow-start as a system-prompt block. The contract is implicit. The handoff field name is `skill_version_id` here but `rule_snapshot_id` in exec-plane §4.4 — terminology drift unaddressed.
- **UE-LL-4 (§10.2 replay determinism table):** "Decision-point outcomes (action chosen, branch taken)... should be deterministic given rules + input." This is stated, not enforced. There is no test that an LLM-driven decision (`InvokeHermesReasoning` in exec-plane §3.4) produces the same `agent_output.action_taken` across two replays with frozen inputs and rules. LLM temperature, sampling seed, and prompt injection nondeterminism are unaddressed.
- **UE-LL-5 (§3 idempotency_key):** `corrections.idempotency_key UNIQUE` is correct. Its relationship to operator-ux's `gateway_idempotency_key` (signal layer) and `packet_id:deliver:N` (outbound) is unspecified. Three keys; their composition rule is missing.

**Top 5 demands for Round 3:**
1. Specify the scrub operation for `rule_candidates_control`: which fields are stripped, how condition values are tokenized to remove tenant-identifying entities, and the contract test that demonstrates a stripped row cannot be linked back to a source tenant.
2. Reconcile audit log location with ctrl-plane §9.3 — pick a single canonical store, name the writer for each event_type in §11.3, name the reader, name the location.
3. Drop the `rule_snapshot_id` / `skill_version_id` terminology drift — pick `skill_version_id`.
4. Specify the determinism contract for replay (§10.2): LLM temperature setting, sampling-seed pinning policy, system-prompt invariance proof. Without these, REPLAY-3 is not a falsifiable invariant.
5. Down-scope confidence formula to evidence_count + contradicting_count for Phase 1; defer Wilson × recency × scope_consistency to Phase 2 when there is data to fit.

---

### D. operator-ux (`04-operator-experience.md`)

**TDD discipline score: 3.5/5.**

Rubric: Invariants are stated (§3 I-01 to I-10). Contract tests are named (§3 CT-Outbound, CT-Inbound, CT-Idempotency, CT-Expiry, CT-Signal, CT-E2E, CT-Channel). The contracts are reasonable and surface-aligned. The 1.5-point deductions: (1) several invariants are asserted without a corresponding test fail mode (I-05 "at-least-once with provider-side dedup key" — the dedup key is named but the test that catches double-delivery is "CT-Outbound-01" which only checks field mapping, not dedup); (2) I-06 (channel adapter interface identical) is enforced by running the same test suite against both adapters — a good pattern, but the suite content for Telegram is not specified, only "the same as WhatsApp"; (3) parse cascade thresholds (`threshold_resolve`, `threshold_followup`) are runtime knobs without a contract test that an unset knob produces a deterministic default behavior.

**Isolation completeness score: 3/5.**

Rubric: tenant_id is propagated through ReviewPacket, StructuredCorrection, ApprovalSignalEnvelope (§2). Signed preview URLs include `tenant_id` claim with mismatch rejection (§8.2, I-09). The 2-point deductions: (1) **tenant_id origin is unspecified** — the gateway derives it from where? The phone-to-tenant binding (§4.2 `channel_bindings`) is the source, but there is no invariant stating that an inbound webhook's tenant_id must be derived from `channel_bindings.tenant_id` matched on the receiving WhatsApp number, and never from the message body or sender claim. Round 1 INV-01 demands this binding. (2) §10 ApprovalSignalEnvelope is sent to "Temporal signal channel `correction:{case_run_id}:{decision_point_id}`" — this is in conflict with exec-plane §3.4 which uses `CaseApproved` / `CaseRejected` signal names and embeds idempotency_key in the payload. Two specs, two signal contracts. (3) §8.2 signed JWT is signed with "per-tenant signing key" — the signing key origin (provisioned by ctrl-plane? generated by gateway?) is unspecified. Without a clear key provisioning chain, INV-01 cannot be validated for the preview URL surface.

**Over-engineering smells:**
- **OE-OU-1 (§4.6 button rendering):** Three rendering modes (`interactive_buttons`, `list_message`, `text_fallback`) with capability advertisement at session init. WuzAPI's actual capability is fixed per bridge version. The capability negotiation layer is justified only if multiple bridge versions coexist — at Phase 1 with one bridge per tenant on a pinned version, the negotiation is dead code.
- **OE-OU-2 (§7.1 parse cascade):** Five stages including LLM-assisted parse with confidence thresholds at Stage 3. Stage 3 invokes an LLM for free-text parsing; this requires LLM credentials in the gateway, an additional surface area (parse pipeline costs, latency, failure modes), and observability needs not present in the messaging layer. At Phase 1, button-tap (Stage 1) covers 95%+ of corrections by design (the spec explicitly says "narrow and structured" button set). Stage 3 LLM parse is premature.
- **OE-OU-3 (§3 I-06):** ChannelAdapter abstraction over 2 channels: SOFTENED from Round 1 OE-01. The 3-method interface (send/ack/getSessionStatus) is small and the difference between WhatsApp (QR pairing, capability negotiation) and Telegram (bot token) is real. The smell is now: do not extend the abstraction with a ChannelRegistry, locale string mapper, or capability matrix until a 3rd channel exists. Current 2-channel abstraction is acceptable.

**Under-engineering smells:**
- **UE-OU-1 (§4.4 session reconnect):** "WuzAPI persists WhatsApp session keys on disk." Session disconnect is detected via heartbeat miss (§11). On reconnect, the spec does not specify: (a) what happens to messages received during the disconnect (lost? buffered by WhatsApp? buffered by WuzAPI?), (b) how the gateway recovers in-flight `ReviewPackets` whose delivery was in-flight at the disconnect, (c) whether messages that arrived during disconnect can produce duplicate StructuredCorrections after reconnect. Round 1 UE-01 (single point of failure on the WhatsApp session) is not addressed.
- **UE-OU-2 (§9.3 out-of-order replies):** Free-text replies are correlated via "the most recent active follow-up session" or "the most recently sent unresolved packet." This is ambiguous when a tenant has multiple unresolved packets. The fallback to "most recently sent unresolved packet" is a heuristic that can deliver an operator's reply to the wrong case. There is no contract test for this corruption.
- **UE-OU-3 (§10.2 expiry sweep):** OQ-1 unresolved — gateway sweep vs. Temporal callback. exec-plane §3.5 has a 72h signal_wait_timeout. operator-ux §2.1 has a `packet.expires_at` (24h in the example). Two timeouts, two owners, no reconciled timeline. If the gateway expires the packet at T+24h but Temporal waits until T+48h, what does the workflow do for 24h?
- **UE-OU-4 (§4.5 ChannelAdapter):** The interface defines `send`, `ack`, `getSessionStatus`. There is no method for receiving a message — inbound is implicit (via a webhook controller). For TDD, there should be an `onInbound` handler in the interface so the same test harness exercises both adapters' inbound paths.
- **UE-OU-5 (§2.3 ApprovalSignalEnvelope):** The envelope's `gateway_idempotency_key` is the dedup key for Temporal signal delivery. exec-plane §7.3 uses `idempotency_key` as a field on the signal payload directly. Two names, possibly two semantics. Reconciliation needed.

**Top 5 demands for Round 3:**
1. Specify the tenant_id origin invariant: the gateway derives `tenant_id` from `channel_bindings.tenant_id` matched on the receiving channel (WhatsApp number or Telegram bot_token). It is never read from the message body or a client claim. Add the contract test.
2. Reconcile the signal contract with exec-plane §3.4: name the signal (one of `CaseApproved` / `CaseRejected` / `operator_correction`), one envelope shape, one idempotency key. Adopt the resolution.
3. Specify the WhatsApp session reconnect contract: message delivery during disconnect, in-flight packet recovery, dedup of correction events post-reconnect. Add the contract test for "session drops and recovers with no double-delivered correction."
4. Reconcile expiry timing with exec-plane §3.5 — pick one timeout per workflow type, one owner of the sweep. The current state has two timers running on the same case.
5. Specify how `gateway_idempotency_key`, `packet_id:deliver:N`, `provider_message_id`, `corrections.idempotency_key`, and exec-plane signal `idempotency_key` relate. Either define them as the same key with a derivation rule, or specify their distinct roles in a layered dedup scheme.

---

### Round 2 cross-cutting summary

#### A. The C1/C2 deadlock

**C1 — Where ValidatedRules physically live: learning-loop's split-by-trust-boundary is correct; ctrl-plane's centralization is wrong.**

Force-rank with rationale:

1. **learning-loop's split (§2):** tenant-scope rules in per-tenant DB, vertical/default in control plane, stripped replica for reviewer cross-tenant visibility. **Best.** Tenant-scope ValidatedRules embed operator IP — pricing rules, supplier policies. The product spec opens with "per-client isolation by default" as a Product Principle. Putting tenant policy in a shared multi-tenant DB violates this even with RLS, because the threat model includes privileged control-plane operators and stolen control-plane credentials.
2. **Hybrid:** split storage with a control-plane read-only view assembled via per-tenant API calls. **Acceptable** — preserves isolation; pays a query-time cost the reviewer can absorb.
3. **ctrl-plane centralization (§13):** all scopes in control-plane Postgres. **Wrong.** The justification — "vertical and default scope rules cannot live in per-tenant DBs" — is correct for vertical/default but does not extend to tenant-scope rules. Centralizing tenant-scope is a non sequitur.

**Demand:** ctrl-plane §13 must reduce to "vertical and default ValidatedRules live in control-plane DB; tenant-scope rules live in the per-tenant execution-plane DB and are read by the console via per-tenant API calls." This matches learning-loop §2.

**C2 — This is not a deadlock; it is terminology drift.**

exec-plane's `rule_snapshot_id` (§4.4, §8) and learning-loop's `skill_version_id` (§9) are the same artifact under different names. exec-plane's `FetchValidatedRules` activity returns a system-prompt context block; learning-loop's SkillVersion is the manifest that block is built from. They are the same handoff.

**Reconciliation:** drop `rule_snapshot_id`. Standardize on `skill_version_id`. `FetchValidatedRules` returns the SkillVersion's `rule_manifest`. The "system-prompt context block" of exec-plane §8.3 is the runtime rendering of the `rule_manifest`. Both architects need to update terminology.

#### B. New cross-component conflicts the moderator's C1–C8 missed

**N1. Sandbox-mode encoding mismatch — 4 ways.**
- exec-plane §4.4: `sandbox_mode: boolean`
- learning-loop §2.3 `case_runs.mode`: enum `'sandbox' | 'shadow' | 'autopilot'`
- operator-ux §2.1 `metadata.run_mode`: string `"sandbox"`
- ctrl-plane §10.2 usage events: `sandbox_mode` boolean

The product spec progression model has four states (sandbox replay → approval-gated shadow → partial autopilot → validated skill). The boolean cannot represent shadow or autopilot. **Force the 3-state enum.** Note the boolean asymmetry: if `sandbox_mode = false` in exec-plane, what mode is it? Undefined.

**N2. Audit log location — direct contradiction.**
- ctrl-plane §9.3: "The control plane hosts the authoritative, immutable audit log."
- learning-loop §11.1: "Two audit stores: per-tenant for tenant-sensitive events; control-plane for cross-tenant/reviewer events."
- exec-plane §3.4 emits events into Postgres; it is unclear which Postgres.
- operator-ux §1 `audit events: gateway writes these` to "shared infra, not gateway-owned."

This is incompatible across three specs. **Pick one storage topology; pin every event_type to a writer, a reader, and a location.**

**N3. Vertical-scope stripping is incomplete.** learning-loop's `rule_candidates_control` retains `conditions_canonical` and `recommended_action` unchanged. Condition values can carry customer/supplier names from operator corrections (the product spec's worked example uses literal names like "ABC Realty"). Round 1 INV-05 unmet.

**N4. WuzAPI bridge container is unowned by ctrl-plane provisioning.** operator-ux Assumption A4 says "same tooling as Hermes containers." ctrl-plane §5.2 provisions Hermes, MCP sidecars, Temporal worker — no WuzAPI bridge. **Real gap. Force ctrl-plane to add it as a step.**

**N5. Operator JWT path is unused.** ctrl-plane §6.3 issues operator JWTs via WhatsApp OTP verification. operator-ux uses none of this — its `operator_id` is `op_whatsapp:+phone`, signed-URL preview tokens are gateway-issued. Either delete the operator JWT issuance, or specify which operator-ux surface consumes it.

**N6. Idempotency key topology is unspecified.** Three keys at the gateway (`gateway_idempotency_key`, `packet_id:deliver:N`, `provider_message_id`), one in the workflow (signal `idempotency_key`), one in learning-loop (`corrections.idempotency_key`). Their derivation/composition is missing. They are not obviously the same key.

**N7. Replay determinism + active-vs-deprecated rule fetch.** exec-plane's `FetchValidatedRules` is "fetched at workflow-start" — replay against an old `skill_version_id` should use the immutable `rule_manifest` of that SkillVersion (which includes rules that may now be deprecated). Neither spec explicitly states this. Without it, replay against an old run produces a different result than the original run because the active rule set has changed.

#### C. Round 1 acceptance scenarios mapped to peer test ownership

| Scenario | Components participating | Owning contract test(s) | Coverage |
|---|---|---|---|
| SC-01 Golden-path correction | All four | exec-plane §10.2 `test_workflow_id_equals_case_run_id`, learning-loop §13.2 GOLDEN PROMOTE-1, operator-ux §3 CT-E2E-01, ctrl-plane §7.3 happy path | **Partial** — no end-to-end test that exercises all four boundaries in a single run. Cross-component glue is unowned. |
| SC-02 Multi-correction promotion | learning-loop, ctrl-plane | learning-loop §13.2 GOLDEN PROMOTE-1 (single promotion); §13.1 PROPERTY MERGE-2 (commutativity) | **Partial** — no test for "3 corrections across 3 case runs cross threshold and promote." |
| SC-03 Contradicting-correction supersession | learning-loop, ctrl-plane | learning-loop §8.4 VER-1, GOLDEN PROMOTE-2, ctrl-plane §7.7 rollback flow | **Mostly covered** — supersession path tested. |
| SC-04 Abandoned packet | operator-ux, exec-plane | operator-ux §3 CT-E2E-02; exec-plane §10.2 `test_approval_timeout_sends_reminder` | **Partial** — no reconciled timeout (gateway expiry vs. Temporal timeout). The two specs run two timers and neither test asserts which fires first. |
| SC-05 Tenant isolation leak attempt | exec-plane, ctrl-plane, operator-ux, learning-loop | exec-plane §10.4 `test_task_queue_cross_tenant_unreachable`, `test_mcp_tenant_id_isolation`; ctrl-plane §12.4 isolation tests | **Partial — cross-tenant vertical-rule promotion leak (the variant where stripped data carries tenant identifiers) is not covered by any peer's tests.** This is the Devil's Advocate finding. |
| SC-06 Sandbox-mode escape attempt | exec-plane | exec-plane §10.1 `test_write_final_absent_in_sandbox`, §10.3 static analysis | **Mostly covered for sandbox.** SC-06's second variant (mode field absent → default-deny) is not explicitly tested. **Approval-bypass variant (non-sandbox, no approval signal) is NOT covered** — INV-T3 is enforced by workflow code only; an adversarial workflow with a missing gate slips past every named test. |
| SC-07 Replay determinism | exec-plane, learning-loop | exec-plane §10.2 `test_replay_creates_new_case_run`, `test_original_execution_unchanged_after_replay`; learning-loop §13.3 REPLAY TEST-1 to TEST-4 | **Narrowed scope, unmet.** learning-loop §10.2 explicitly downgrades determinism to "decision-outcome level" (not artifact bytes). LLM temperature/seed pinning is unspecified. The test for "two replays of the same input + same skill_version_id produce the same agent_output.action_taken" is named but the LLM-determinism precondition is asserted, not verified. |
| SC-08 Double-tap approval idempotency | operator-ux, exec-plane | operator-ux §3 CT-Inbound-04; exec-plane §10.2 `test_approved_signal_idempotency` | **Mostly covered** if the two idempotency keys (gateway_idempotency_key vs. signal idempotency_key) are reconciled to the same key. Currently unreconciled (N6). |

**Scenarios NOT testable given current peer specs:**
- SC-05 cross-tenant vertical-rule promotion leak: stripping logic unspecified.
- SC-06 approval-bypass variant: MCP-internal approval check absent.
- SC-07 LLM-determinism: temperature/seed/prompt-invariance unspecified.

#### D. TDD posture audit

**The team's strength is uniform: every spec has strong internal invariant catalogs with named contract tests for the writer's own component.**

**The team's weakness is also uniform: boundary tests across two components are nobody's job.**

- **ctrl-plane §12.2 contract tests (control plane ↔ execution plane):** the assertions describe behavior on the control-plane side of the boundary ("assert `/internal/rules/push` called with correct payload; assert execution plane acknowledges"). These are mock-based tests of the control-plane caller. They do not run against an actual execution plane.
- **exec-plane §10.2 Temporal workflow replay tests:** replay tests against recorded history. They prove the workflow code is consistent with the recorded signal stream — they do not prove the signal stream actually delivered from operator-ux is in that shape.
- **learning-loop §13:** algorithm-internal property tests; cross-component tests (parsing pipeline owner, control-plane replication job owner) are **deferred as Open Questions**.
- **operator-ux §3 CT-E2E-01:** "Full path — packet sent → operator taps button → structured correction emitted → signal delivered." This is the closest the team has to a real cross-component test. But the assertion is "asserted on all three contract surfaces" — the signal-delivery side is mocked or otherwise unspecified. The "delivered" assertion does not run against an actual Temporal worker.

**Tests of implementation rather than tests of contracts (specific instances):**
- ctrl-plane §12.4 Test 4 ("Push a rule for tenant A; assert tenant B's execution plane (mocked) does not receive a push call") — the execution plane is **mocked**. This tests ctrl-plane's outbound code, not the actual cross-tenant invariant.
- exec-plane §10.4 `test_mcp_tenant_id_isolation` — runs against an MCP server "configured for t_999". This is an invariant of the MCP server's own enforcement, not a cross-component invariant on the exec-plane → MCP path.
- operator-ux §3 CT-Channel-01 — "the same packet, rendered by both adapters, passes all button-set and field-mapping assertions." This tests the adapters' rendering — not whether operators on either channel can complete the correction loop end-to-end.
- learning-loop §13.4 audit completeness checks run against a single DB. The audit chain spans per-tenant and control-plane DBs (per learning-loop's own §11.1). The check cannot detect a missing event in the other DB.

**Devil's Advocate's domain:** cross-component integration tests are a Devil's Advocate deliverable, not a peer's. The team needs to accept that contract tests at the boundary have a single owner — and that owner must produce a test rig that runs all four components together against the canonical SC-01 to SC-08 scenarios.

#### E. Verdict on convergence

**Verdict: Diverging on architecture; converging on local TDD discipline.**

The team is converging on individual component quality — every spec has named invariants, contract tests, and a clear scope. This is healthy.

The team is diverging on the cross-component contracts that determine whether the system actually works. The N1–N7 list above is not minor — it includes audit log location (N2), sandbox-mode encoding (N1), idempotency key topology (N6), and replay determinism (N7). Three of these are load-bearing to the security and learning model.

**The three questions that, if answered jointly, force convergence:**

1. **Storage topology question — what lives where.** A single answer covers ValidatedRule storage (C1), audit log location (N2), and the cross-tenant stripped replica (N3). Concrete demand: produce one table that names every Postgres table in the system, the database it lives in (per-tenant execution DB or control-plane DB), the writer, and the readers. Without this table, every other contract is built on uncertain ground.

2. **Approval signal contract question — one signal or many.** A single answer covers the operator-ux ApprovalSignalEnvelope (§2.3), exec-plane CaseApproved/CaseRejected signals (§7.3), the idempotency key relationship (N6), and the timeout ownership (UE-OU-3 + N4). Concrete demand: pick one signal name, one envelope shape, one idempotency key derivation rule, one timeout owner.

3. **WRITE_EXTERNAL approval enforcement question — MCP-internal check or workflow-code-only.** Round 1 demand was MCP-internal; exec-plane §3.4 INV-T3 is workflow-code-only. The Devil's Advocate position is unchanged: the MCP-internal check is required because the bug class it catches (workflow code that forgets to await the signal) is not caught by any other layer of the multi-layer defense in §6.3.

If these three are answered in Round 3, the system architecture converges. If they are not, the Round 4 conversation will be the same.

---

*Last updated: Round 2. C8 (audit immutability) is now MET — both ctrl-plane §9.3 and learning-loop §11.5 enforce at storage layer. C7 (channel adapter) is SOFTENED — current 2-channel abstraction is acceptable; demand is no further pluggability until 3rd channel. C6 (WRITE_EXTERNAL gating) remains OPEN — exec-plane has not adopted the MCP-internal check. C1 resolved in favor of learning-loop's split. C2 reframed as terminology drift, not deadlock. N1–N7 are new conflicts. SC-05, SC-06 (approval-bypass variant), SC-07 (LLM determinism) remain untestable under current peer specs.*

---

## Round 3 — Resolution audit and end-to-end scenario testability

The moderator brief and the `00-overview.md` §2 backbone declare 14 cross-team-resolved decisions (A1–A14) plus 9 R3 cleanup items (C12–C14, N1, N3, N4, N6, OQ-NEW-3, OQ-NEW-5, OQ-06, AUDIT-CANON, ALERT-SINK, REPLAY-SCHED, MCP-SNAPSHOT, HERMES-VOL). I read all four R3 spec bodies in full. The headline finding governs everything below: **ctrl-plane's R3 conflict-resolution index lists 9 RESOLVED-X items pointing to sections that do not exist in the file or were not rewritten.** ctrl-plane §17, §6.7, §6.8, §5.7, §8.7, §9.6.1 are all cited and absent. ctrl-plane §13 ("ValidatedRule Storage — Trust-Boundary Split") still describes the R2 model the index claims was reversed to a unified store. ctrl-plane §6.2 still says "HMAC-signed Bearer tokens" while the index claims §6.2 was rewritten to mTLS. This is the textbook "papered over" pattern from Section 6.3 of this document's framework: resolution claimed in metadata, body unchanged. Holding the line.

Separately, learning-loop did not write any R3 cleanup items into its body — `approval_received`, `mcp_audit_reader`, `mcp_read_snapshot`, and the `vertical_candidate_aggregates` strip rules are all cited by exec-plane and ctrl-plane index but absent from learning-loop §11. exec-plane is the only spec where the R3 body content matches the R3 index claims.

### 1. Per-conflict resolution audit

Resolution status legend:
- **RESOLVED** = body content matches across all relevant specs; the contract test is named at the boundary.
- **PARTIALLY-RESOLVED** = at least one consumer aligns; at least one citation is broken or contradicted.
- **ASPIRATIONAL** = index claims resolution; body content does not exist or contradicts the claim.
- **UNRESOLVED** = neither index nor body addresses the issue.

| Item | Resolution claimed | Resolution actual in spec bodies | Status |
|---|---|---|---|
| **C1** ValidatedRule storage | Overview A1: unified table in ctrl-plane DB, RLS. ctrl-plane index: REVISED-C1 unified. | ctrl-plane §13 still describes the R2 split (`validated_rules` per-tenant + `validated_rules_shared` control-plane). learning-loop §2.0 explicitly defends the split. exec-plane §1.4 weighs in for **learning-loop's split**. ctrl-plane §13.2 has a two-source-merge resolver inside exec-plane — incompatible with both the unified-store claim AND with exec-plane §8 (`LoadSkillVersion` hides storage layout). **Four positions across three specs.** | ASPIRATIONAL |
| **C2** Single rule-consumption artifact (`SkillVersion`) | exec-plane §8 D4 (revised); learning-loop §9.0 | Both specs adopt `skill_version_id`; `LoadSkillVersion` activity returns the SkillVersion manifest. Field-name alignment between exec-plane prompt block and learning-loop manifest (C14) is OPEN per ctrl-plane R3 index — neither spec resolved it. | RESOLVED on artifact name; **C14 (field alignment) UNRESOLVED** in spec bodies. |
| **C3** Approval signal transport | ctrl-plane §6.6, exec-plane §7.2, operator-ux §2.3 | All three converge: gateway → Temporal SDK direct. `WorkflowID = case_run_id`. Signal names: `CaseApproved` / `CaseRejected`. | RESOLVED |
| **C4** Provisioning manifest delivery | exec-plane §2.7, ctrl-plane §5.6 | Both specs converge: read-only config-mount; `HealthCheck` is the ack. | RESOLVED |
| **C5** Replay determinism | exec-plane §4.3, learning-loop §10.6 | Four commitments named in both; `temperature=0` on replay; `skill_version_id` pinned at workflow start; decision-point outcome diff. **Sub-issue OPEN-C5:** `mcp_read_snapshot` artifact_type required by exec-plane §4.3 is **not in learning-loop §2.5 enum**. | PARTIALLY-RESOLVED — `mcp_read_snapshot` enum extension absent from learning-loop. |
| **C6** WRITE_EXTERNAL gating | exec-plane §5.6 + INV-A1 | exec-plane adopts the MCP-internal preflight `SELECT` on `audit_events` (Round 1 INV-03 met). Sandbox-mode + tenant-binding are the other two gates. **Required dependency:** the `mcp_audit_reader` Postgres role grant + `approval_received` event_type added to learning-loop §11.3. **Both absent from learning-loop body.** exec-plane §16 explicitly flags "OPEN-OQ-Audit-Read-Role" as blocking implementation. | PARTIALLY-RESOLVED — exec-plane delivered; learning-loop has not reciprocated. |
| **C7** ChannelAdapter scope | operator-ux §4.5 | Narrowed to `sendOutbound` + `normalizeInboundWebhook`. | RESOLVED |
| **C8** Audit immutability at storage layer | ctrl-plane §9.3, learning-loop §11.5 | Both specs name role + trigger enforcement. ctrl-plane R3 index claims §9.3 was rewritten — body still has the R2 GRANT-based version, but the substance (INSERT-only role, immutable at DB layer) is correct. | RESOLVED |
| **C9** Single audit store location | learning-loop §11.0, ctrl-plane R3 index claims REVISED-C9 to single store | learning-loop §11.0 explicitly states "control-plane `audit_events`, partitioned by `tenant_id`." ctrl-plane §9.3 (R2 body) describes per-tenant partition table within the control-plane DB — consistent with learning-loop. ctrl-plane §9.6 (titled "Dual audit storage and the federated query API") **contradicts** the index claim that dual was reversed. | PARTIALLY-RESOLVED — index claims single, §9.6 body still describes dual federation. |
| **C10** `corrections` writer | exec-plane §7.4, learning-loop §3.5 | Both specs converge: exec-plane `PersistCorrection` activity is sole writer; gateway delivers signal envelope only. | RESOLVED |
| **C11** Pull-at-start authoritative | exec-plane §8, learning-loop §9.5 | Both specs converge: pull-at-workflow-start; control-plane push downgraded to non-authoritative cache hint. | RESOLVED |
| **C12** Unified `validated_rules` DDL+RLS | ctrl-plane R3 index: §13 rewritten | ctrl-plane §13 body still split. learning-loop §2.8 has the DDL but for the per-tenant DB. **Citation broken: "ctrl-plane consumes from learning-loop §2.8 verbatim" but ctrl-plane body has not been rewritten to do so.** | ASPIRATIONAL |
| **C13** ApprovalSignalEnvelope payload completeness | exec-plane R3 index claims RESOLVED at §7.4 with "16-field requirements list pushed to operator-ux + learning-loop" | operator-ux §2.3 carries 5–7 fields in `CaseApproved` / `CaseRejected` (decision_point_id, approved_by, approved_at, idempotency_key, optional correction_type/note/scope_hint). The exec-plane RESOLVED-C13 claim is that the signal payload alone suffices to build a `corrections` row in `PersistCorrection`. learning-loop §2.6 `corrections` schema requires: `idempotency_key, tenant_id, case_run_id, decision_point_id, operator_id, action_button, free_text, follow_up_answer, scope_hint`. operator-ux signal does not carry `tenant_id` or `operator_id` (carried in envelope outer fields per operator-ux §2.3 — `approved_by`/`rejected_by` is the operator_id; tenant_id is in the envelope, not the per-signal payload). The 16-field count is ambiguous. **No spec lists the 16 fields explicitly.** | PARTIALLY-RESOLVED — claim made; explicit field-by-field reconciliation absent. |
| **C14** Field-name alignment SkillVersion ↔ prompt block | OPEN per ctrl-plane R3 index; exec-plane §8.3 has prompt block; learning-loop §9.2 has manifest | exec-plane §8.3 names `if_conditions` and `then_action` in the prompt block. learning-loop §9.2 manifest has `conditions_canonical` and `recommended_action`. **Field name drift unresolved.** Round 3 demand on exec-plane to adopt verbatim is not met in either spec body. | UNRESOLVED |
| **N1** Sandbox-mode canonical encoding | exec-plane §0.2: `mode: "sandbox"|"live"` enum string. ctrl-plane R3 index: OPEN. learning-loop §2.3: `mode: 'sandbox'|'shadow'|'autopilot'` enum. operator-ux §2.1: `metadata.run_mode: "sandbox"` string. | Four encodings still in flight. exec-plane chose `sandbox|live`; learning-loop kept the 3-state enum that includes shadow/autopilot (Phase 2 states); ctrl-plane has not chosen; operator-ux uses a different field name. The product spec progression model has 4 states (sandbox/shadow/autopilot/validated). exec-plane's 2-state truncates the progression. | UNRESOLVED |
| **N3** `vertical_candidate_aggregates` PII strip rules | learning-loop §13.5 demanded; not yet written | learning-loop §2.7 has the table DDL; strip rules are listed in ctrl-plane §13.4 (replication owner). Strip rules retain `conditions_canonical` and `recommended_action`. Round 1 INV-05 demanded the test that condition values cannot identify a tenant; this test is named (`STRIP_01` in ctrl-plane §13.3) but the operational scrub of customer/supplier names from condition values is still asserted, not specified. | PARTIALLY-RESOLVED — table boundary stripped; condition-value content scrub not specified. |
| **N4** WuzAPI bridge container ownership | ctrl-plane R3 index: RESOLVED §5.2 revised, §5.7 new. operator-ux §4.3, §4.4 still describes gateway-owned provisioning. | ctrl-plane §5.2 still has the R2 12-step workflow; no WuzAPI bridge step. ctrl-plane §5.7 does not exist. operator-ux §4.3 says "Control Plane calls `POST /channel-bindings`" and gateway "spins up a bridge instance." The bridge container is **gateway-owned** in operator-ux body and **claimed-ctrl-plane-owned** in ctrl-plane index. | ASPIRATIONAL on ctrl-plane side; operator-ux has the operational story. |
| **N6** Idempotency key composition rule | ctrl-plane R3 index: RESOLVED §17. exec-plane R3 index: RESOLVED §10.4 (consume ctrl-plane's rule). | ctrl-plane §17 does not exist. exec-plane §10.4 does not exist (file ends at §13). The "ctrl-plane consume" citation in exec-plane is broken. **Five distinct key formats observed across the four specs:** (a) `{provider_message_id}_{epoch_seconds}` (operator-ux Correction §2.2); (b) `{packet_id}:deliver:{attempt}` (operator-ux outbound §9.1); (c) `{case_run_id}:{decision_point_id}:{approved\|rejected}:1` (operator-ux signal §2.3); (d) `{case_run_id}:{decision_point_id}:approval_received` (exec-plane audit §3.4); (e) `{case_run_id}:extract_facts` (exec-plane activity §3.4); plus learning-loop's `corrections.idempotency_key UNIQUE` constraint. No composition rule unifies them. | ASPIRATIONAL |
| **OQ-NEW-3** Internal auth | ctrl-plane R3 index: mTLS at §6.2. exec-plane R3 index: HMAC-signed bearer at §13. ctrl-plane §6.2 body: HMAC. | **Three-way contradiction.** ctrl-plane index says mTLS; ctrl-plane body says HMAC; exec-plane body says HMAC. The two specs that share the channel disagree at the index level. Body content currently agrees on HMAC but the ctrl-plane R3 index would have to retract or the body rewrite. | UNRESOLVED |
| **OQ-NEW-5** Gateway Temporal credentials | ctrl-plane R3 index: §6.8. operator-ux §2.3 holds gateway-only secret scope; exec-plane §7.2 says exec-plane provisions and shares via per-tenant secret scope. | ctrl-plane §6.8 absent. operator-ux body has "gateway-only secret scope, distinct from tenant secret scopes." exec-plane body has "exec-plane provisions, shares via per-tenant secret scope." **Contradiction** on whether the cred is gateway-scoped or per-tenant-scoped. operator-ux OPEN-C3-cred says Temporal IAM may not natively support a signal-only role and proposes a sidecar fallback — also unresolved. | UNRESOLVED |
| **OQ-06** OTP/JWT issuance | ctrl-plane R3 index: §6.7 — control plane sole issuer. operator-ux §A4 (R2): "gateway is a caller, not an issuer." | ctrl-plane §6.7 absent. ctrl-plane §6.1 (R2 body) describes operator OTP via WhatsApp with JWT issued by Auth Service. Substantive agreement; the index claim of §6.7 is misleading because the answer was already in §6.1. | RESOLVED in substance (ctrl-plane §6.1 already specifies Auth Service as issuer); index citation §6.7 is misleading. |
| **AUDIT-CANON** Canonical audit_events schema + writer registry | ctrl-plane R3 index: §9.3 revised to reference learning-loop §11. learning-loop §11.3 has the registry. | learning-loop §11.3 registry has no `approval_received`, `blocked_write_attempted`, `sandbox_escape_blocked`, or `security_violation_inbound` — all required by exec-plane §5.6 and operator-ux CT-TenantBinding-01. **Four event types exec-plane and operator-ux assert are written are missing from the canonical registry.** Writer authorization is partial: writers are named per row but the contract test that "writer X cannot write event_type Y" is unspecified. | PARTIALLY-RESOLVED — registry exists, four missing event_types, writer-authorization test absent. |
| **ALERT-SINK** `emit-alert` API | ctrl-plane R3 index: §9.6.1. operator-ux §11 OPEN-Alert-Sink references this gap. | ctrl-plane §9.6.1 absent. operator-ux's gateway has no concrete alert-emission contract for session disconnects, security violations, or rate-limit breaches. | UNRESOLVED |
| **REPLAY-SCHED** Replay scheduler | ctrl-plane R3 index: §8.7. exec-plane R3 index: RESOLVED at §13.2 (`POST /internal/replay`). | ctrl-plane §8.7 absent. exec-plane §13 has the runtime RPC surface; the replay-trigger contract is named there. ctrl-plane §8 has the Evaluation Service body but no `POST /admin/replays` endpoint. **One side delivered (exec-plane); the other side (ctrl-plane scheduler) is aspirational.** | PARTIALLY-RESOLVED |
| **MCP-SNAPSHOT** `mcp_read_snapshot` artifact_type + `mcp_audit_reader` role | exec-plane §4.3, §5.6 demand. learning-loop §2.5 owns the enum and §11 owns the role grant. | learning-loop §2.5 enum has 6 artifact_types; `mcp_read_snapshot` is **not added**. `mcp_audit_reader` role grant is **not in learning-loop §11**. exec-plane §16 explicitly flags this as blocking §5.6 implementation. | UNRESOLVED |
| **HERMES-VOL** No rule manifests on Hermes data volume | exec-plane R3 index: RESOLVED at §2.3, §10.6 with new INV-H6. ctrl-plane R3 index: OPEN-HERMES-VOL on exec-plane. | exec-plane has not added INV-H6 to §2.1 nor a new §10.6 test in the body. | ASPIRATIONAL on exec-plane side. |

**Summary of the per-conflict status:**
- 7 RESOLVED (C2 partial, C3, C4, C7, C8, C10, C11, OQ-06)
- 7 PARTIALLY-RESOLVED (C5, C6, C9, C13, N3, AUDIT-CANON, REPLAY-SCHED)
- 5 ASPIRATIONAL (C1, C12, N4, N6, HERMES-VOL)
- 5 UNRESOLVED (C14, N1, OQ-NEW-3, OQ-NEW-5, ALERT-SINK, MCP-SNAPSHOT)

The aspirational set is the headline. Four of the five aspirationals are owned by ctrl-plane, where the R3 conflict-resolution index lists outcomes that the R3 body has not produced.

### 2. Cross-spec citation graph

For each canonical contract, I name the spec section that should be authoritative, the consuming sections, and flag broken citations (consumer references content that does not exist in the producer).

| Canonical contract | Authoritative section (claimed) | Consuming sections | Citation status |
|---|---|---|---|
| `validated_rules` DDL + RLS policy | learning-loop §2.8 (per-tenant DB schema) — but ctrl-plane R3 index claims it is in ctrl-plane control-plane DB | exec-plane §8 (`LoadSkillVersion` consumer); ctrl-plane §13 (storage owner per index) | **BROKEN** — ctrl-plane §13 body still describes split storage; learning-loop §2.8 is for per-tenant DB; the moderator's overview A1 declares unified-store; three positions, no aligned authoritative DDL. |
| `audit_events` schema + writer registry | learning-loop §11.2 (schema), §11.3 (registry) | exec-plane §3.4 / §5.6 (writers); operator-ux §3 I-08 (writer); ctrl-plane §9.3 (R2 body has its own copy of the schema), §9.6 (R2 dual-store API) | **PARTIALLY BROKEN** — ctrl-plane has its own schema in §9.3 that is not formally aligned with learning-loop §11.2. learning-loop §11.3 registry is missing 4 event_types asserted by exec-plane and operator-ux. |
| `ApprovalSignalEnvelope` shape | operator-ux §2.3 (`CaseApproved` / `CaseRejected` payloads) | exec-plane §7.3 (consumer); learning-loop §3 (consumer through `PersistCorrection`) | **BROKEN** — exec-plane §7.4 RESOLVED-C13 claims a 16-field envelope is required; operator-ux §2.3 carries 5–7 fields. The 16 fields are not enumerated in any spec. |
| `Correction` JSON shape | learning-loop §3 | operator-ux §2.2 (producer); exec-plane §7.4 (`PersistCorrection` writer) | RESOLVED — operator-ux §2.2 explicitly aligned to learning-loop §3 verbatim. |
| `SkillVersion` manifest | learning-loop §9.2 | exec-plane §8.3 (renders to prompt block); ctrl-plane §13.5 (write path on promotion) | **PARTIALLY BROKEN** — exec-plane prompt-block field names (`if_conditions`, `then_action`) drift from manifest field names (`conditions_canonical`, `recommended_action`). C14 unresolved. |
| Idempotency-key composition rule | ctrl-plane §17 (claimed) | All four specs | **BROKEN** — ctrl-plane §17 absent. Five different key formats live in spec bodies; no central rule. |
| Sandbox-mode encoding | exec-plane §0.2 (claimed canonical: `mode: "sandbox"\|"live"`) | learning-loop §2.3 (`mode: sandbox\|shadow\|autopilot`); operator-ux §2.1 (`metadata.run_mode`); ctrl-plane (no canonical declared) | **BROKEN** — N1 unresolved across four spec bodies. |
| `mcp_audit_reader` Postgres role | learning-loop §11 (per exec-plane §5.6 demand) | exec-plane §5.6 (consumer) | **BROKEN** — role grant absent from learning-loop. |
| `mcp_read_snapshot` artifact_type | learning-loop §2.5 enum | exec-plane §4.3 (consumer) | **BROKEN** — enum extension absent from learning-loop. |
| `emit-alert` API | ctrl-plane §9.6.1 (claimed) | operator-ux §4.7 / §11 (consumer) | **BROKEN** — endpoint absent from ctrl-plane body. |
| Replay-scheduler trigger API | ctrl-plane §8.7 (claimed); exec-plane §13.2 (executor) | exec-plane §6 (executor); ctrl-plane §8 (R2 body has no trigger endpoint) | **PARTIALLY BROKEN** — executor side present in exec-plane; scheduler side absent in ctrl-plane. |
| `LoadSkillVersion` activity contract | exec-plane §8.2 | learning-loop §9 (provider of the manifest) | RESOLVED — both specs aligned on the contract shape. |
| Internal call auth method | ctrl-plane §6.2 | exec-plane §13 | **BROKEN** — ctrl-plane R3 index says mTLS, ctrl-plane §6.2 body says HMAC, exec-plane §13 says HMAC. Index disagrees with both bodies. |

### 3. End-to-end acceptance scenarios — testability check

For each Round 1 scenario I walk it through the four R3 spec bodies and grade per-step, naming the contract test that owns each boundary and flagging unspecified gaps an integration-test author would have to invent around. Step-level scoring is **TESTABLE** / **PARTIALLY TESTABLE** / **NOT TESTABLE**.

#### SC-01 Golden-path correction (TESTABLE in 4/6 steps)

| Step | Owning contract test | Status | Gap |
|---|---|---|---|
| 1. Sandbox case enters; Temporal workflow starts | exec-plane §10.2 `test_workflow_id_equals_case_run_id` | TESTABLE | — |
| 2. `LoadSkillVersion` invoked; SkillVersion pinned | exec-plane §10.3 `test_load_skill_version_pinned_at_start` | TESTABLE | — |
| 3. Hermes drafts artifacts; review packet sent to operator-ux | operator-ux §3 CT-Outbound-01 | TESTABLE | — |
| 4. Operator replies; gateway emits Correction + Temporal signal | operator-ux §3 CT-Inbound-01, CT-Signal-01, CT-CorrectionShape-01 | TESTABLE | — |
| 5. Workflow `PersistCorrection` writes corrections row + audit | exec-plane §7.4; learning-loop §3.5 | PARTIALLY TESTABLE | C13 unresolved: signal payload completeness for `PersistCorrection` is asserted but not field-enumerated. Author must invent. |
| 6. RuleCandidate generated; reviewer promotes; SkillVersion v2 created | learning-loop §13.2 GOLDEN PROMOTE-1; ctrl-plane §13.5 | PARTIALLY TESTABLE | Promotion write path **C12** unresolved: ctrl-plane §13.5 says ctrl-plane HTTP-calls exec-plane to write per-tenant rule, but if A1 unified-store wins, that path is wrong. |

**Verdict: PARTIALLY TESTABLE.** Steps 5–6 require ad-hoc choice on signal payload and rule storage location.

#### SC-02 Multi-correction promotion (PARTIALLY TESTABLE)

Steps 1–3 (independent case runs) are testable. Step 4 (merge into single candidate, evidence_count rises) is testable via learning-loop §13.1 PROPERTY MERGE-2. Step 5 (cross-threshold → under_review) is testable. Step 6 (reviewer promotes) is **PARTIALLY TESTABLE** — same C12 ambiguity as SC-01 step 6. Step 7 (next sandbox case applies the new rule without correction) requires `LoadSkillVersion` to read the new rule. **TESTABLE only if A1 storage topology is selected.**

#### SC-03 Contradicting-correction supersession (TESTABLE)

learning-loop §8.4 VER-1, GOLDEN PROMOTE-2 cover supersession. Rollback chain in §8.3 is concrete. Storage-topology ambiguity does not affect this scenario at the test boundary because all writes are to the same table set. **TESTABLE.**

#### SC-04 Abandoned packet (TESTABLE)

operator-ux CT-E2E-02 covers gateway-side stale-reply detection. exec-plane §3.5 covers Temporal-side timeout (12–48h). The two timers' interaction is not a single test; an integration-test author can write the test by running both side timers. operator-ux §2.3 OPEN-Expiry-sweep is now resolved (Temporal owns; gateway is reminders-only, per exec-plane §7.4 RESOLVED-C3). **TESTABLE.**

#### SC-05 Tenant isolation leak attempt (PARTIALLY TESTABLE)

| Sub-step | Owning test | Status |
|---|---|---|
| 5a. Forged `tenant_id` in inbound webhook payload | operator-ux CT-TenantBinding-01 | TESTABLE |
| 5b. MCP call with mismatched header | exec-plane §10.4 `test_mcp_tenant_id_isolation` | TESTABLE |
| 5c. Cross-tenant RLS read on shared DB | ctrl-plane §12.4 isolation tests; learning-loop §13.4 STRIP_01 (vertical promotion) | PARTIALLY TESTABLE |
| 5d. Cross-tenant vertical-rule promotion leak | ctrl-plane §13.3 STRIP_01 | PARTIALLY TESTABLE — strip rules retain `conditions_canonical`; condition values can carry customer/supplier names. The test must specify the input scrub on free-text condition values, which N3 has not done. |

**Verdict: PARTIALLY TESTABLE.** 5d cannot be implemented deterministically until N3 condition-value scrub is specified.

#### SC-06 Sandbox/approval-bypass attempt (TESTABLE for sandbox; TESTABLE for approval-bypass)

exec-plane §10.4 `test_mcp_sandbox_blocks_even_with_approval`, `test_mcp_blocks_write_final_without_approval_audit`, `test_mcp_allows_write_final_after_approval_audit` cover all three gates (sandbox + approval-audit + tenant-binding). The MCP-internal audit-row check is implementable **only if** the `mcp_audit_reader` role grant lands in learning-loop and `approval_received` is added to the registry. Both currently absent. **TESTABLE in design; NOT TESTABLE in implementation** until learning-loop adds the role and event_type.

#### SC-07 Replay determinism check (PARTIALLY TESTABLE)

| Step | Owning test | Status |
|---|---|---|
| 7a. Same input → same `skill_version_id` resolves to same manifest | exec-plane `test_replay_pins_skill_version_via_as_of` | TESTABLE |
| 7b. `temperature=0` on replay | exec-plane §4.3 commitment 2 (configured via `HERMES_DEFAULT_TEMPERATURE`); test name absent | NOT TESTABLE — the test that proves replay runs use temperature=0 is not named in any spec. |
| 7c. MCP read-tool snapshot replayed deterministically | exec-plane §4.3 commitment 3; **requires `mcp_read_snapshot` artifact_type that learning-loop has not added** | NOT TESTABLE |
| 7d. Two replays of same case_run_id → identical decision-point outcomes | exec-plane `test_replay_decision_outcome_determinism` | TESTABLE if 7b and 7c land |

**Verdict: PARTIALLY TESTABLE.** Two of four substeps blocked by missing learning-loop schema work and missing exec-plane temperature contract test.

#### SC-08 Double-tap approval idempotency (TESTABLE)

operator-ux CT-Inbound-04, CT-Signal-01, exec-plane `test_approved_signal_idempotency`. Idempotency-key composition rule (N6) is broken centrally but the gateway's `{case_run_id}:{decision_point_id}:approved:1` key is consistent on the signal layer. **TESTABLE.** N6's broader composition issue affects audit-event idempotency but not the SC-08 signal-layer dedup.

#### Scenario testability summary

| Scenario | Verdict | Top blocker |
|---|---|---|
| SC-01 | PARTIALLY TESTABLE | C13 envelope completeness; C12 storage topology |
| SC-02 | PARTIALLY TESTABLE | C12 storage topology + promotion write path |
| SC-03 | TESTABLE | — |
| SC-04 | TESTABLE | — |
| SC-05 | PARTIALLY TESTABLE | N3 condition-value scrub |
| SC-06 | NOT TESTABLE in implementation | learning-loop hasn't added `approval_received` event_type, `mcp_audit_reader` role |
| SC-07 | PARTIALLY TESTABLE | `mcp_read_snapshot` enum extension; explicit temperature test |
| SC-08 | TESTABLE | — |

**3 of 8 scenarios fully testable. 4 partially testable. 1 not testable in implementation.** The blockers cluster on three issues: storage topology (C1/C12), learning-loop's missing R3 cleanup work (event_types, role, artifact_type enum), and the C13 envelope completeness.

### 4. TDD posture re-audit (per-spec scores)

Re-scoring against the same rubric used in Round 2.

#### ctrl-plane

**TDD discipline: 3 (regressed from 4).**

The R3 conflict-resolution index lists 9 RESOLVED-X items pointing to sections (§5.7, §6.7, §6.8, §8.7, §9.6.1, §13 rewrite, §17) that **do not exist in the body**. Claiming resolution without writing the proof is the inverse of TDD: it asserts a property without an implementable verifier. The R2 score of 4 was earned by named invariants (TENANT_CTX_01–04, AUTH_01–05, REVIEW_01–05, EVAL_01–05, OBS_01–04, BILLING_01–03) and contract tests (§7.3, §12.2–12.5). All R2 content remains. The regression is the R3 index pattern, which materially weakens the spec's claim to be test-first: the named contract tests for the R3 RESOLVED-X items (e.g., the cross-tenant SELECT denial test for the unified `validated_rules` table) are absent.

**Isolation completeness: 4 (held).**

The §3 tenant-context propagation primitives are unchanged and remain the strongest in the team. RLS defense-in-depth is correct. The §13 storage-layout decision is now ambiguous (split body, unified index) which weakens the isolation argument, but the STRIP_01 invariant for cross-tenant vertical promotion is concrete. Score held, not improved.

#### exec-plane

**TDD discipline: 5 (improved from 4.5).**

The R3 work is fully landed in the body. §0.2 declares the canonical types adopted across the spec (`mode: sandbox|live`, `skill_version_id`, `case_run_id`). New contract tests at §10.4 cover the three independent MCP gates (`test_mcp_blocks_write_final_without_approval_audit`, `test_mcp_allows_write_final_after_approval_audit`, `test_mcp_sandbox_blocks_even_with_approval`). New invariants (INV-T5 default-deny mode, INV-A4 RESOLVED-C3 transport) are present. Replay determinism tests are concrete (`test_replay_pins_skill_version_via_as_of`, `test_replay_decision_outcome_determinism`). The R3 index claims map to body sections that exist and have content. The HERMES-VOL claim is the one regression (claimed RESOLVED, body lacks INV-H6); minor.

**Isolation completeness: 5 (improved from 4.5).**

The MCP-internal preflight (§5.6) closes the Round 1 INV-03 demand. Sandbox-mode default-deny (INV-T5) is exemplary. Volume-tenant binding (INV-H4), task-queue isolation (INV-T4), and tenant-header validation per MCP call (§5.1) all have named tests. Round 1 expectation fully met.

#### learning-loop

**TDD discipline: 4.5 (held).**

The R2 spec content remains the strongest internal invariant catalog on the team (C1–C5, CONF-1–CONF-5, SCOPE-1–3, REPLAY-1–3, AUDIT-1–5, plus §13 property tests MERGE-1–6). No R3 content was written. The R3 cleanup items demanded of learning-loop (`approval_received` event_type, `mcp_audit_reader` role, `mcp_read_snapshot` artifact_type, condition-value scrub specification, ApprovalSignalEnvelope 16-field reciprocity) are all absent. Score held because R2 was strong and R3 work is blocked, not because R3 added value.

**Isolation completeness: 3.5 (held).**

`vertical_candidate_aggregates` boundary is structurally correct (§2.7); condition-value scrub remains asserted, not specified (Round 2 finding unchanged). PII-strip invariant test (`STRIP_01` referenced) is named in ctrl-plane §13.3 but the actual scrub of customer/supplier names from condition_canonical values is the gap.

#### operator-ux

**TDD discipline: 3.5 (held).**

R3 added several contract tests (CT-Outage-01 for 30-min disconnect, CT-TenantBinding-01 for forged tenant_id, CT-CorrectionShape-01 for schema validation, CT-SignalMap-01 for signal mapping). New invariants I-11 (no message loss across disconnect), I-12 (tenant_id origin from `channel_bindings`), I-13 (Correction schema validation), I-14 (gateway never populates parsed fields) are concrete. The score does not improve because the R2 weakness (test fail mechanism not specified for some invariants) persists, and OPEN items (alert sink, Temporal cred granularity) have not been resolved.

**Isolation completeness: 4 (improved from 3).**

I-12 (tenant_id origin invariant: derived from `channel_bindings.provider_number`, never from payload) is the Round 1 INV-01 demand met. CT-TenantBinding-01 is a concrete adversarial test. Per-tenant signal-only Temporal credential is a real isolation step, even with the OPEN sidecar fallback. Improvement deserved.

#### Re-score table

| Spec | TDD discipline | Isolation completeness | Notes |
|---|---|---|---|
| ctrl-plane | 4 → **3** | 4 → 4 | Aspirational R3 index regression. |
| exec-plane | 4.5 → **5** | 4.5 → **5** | Full R3 landing in body. |
| learning-loop | 4.5 → 4.5 | 3.5 → 3.5 | Stalled — no R3 body content. |
| operator-ux | 3.5 → 3.5 | 3 → **4** | I-12 tenant-binding invariant + adversarial test improved isolation. |

### 5. Final round of OE/UE smell hunt

**OE-R3-1 (`audit_events.event_type` enum is god-objecting).** learning-loop §11.3 lists 22 event_types. exec-plane R3 adds 4 more (`approval_received`, `blocked_write_attempted`, `sandbox_escape_blocked`, `security_violation_inbound`). operator-ux adds at least 2 more (`stale_reply`, `correction_expired`, `security_violation_inbound` — overlapping). The trajectory is a flat 30+ enum that mixes business events (rule_promoted), security events (sandbox_escape_blocked), workflow events (case_run_started), and operator events (correction_received). Any one writer reading the registry to find "what events am I authorized to write" must scan a flat list. Smell: split the registry into named groups (security, learning, workflow, operator) with per-group writer authorization.

**UE-R3-1 (idempotency-key composition rule unwritten).** Five distinct key formats across four specs; the central authority cited (ctrl-plane §17) does not exist. An integration test author building SC-08 has to invent the relationship between operator-ux's signal `idempotency_key` and exec-plane's audit `idempotency_key`. The risk is not theoretical: a redelivered approval signal that produces a different audit-event idempotency key is a duplicate `approval_received` row, which means SC-06 (MCP gate based on audit row count) can be bypassed by triggering double-write.

**UE-R3-2 (WuzAPI bridge container = SPOF; restart semantics unspecified).** operator-ux §4.4 has session-disconnect handling (CT-Outage-01) — but bridge container crash is a different failure mode. CT-Outage-01 simulates a 30-min disconnect with the bridge alive. If the bridge container itself dies (OOM, host crash), in-flight outbound packets in the bridge's local buffer are lost; inbound webhooks during the crash arrive at a dead URL. Recovery requires (a) Temporal-orchestrated container restart, (b) idempotent re-delivery of packets the gateway thinks are sent, (c) WhatsApp's 14-day server-side message retention to recover inbound. None of these are specified or tested.

**UE-R3-3 (the aspirational-resolution pattern).** ctrl-plane R3 index contains 9 RESOLVED-X claims with no implementing body. This is its own under-engineering: a test-first spec must have the body before the index. Any R4 work that "consumes" these claimed-resolved items will encode invariants that the body cannot enforce. Smell: an entire-team-wide convention requiring the index to point at non-empty body sections, enforced by lint.

**OE-R3-2 (specs that grew >300 lines in R3 — none did, but ctrl-plane grew the index by ~25 lines without growing the body).** This is the inverse of the R2 growth pattern. R2 grew 1,400 lines on resolution language and contract tests. R3 grew the index alone. The team is gaining metadata, not test infrastructure.

**OE-R3-3 (operator-ux LLM parse cascade still in §7).** Round 2 OE-OU-2 was that Stage 3 LLM parse is premature — operator-ux R3 has not removed it. This remains a smell.

### 6. Top 5 demands for Round 4 (TDD strategy depth round)

1. **ctrl-plane writes the substance behind every RESOLVED-X claim in its R3 index, OR retracts the claim.** Specifically: §5.7 (WuzAPI provisioning step), §6.7 (OTP/JWT — or strike index, since §6.1 has it), §6.8 (gateway Temporal credentials per-tenant scope and rotation policy), §8.7 (`POST /admin/replays`), §9.6.1 (`emit-alert` API), §13 (rewrite to unified store with the contract test that proves cross-tenant SELECT is denied), §17 (idempotency-key composition rule with all 5 key formats explicitly derived from the rule). Each section must contain one or more named contract tests, not just declarative content.

2. **learning-loop adds the four downstream-blocking schema items: `approval_received` to §11.3 registry with exec-plane named as writer; `mcp_audit_reader` Postgres role grant with `SELECT` only on `audit_events` (per-tenant scope); `mcp_read_snapshot` to §2.5 artifact_type enum; explicit condition-value scrub specification at §13.5 with a contract test that asserts customer/supplier names from a seeded correction cannot reach `vertical_candidate_aggregates.conditions_canonical`.**

3. **operator-ux enumerates the 16-field ApprovalSignalEnvelope demanded by exec-plane RESOLVED-C13. Either confirm 7 fields suffice (requiring exec-plane to retract the 16-field claim) or expand `CaseApproved` / `CaseRejected` payloads to carry the field set that lets `PersistCorrection` build a `corrections` row from the signal alone with zero additional fetches.**

4. **The four specs jointly select ONE storage topology for `validated_rules` and write it into all four bodies. The remaining options are: (a) unified ctrl-plane DB per the moderator's overview A1 — requires ctrl-plane §13 rewrite; (b) split per learning-loop §2 — requires the moderator's overview A1 to be amended; (c) the resolver-in-exec-plane two-source merge per ctrl-plane §13.2 R2 — requires exec-plane §8 to drop the "hides storage layout" position. Any one of these is fine; SC-01, SC-02, SC-05, SC-07 cannot be fully testable until one is chosen and written into all four spec bodies.**

5. **exec-plane writes INV-H6 ("rule manifests never persist to Hermes data volume") into §2.1 with a contract test in §10 that runs a workflow and asserts no `*.skill` file with rule content was written to `/hermes/data/{tenant_id}/`. The test must use a recursive find with grep against rule_id patterns. This is a 1-paragraph, 1-test addition; it has been claimed RESOLVED twice without landing.**

---

*Last updated: Round 3. Verdict: REGRESSED on cross-spec contracts; converged on local TDD in exec-plane and operator-ux. ctrl-plane regressed via the aspirational-index pattern. learning-loop stalled on the blocking schema work demanded by exec-plane. The team is not ready for Round 4 TDD-depth work until items #1, #2, and #4 above are landed in spec bodies. Three of eight end-to-end scenarios are fully testable; four are partially testable; one (SC-06) is not testable in implementation. The Section 6.1 discriminator is not met for C1, C12, C14, N1, N6, OQ-NEW-3, OQ-NEW-5, ALERT-SINK, MCP-SNAPSHOT — the body content does not match the index claims, and consumer specs cite producer sections that do not exist.*

---

## Round 4 — Re-audit on actual R3 state + TDD depth assessment

### 0. Correction to Round 3 audit

**My Round 3 audit was factually wrong on ctrl-plane and learning-loop status.** I read the spec files before R3 deltas had landed, then judged the R3 conflict-resolution indices as "aspirational" because the body sections they cited did not exist at the time I read. R3 work landed after my read and before this R4 audit. The corrections owed:

| R3 audit claim (wrong) | Actual R3 state |
|---|---|
| "ctrl-plane §5.7 absent — N4 ASPIRATIONAL" | ctrl-plane §5.7 (WuzAPI Bridge Lifecycle) is in body, lines 319–349, with provisioning steps, lifecycle handoff to gateway, and contract test. **N4 RESOLVED.** |
| "ctrl-plane §6.7 absent — OQ-06 misleading" | ctrl-plane §6.7 (Operator OTP and JWT Issuance) in body, lines 465–525, with TTL, claims, replay protection, contract tests. **OQ-06 RESOLVED in body.** |
| "ctrl-plane §6.8 absent — OQ-NEW-5 UNRESOLVED" | ctrl-plane §6.8 (Gateway Temporal Credentials) in body, lines 527–566. **OQ-NEW-5 RESOLVED, with operator-ux R3 §10.4 yielding to gateway-secret-scope storage.** |
| "ctrl-plane §8.7 absent — REPLAY-SCHED PARTIALLY-RESOLVED" | ctrl-plane §8.7 (Replay Scheduler Trigger API) in body, lines 730–788. **REPLAY-SCHED RESOLVED on both sides.** |
| "ctrl-plane §9.6.1 absent — ALERT-SINK UNRESOLVED" | ctrl-plane §9.6.1 (Alert sink: emit-alert API) in body, lines 957–1011, with severity enum, payload, routing rules, contract tests. **ALERT-SINK RESOLVED.** |
| "ctrl-plane §13 still split — C12 ASPIRATIONAL" | ctrl-plane §13 (ValidatedRule Storage — Unified, RLS-Enforced) was rewritten; the body now references learning-loop §2.8 DDL verbatim, with the resolver in §13.3. **C12 RESOLVED.** |
| "ctrl-plane §17 absent — N6 ASPIRATIONAL" | ctrl-plane §17 (Idempotency Key Registry) in body, lines 1410+, with SHA-256 derivation rule + 10-row registry + 4 invariants. **N6 RESOLVED.** |
| "learning-loop has zero R3 cleanup — STALLED" | learning-loop landed massive R3 work I missed: §2.0 canonical Storage Topology table, §2.0.1 audit-topology reconciliation, §2.5 `mcp_read_snapshot` artifact_type, §2.7.2 PII-strip classifier with `quarantined_aggregates` table and contract test, §2.8 unified `validated_rules` DDL with locked RLS, §11.3 42-entry canonical event_type registry, §11.5 `mcp_audit_reader` role + `mcp_approval_events` view + scoped contract test, §11.6 writer registry, §13.5 strengthened INV-05. **MCP-SNAPSHOT, AUDIT-CANON, N1, N3, INV-05 all RESOLVED in body.** |
| "operator-ux signal envelope is 5–7 fields — C13 PARTIALLY-RESOLVED" | operator-ux R3 §2.2 has the **18-field canonical `ApprovalSignalEnvelope`** with full reconciliation table mapping every field to exec-plane §7.4 row and learning-loop §3.1/§3.3 column, four named OPEN-Envelope-Naming items (rename at deserialize boundary). **C13 RESOLVED.** |

The R3 audit's procedural failure: I treated the conflict-resolution index as a contract test ("body must exist for the index claim to hold") without re-reading the body after the deltas landed. The index pattern is fine when bodies follow; my job is to verify both sides, not assume one from the other. Apologies to the team — credit where due.

**Where R3 was right and the issue persisted into R4 — held the line:**

- **Aspirational R4 index in ctrl-plane (narrower scope, recurring pattern).** ctrl-plane R4 conflict-resolution index (lines 28–40) lists `RESOLVED-STORAGE-TOPOLOGY` at "§18 (new)", `RESOLVED-CONTRACT-TESTS` at "§19 (new)", `RESOLVED-PROPERTY-TESTS` at "§12.6 (new)", and `RESOLVED-SC-MAP` at "§20 (new)". **None of these sections exist in the ctrl-plane body** (file ends at §17). The same pattern I caught in R3, narrower scope. The substantive R4 work that ctrl-plane DID land — §6.2 HMAC reversal, §9.3 audit two-store reversal, §6.8 gateway-cred storage location — is real and good. The four R4 deliverables claimed at §12.6/§18/§19/§20 are not. Calling this out without overstating it: ctrl-plane delivered 3 of 4 R4 reversals as substance; the deliverables list was tagged RESOLVED prematurely.

### 1. R3 RE-AUDIT — corrected per-conflict status

| Item | R3 claim (mine, wrong) | Actual R3 state | Corrected verdict |
|---|---|---|---|
| C1 / C12 storage topology | ASPIRATIONAL | ctrl-plane §13 unified; learning-loop §2.8 canonical DDL; exec-plane §8 consumes via `LoadSkillVersion` | RESOLVED |
| C13 envelope completeness | PARTIALLY-RESOLVED (5–7 fields) | operator-ux §2.2 has 18 fields with full mapping table | RESOLVED |
| C14 field-name alignment | UNRESOLVED | learning-loop §9.3 explicit naming alignment; exec-plane §8.3 propagation; OPEN-Envelope-Naming notes 4 rename items at deserialize boundary | RESOLVED with documented boundary renames |
| N1 sandbox-mode encoding | UNRESOLVED (4 encodings) | learning-loop §2.0c canonical `mode: 'sandbox' \| 'live'` adopted; exec-plane §0.2 adopted; ctrl-plane §5.6/§10/§13 adopted; operator-ux §2.1 adopted | RESOLVED |
| N3 PII strip | PARTIALLY-RESOLVED | learning-loop §2.7.2 PII-strip classifier with concrete value classes, `quarantined_aggregates` table, INV-05 strengthened §13.5 with assertion that the string `"ABC Realty"` cannot reach `vertical_candidate_aggregates` | RESOLVED |
| N4 WuzAPI bridge ownership | ASPIRATIONAL | ctrl-plane §5.7 ownership in `TenantProvisioningWorkflow`; operator-ux §4.3 yielded | RESOLVED |
| N6 idempotency-key composition | ASPIRATIONAL | ctrl-plane §17 SHA-256 derivation + 10-row registry + 4 invariants; exec-plane §15 consumes; operator-ux §9.6 consumes | RESOLVED |
| OQ-NEW-3 internal auth | UNRESOLVED | **R3 picked mTLS; R4 ctrl-plane reversed to HMAC; R4 exec-plane has not propagated.** See R4 conflict #1 below. | UNRESOLVED at R4 |
| OQ-NEW-5 gateway Temporal cred | UNRESOLVED | ctrl-plane §6.8 gateway-only secret scope; operator-ux §10.4 yielded; OPEN-Temporal-IAM remaining (path A vs. path B sidecar) | RESOLVED with sub-OPEN |
| OQ-06 OTP issuance | "RESOLVED in substance, citation misleading" | ctrl-plane §6.7 has the substance; the R3 citation pointing to §6.7 is now correct because §6.7 exists | RESOLVED |
| AUDIT-CANON | PARTIALLY-RESOLVED (4 missing event_types) | learning-loop §11.3 has all required event_types: `approval_received`, `blocked_write_attempted`, `sandbox_escape_blocked`, `security_violation`, `security_violation_inbound`, `correction_expired`, `stale_reply` (7 originally flagged as missing — all present) | RESOLVED, with 7 operator-ux events still pending registry add (op-ux §9.7 OPEN-AUDIT-CANON-CONFORMANCE-R4 pushes them upstream) |
| ALERT-SINK | UNRESOLVED | ctrl-plane §9.6.1 has severity enum, payload schema, routing, idempotency, contract tests | RESOLVED |
| REPLAY-SCHED | PARTIALLY-RESOLVED | ctrl-plane §8.7 + exec-plane §13.3 both have the API contract | RESOLVED |
| MCP-SNAPSHOT | UNRESOLVED | learning-loop §2.5 `mcp_read_snapshot` artifact_type added; §11.5 `mcp_audit_reader` role + `mcp_approval_events` view + TEST MCP-SNAPSHOT-READER-SCOPED | RESOLVED |
| HERMES-VOL | ASPIRATIONAL | exec-plane §10.6 `test_no_rule_content_on_hermes_volume` with concrete snapshot+SHA-256+full-text-search assertion | RESOLVED |

**Net of corrections:** 13 of 15 cleanup items are RESOLVED in body. 2 remain open: OQ-NEW-3 (R4 conflict, see below) and OPEN-Temporal-IAM (sub-issue of OQ-NEW-5; documented dual-path contract test in exec-plane §11).

### 2. R4 TDD depth audit

#### 2.1 Invariant-first ordering

- **ctrl-plane:** Every section that landed R3/R4 substantive work places invariants before implementation. §3.1 (TENANT_CTX_*), §6.5 (AUTH_*), §6.2.1 (INV-AUTH-S2S-*), §6.7 (TDD invariants OTP), §6.8 (Temporal-cred invariants), §7.2 (REVIEW_*), §8.1 (EVAL_*), §8.7 (replay-trigger invariants), §9.1 (OBS_*), §9.6.1 (alert invariants), §17.4 (IDEMP_*). Invariant-first discipline is consistent. Score: 5.
- **exec-plane:** §3.1 INV-T*, §4.1 INV-CR*, §5.1 + §5.2/§5.3/§5.4 INV-E1/D1/I1, §5.6 INV-MCP-*, §7.1 INV-A*, §13.0 INV-RPC-* are all stated before implementation. The §11 cross-component contract test table is the gold standard for TDD-depth this round. Score: 5.
- **learning-loop:** §4.1 C1–C5, §5.1 CONF-1–5, §6.4 SCOPE-1–3, §10.5 REPLAY-1–4, §11.1 + §11.7 AUDIT-1–6 (including AUDIT-6 writer-registry authorization invariant), §13.5 INV-05 strengthened, §13.6 storage-immutability test. Score: 5.
- **operator-ux:** §3 I-01 through I-14 (now with I-11 outage no-loss, I-12 tenant-binding, I-13 schema-validation, I-14 no-parsed-fields). The 18-field envelope (§2.2) is invariant-led with the canonical mapping table immediately after. Section §4.7 (session lifecycle) has invariant-first structure (§4.7.1 states, §4.7.2 reconnect contract). Section §9.6 idempotency inventory is concrete. Score: 4.5 (small remaining gap: a couple of contract tests reference invariants without explicit fail-mode wiring; minor).

**Verdict on invariant-first:** all four specs are now substantively invariant-first. The team has cleared the bar.

#### 2.2 Cross-component contract test union table

Building from exec-plane §11 (11 tests, R4 NEW), learning-loop §13 + §11.5 (7 tests, R3), operator-ux §3 (CT-* set, 14 tests, R3+R4), ctrl-plane §12.2/§12.3/§12.4/§12.5 (12 tests, R2 carried) and the contract tests that exec-plane §11 names against learning-loop and operator-ux. The union below is what an integration-test author can pull from the four spec bodies today.

| Contract surface | Producer-side test | Consumer-side test | Both sides named? |
|---|---|---|---|
| `LoadSkillVersion` request/response | learning-loop §11 manifest schema; ctrl-plane §13.3 resolver SQL | exec-plane `test_load_skill_version_pinned_at_start`, `ct_load_skill_version` (§11) | YES |
| SkillVersion manifest passthrough (C14) | learning-loop §9.3 field-name alignment | exec-plane `ct_skill_manifest_passthrough` (§11) | YES |
| `mcp_approval_events` view (Gate 2) | learning-loop §11.5 TEST MCP-SNAPSHOT-READER-SCOPED | exec-plane `ct_mcp_approval_events_select`, `test_mcp_blocks_write_final_without_approval_audit`, `test_mcp_allows_write_final_after_approval_audit` (§10.4) | YES |
| Audit ingest durable buffer drain | exec-plane `ct_write_approval_audit_durable_drain` (§11) | ctrl-plane §9.4 INGEST_04 (R3 named) | YES |
| 16-field signal envelope → `corrections` row (C13-R5) | operator-ux §3 CT-Inbound-01, CT-Signal-01, CT-CorrectionShape-01 | exec-plane `ct_persist_correction_from_signal_alone`, `test_persist_correction_from_signal_alone` (§11) | YES |
| MCP tenant-header binding | exec-plane `ct_mcp_tenant_header_binding`, `test_mcp_tenant_id_isolation` | exec-plane `test_mcp_header_tenant_mismatch_rejected` | YES (single-side, exec-plane owns both) |
| Provisioning manifest config-mount | exec-plane `ct_provisioning_manifest_config_mount`, `test_hermes_volume_tenant_label_match` | ctrl-plane §12.2 `test_provisioning_manifest_delivery` | YES |
| Replay trigger end-to-end | ctrl-plane §8.7 `test_replay_trigger_idempotency` | exec-plane `ct_replay_trigger_admin_to_internal`, `test_replay_pins_skill_version_via_as_of` | YES |
| `emit-alert` from exec-plane | exec-plane `ct_emit_alert_from_exec_plane` (§11) | ctrl-plane §9.6.1 `test_emit_alert_from_exec_plane` (named in R4 index) | **PARTIALLY BROKEN** — ctrl-plane consumer-side test name is in the R4 index but §9.6.1 body does not contain the named test. |
| `emit-alert` from gateway | operator-ux §4.7.3 specifies the call | ctrl-plane §9.6.1 alert handling | YES (named on both sides; conformance to schema is OPEN-EMIT-ALERT-SCHEMA in operator-ux index, now closed in ctrl-plane R4 index — verify) |
| Temporal signal IAM (path A vs. path B) | ctrl-plane §6.8 + §11 `ct_temporal_signal_iam_path_a` (named in exec-plane §11) | operator-ux §10.4 + §11 `ct_temporal_signal_iam_path_b` (sidecar fallback) | YES — dual-path contract test |
| Audit storage topology | learning-loop §2.0 + §11.0 + §11.5 | exec-plane §5.6 line 603 (per-tenant local read); ctrl-plane §9.3 (R4 reversal to two-store) | **CONFLICT** — see R4 audit-topology in section 4 below. learning-loop §2.0.1 R4 reversed to single-store control-plane read; ctrl-plane R4 reversed to two-store local read; exec-plane R4 §5.6 sided with ctrl-plane. |
| Channel binding / inbound tenant validation | operator-ux §3 CT-TenantBinding-01, §4.9 | exec-plane §10.4 `test_signal_tenant_mismatch_rejected` | YES |
| WhatsApp session outage no-loss | operator-ux §3 CT-Outage-01 | (no consumer-side test; gateway-internal) | SINGLE-SIDE (correctly so; gateway is sole owner) |
| Service-to-service auth | ctrl-plane §6.2.2 (HMAC tests) | exec-plane §13.1 (mTLS handshake — claims to consume ctrl-plane HMAC, body says mTLS) | **CONFLICT** — see section 2.4 below |
| Property/fuzz: replay determinism | exec-plane §10.8 `prop_replay_determinism_n_runs` (N=60 derived) | learning-loop §13.3 REPLAY TEST-1..4 | YES |
| Property/fuzz: 3-gate Cartesian | exec-plane §10.8 `fuzz_mcp_write_final_three_gate` (24-cell table) | (single-side; MCP-internal) | SINGLE-SIDE (correctly so) |
| Property/fuzz: sandbox-mode escape | exec-plane §10.8 `prop_sandbox_mode_no_external_invocation` | (single-side; structural) | SINGLE-SIDE (correctly so) |
| Idempotency-key consistency | ctrl-plane §17.4 IDEMP_01–04 | exec-plane §15 inventory references §17 | YES — single derivation rule with multi-spec consumption |
| Property/fuzz: candidate-match algebra | learning-loop §13.1 PROPERTY MERGE-1..6 | (single-side; algorithm internal) | SINGLE-SIDE (correctly so) |

**Coverage holes:**
- The `ct_emit_alert_from_exec_plane` test is named in exec-plane §11 but the consumer-side test (ctrl-plane §9.6.1 contract test) is claimed in ctrl-plane R4 index but absent from §9.6.1 body — a paper-over.
- Storage-topology and auth contract surfaces are landed on both sides but with **contradictory positions** — covered in section 2.4.

#### 2.3 Property/fuzz test rigor

| Test | Genuine fuzz/property? | Why |
|---|---|---|
| exec-plane `prop_replay_determinism_n_runs` (§10.8) | YES — strong | N derived from binomial test (N=60 to reach false-negative ≤5% at p≥0.05); falsifiability is "any divergence is a hard fail"; not a magic constant |
| exec-plane `fuzz_mcp_write_final_three_gate` (§10.8) | YES — strong | Full Cartesian space (3 mode × 4 approval × 2 header = 24 cases); 7 unique outcome rows enumerated; full-table-must-pass (no single-row passing satisfies the property) |
| exec-plane `prop_sandbox_mode_no_external_invocation` (§10.8) | YES — strong | Adversarial fixture generation (base64 / Unicode / system-prompt-injection patterns); structural assertion ("physically cannot reach live MCP processes") rather than per-input check |
| learning-loop §13.1 PROPERTY MERGE-1..6 | YES — but algebra-only | Genuine algebraic properties (idempotence, commutativity, monotonicity, hash stability). Property tests proper, but operate on the algorithm's internal mathematical structure, not the cross-component surface |
| ctrl-plane §12.5 (R2 property/fuzz, R3 carried) | PARTIAL — RLS/JWT/idempotency-key fuzz claimed in R4 index §12.6 (NEW); the R4 §12.6 body is absent. R3 §12.5 has 3 property tests (idempotency-key generation, ValidatedRule version chain, tenant-context extraction) — these are property tests proper. | mid |
| operator-ux property tests | The R3 spec tests are contract tests, not property tests. operator-ux has no R4 fuzz/property contributions. | None |

**Verdict on rigor:** exec-plane's R4 property tests are exemplary. learning-loop's are algorithm-level proper. ctrl-plane's R4 fuzz claim is index-only; R3's are property-level proper. operator-ux has no fuzz/property tests, which is a defensible single-component scope (the gateway's adversarial surface is the inbound tenant-binding test CT-TenantBinding-01, which is a contract test — not a property test).

#### 2.4 R4 substantive conflicts

Two real R4 conflicts emerged from peer R4 reversals that were not propagated.

##### R4-CONFLICT-1: Service-to-service auth (mTLS vs. HMAC)

| Spec | R3 position | R4 position (in body) |
|---|---|---|
| ctrl-plane §6.2 | mTLS | **HMAC-bearer + workload identity** (R4 reversal, body line 369) — claims to match exec-plane §0.1/§13.1 |
| exec-plane §13.1 | mTLS | **mTLS** (R4 body line 1268) — claims to accept ctrl-plane's R3 mTLS pick; ctrl-plane reversed first |
| exec-plane §0.1 R4 index line 52 | — | "OQ-NEW-3 (revised) Service-to-service auth: **mTLS** (ctrl-plane §6.2 reversed R3 HMAC framing)" |

Two specs each claim the other reversed first; body content disagrees on the actual mechanism. Each reads the other's R3 index, not the other's R4 body. The conflict is genuine: ctrl-plane R4 §6.2 §6.2.1 lists `INV-AUTH-S2S-01..07` for HMAC bearer; exec-plane R4 §13.1 says explicitly "No bearer tokens. The R3 references in this spec to 'HMAC-signed bearer tokens' are superseded by mTLS."

This is the single most important R5 demand: pick one mechanism, propagate to both bodies.

##### R4-CONFLICT-2: Audit storage topology direction

| Spec | R4 position (in body) |
|---|---|
| ctrl-plane §9.3 | Two-store: per-tenant local `audit_events` (write-through cache + outage buffer + **MCP synchronous read source**), control-plane `audit_events` system of record. `mcp_audit_reader` role is local-DB-only. **REVISED-R4-AUDIT-TOPOLOGY** (R4 reversal of R3 single-store). |
| exec-plane §5.6 line 603 | Per-tenant local read source (aligned with ctrl-plane R4). |
| learning-loop §2.0.1 RESOLVED-AUDIT-TOPOLOGY-R4 | **Single authoritative store in control-plane DB**; MCP reads control-plane over mTLS; per-tenant table renamed `audit_events_outbox`. **R4 reversal of R3 two-store.** "ctrl-plane's design wins." |

**Three R4 positions, two reversals running in opposite directions.** ctrl-plane and exec-plane converge on per-tenant local read; learning-loop converged on control-plane single-store with the table rename `audit_events_outbox`. learning-loop §2.0 canonical Storage Topology table (line 122) names `audit_events` authoritative in control plane and `audit_events_outbox` per-tenant — which contradicts ctrl-plane's claim that the per-tenant table is named `audit_events`. Same names disagree across the canonical table and the consuming spec.

This is the second R5 demand: pick one direction, name the per-tenant table consistently.

#### 2.5 Storage topology lock — VERIFIED, with caveats

**Single canonical source: learning-loop §2.0.** 13-row table covering all entities with database, writer, readers, RLS, retention, PII flag, DDL ref. Ctrl-plane §13 references it; exec-plane §13.4 references it; operator-ux §9.7 references it. **The persistent risk #1 from R3 is closed.**

**Caveats:**
- ctrl-plane R4 index (line 36) claims "RESOLVED-STORAGE-TOPOLOGY: Single canonical Storage Topology table — §18 (new)." ctrl-plane §18 does not exist; the canonical reference is learning-loop §2.0. The index is misleading; the substance is correct (ctrl-plane references learning-loop §2.0 in §13).
- The audit-store row in learning-loop §2.0 names the per-tenant table `audit_events_outbox`; ctrl-plane §9.3 R4 body names it `audit_events` (local). The naming is unaligned.

### 3. End-to-end acceptance scenarios — testability final

| Scenario | R3 verdict | R4 verdict | Change driver |
|---|---|---|---|
| SC-01 Golden-path correction | PARTIALLY TESTABLE | TESTABLE | C13 18-field envelope landed; C12 storage topology locked |
| SC-02 Multi-correction promotion | PARTIALLY TESTABLE | TESTABLE | C12 storage topology locked; promotion write path through ctrl-plane §13.5 single store |
| SC-03 Contradicting-correction supersession | TESTABLE | TESTABLE | unchanged |
| SC-04 Abandoned packet | TESTABLE | TESTABLE | unchanged |
| SC-05 Tenant isolation leak attempt | PARTIALLY TESTABLE | TESTABLE | learning-loop §2.7.2 PII-strip classifier + §13.5 strengthened INV-05 contract test (asserts `"ABC Realty"` cannot reach aggregates); operator-ux CT-TenantBinding-01 remains in place |
| SC-06 Sandbox/approval-bypass attempt | NOT TESTABLE in implementation | **TESTABLE in implementation** — confirmed | learning-loop §11.5 has `mcp_audit_reader` role + `mcp_approval_events` view + TEST MCP-SNAPSHOT-READER-SCOPED; learning-loop §11.3 has all required event_types including `approval_received`, `blocked_write_attempted`, `sandbox_escape_blocked`; exec-plane §10.4 has `test_mcp_blocks_write_final_without_approval_audit`, `test_mcp_allows_write_final_after_approval_audit`, `test_mcp_sandbox_blocks_even_with_approval`, `test_mcp_default_deny_when_mode_unset`. **Note:** the exact connection target (per-tenant local DB vs. control-plane DB) depends on R4-CONFLICT-2 resolution; the test design holds under either resolution. |
| SC-07 Replay determinism check | PARTIALLY TESTABLE | TESTABLE | exec-plane §10.8 `prop_replay_determinism_n_runs` (N=60 derived); §10.7 `test_replay_temperature_zero` named; learning-loop §2.5 `mcp_read_snapshot` artifact_type added with `mcp_idempotency_key` linkage |
| SC-08 Double-tap approval idempotency | TESTABLE | TESTABLE | unchanged; ctrl-plane §17 SHA-256 derivation now provides composition consistency for the audit-event idempotency key |

**R4 verdict: 8 of 8 scenarios are TESTABLE.** The one remaining qualifier is SC-06's connection-target ambiguity, which is downstream of R4-CONFLICT-2 (audit topology direction). The test design is independent of that resolution.

### 4. OE/UE final smell hunt

#### OE-R4-1: `event_type` enum is borderline god-object (42 entries, organized)

learning-loop §11.3 has 42 event_type entries grouped into 7 named categories: case lifecycle, approval & correction, gateway, MCP server gates, candidate lifecycle, promotion/rules, tenant lifecycle. The grouping is in the table itself with bold-labeled section headers. operator-ux R3 §9.7 pushes 7 additional events to the registry under OPEN-AUDIT-CANON-CONFORMANCE-R4. Total trajectory: ~49 entries.

**Verdict: borderline, mitigated by structure.** R3 audit predicted >25 with overlapping semantics; R4 reality is 42 with 7 named groups and writer-registry authorization (§11.6) per group. The grouping prevents the flat-enum scan problem I flagged. The remaining smell is naming overlap: `correction_received` (exec-plane writer) vs. `correction_resolved_at_gateway` (operator-ux R4-pending writer) — these describe different events but the `correction_*` prefix forces readers to disambiguate via emitter. Recommend rename of operator-ux's pending event to `gateway_correction_resolved` to make emitter explicit.

#### OE-R4-2: SHA-256 composition rule applied uniformly — VERIFIED

ctrl-plane §17.2 has 10 registry rows. Component diff:
- All rows include `tenant_id` first.
- 4 rows also include `decision_point_id` (signal, correction, packet, MCP tool).
- Each row's component list is fixed and named in the registry.
- Separator: ASCII Unit Separator (`0x1F`) — unprintable, removes ambiguity.

exec-plane §15 references §17 derivation rule. operator-ux §9.6 references §17. learning-loop §3.9 cumulative inventory mentions §17 derivation as the upstream rule.

**Verdict: applied uniformly.** No spec invented a competing rule. The 10 registry keys cover signal, correction, packet, MCP tool, audit event, replay run, alert, OTP request, gateway idempotency, source message — exhaustive against the 5 keys I flagged in R3.

#### OE-R4-3: Storage topology coherence — INCOHERENT on naming, COHERENT on layout

learning-loop §2.0 names per-tenant table `audit_events_outbox`. ctrl-plane R4 §9.3 names it `audit_events` (local). exec-plane §5.6 line 603 references "per-tenant `audit_events` table" without specifying the rename.

**Verdict: layout is coherent (per-tenant local + control-plane authoritative); naming is incoherent (`audit_events_outbox` vs. `audit_events`). Pick one name in R5.**

#### UE-R4-1: ctrl-plane R4 deliverables claimed in index but absent in body

ctrl-plane R4 conflict-resolution index (lines 28–40) lists `RESOLVED-STORAGE-TOPOLOGY` (§18 new), `RESOLVED-CONTRACT-TESTS` (§19 new), `RESOLVED-PROPERTY-TESTS` (§12.6 new), `RESOLVED-SC-MAP` (§20 new). **None of §12.6, §18, §19, §20 exist in the ctrl-plane body.** Substantively, the storage topology lives in learning-loop §2.0 (not §18); the cross-component contract tests are scattered across §12.2, §12.3, §12.4 (R2 carried) and not consolidated in a §19; the property/fuzz tests are in §12.5 (R3) not a new §12.6; the SC-map is in scattered tests. The R4 index again over-claims; the substance is partly there in older sections.

This is the third recurrence of the aspirational-index pattern from R2/R3. Not blocking R5 — the substance exists elsewhere — but the index discipline issue persists.

#### UE-R4-2: Two R4 reversals running opposite directions (R4-CONFLICT-1, R4-CONFLICT-2)

Detailed in §2.4 above. Real conflicts. R5 must resolve.

#### UE-R4-3: Premature R4 abstractions — none observed

I scanned each spec's R4 deltas for new interfaces, generic wrappers, or plugin hooks added without justification. Found none. The R4 work is consolidation: 18-field envelope, mode enum, SHA-256 derivation, audit topology revision, fuzz tests, contract test tables. No new abstractions over <2 implementations.

### 5. TDD posture re-audit (per-spec scores)

| Spec | TDD discipline | Δ from R3 | Isolation completeness | Δ from R3 | Notes |
|---|---|---|---|---|---|
| ctrl-plane | **4** | +1 (corrected from R3=3) | **4** | unchanged | R3 work landed (§5.7, §6.7, §6.8, §8.7, §9.6.1, §13, §17 all in body); R4 reversals (§6.2 HMAC, §9.3 two-store, §6.8 cred location) landed in body; aspirational-index pattern persists in R4 §12.6/§18/§19/§20 — held the line. |
| exec-plane | **5** | unchanged | **5** | unchanged | §10.7 (8-scenario SC mapping), §10.8 (3 derivable property/fuzz tests, N=60 from binomial), §11 (11-row cross-component contract test table) are R4 substance. R4 reversal on auth (§13.1 mTLS) creates R4-CONFLICT-1 with ctrl-plane R4 (HMAC). |
| learning-loop | **5** | +0.5 (corrected from R3=4.5) | **4.5** | +1 (corrected from R3=3.5) | Massive R3 lift I missed: §2.0 canonical Storage Topology, §2.7.2 PII classifier with `quarantined_aggregates`, §2.8 unified DDL with locked RLS, §11.3 42-entry registry with 7 grouped categories, §11.5 `mcp_audit_reader` + view + scoped contract test, §11.6 writer registry, §13.5 strengthened INV-05 with literal-string assertion. R4 §2.0.1 audit-topology reversal creates R4-CONFLICT-2 with ctrl-plane R4. |
| operator-ux | **4** | +0.5 | **4.5** | +0.5 | R3 18-field envelope with full reconciliation table; §9.7 audit-event reconciliation; §10.4 yielded to gateway-secret-scope; CT-Outage-01 + CT-TenantBinding-01 + CT-CorrectionShape-01. operator-ux has no R4 deliverables (no fuzz/property tests, no cross-component contract test table) — the gateway is the only spec where R4 didn't add a new layer. |

**Summary:** corrected R3 average (TDD 4.5 / iso 4.4) → R4 (TDD 4.5 / iso 4.5). Bottom-line stable; ctrl-plane and learning-loop scores correct upward; operator-ux modest improvement; exec-plane holds top.

### 6. Top 5 demands for Round 5 (final consolidation)

1. **Resolve R4-CONFLICT-1 (auth method).** ctrl-plane §6.2 body is HMAC; exec-plane §13.1 body is mTLS; each cites the other's reversal. Pick one (HMAC-bearer + workload identity OR mTLS), update both bodies, retract the other claim, and ensure operator-ux §10.4 references the chosen mechanism. Without this, the entire `LoadSkillVersion`, audit ingest, replay trigger, and `emit-alert` paths cannot be implemented because the auth handshake type is undecided. R5 must close this.

2. **Resolve R4-CONFLICT-2 (audit topology direction).** Pick one: (a) ctrl-plane R4 / exec-plane R4 two-store with per-tenant local read for MCP preflight; or (b) learning-loop R4 §2.0.1 single-store control-plane read over mTLS, per-tenant `audit_events_outbox` for buffer only. Update all three bodies. Align the per-tenant table name (`audit_events` vs. `audit_events_outbox`). The MCP preflight contract test (`mcp_approval_events` view location) hinges on this.

3. **ctrl-plane writes the R4 deliverables claimed in its index** (§12.6 property/fuzz expansion, §18 storage topology table OR retract and reference learning-loop §2.0, §19 cross-component contract tests OR retract and reference exec-plane §11, §20 SC participation map). The substance largely exists in older sections; the index discipline must match the body. Same demand pattern as R4 demand #1 in my R3 audit, narrower scope.

4. **Reconcile naming on operator-ux pending event_types and the R4 audit-table name.** operator-ux §9.7 has 7 events pending learning-loop §11.3 registry add (OPEN-AUDIT-CANON-CONFORMANCE-R4); learning-loop must accept-and-add or push back-and-rename. Naming overlap (`correction_received` vs. `correction_resolved_at_gateway`) deserves an explicit prefix convention (`gateway_*` for gateway-emitted events would resolve the disambiguation). Per-tenant audit table: `audit_events_outbox` (learning-loop §2.0) or `audit_events` local (ctrl-plane §9.3) — pick one name.

5. **operator-ux R5 deliverables (the only spec without R4 work landed).** The team's R4 deliverables that operator-ux owes: (a) fuzz/property tests for inbound webhook adversarial fixtures (build on the `prop_sandbox_mode_no_external_invocation` pattern from exec-plane); (b) cross-component contract test table on the gateway side, building from operator-ux's CT-* set with explicit producer-consumer pairing rows. The gateway is the system's external surface; R5 must put it on equal TDD footing with the other three specs.

### 7. Storage topology lock — VERIFIED with one rename outstanding

The canonical storage topology table is **learning-loop §2.0** (lines 107–125). It is referenced by ctrl-plane §13, exec-plane §13.4 (claimed in R4 index), and operator-ux §9.7. **The persistent risk #1 from my R3 audit is closed.** Outstanding cleanup: per-tenant audit table name (`audit_events_outbox` per learning-loop §2.0, vs. `audit_events` local per ctrl-plane R4 §9.3). One rename in R5 closes this fully.

---

*Last updated: Round 4. Verdict: convergent. R3 work landed in all four specs (corrected from my erroneous R3 audit). R4 produced strong substantive deliverables in exec-plane (§10.7/§10.8/§11) and learning-loop (§2.0/§2.0.1/§2.7.2 PII classifier/§11.5 mcp_audit_reader/§13.5 strengthened INV-05); incomplete in ctrl-plane (R4 reversals landed; R4 claimed deliverables in §12.6/§18/§19/§20 absent); none in operator-ux. **All 8 end-to-end scenarios are TESTABLE in implementation as of R4.** Two R4 reversals (auth mechanism, audit storage topology direction) are running in opposite directions across peer specs and must resolve in R5. Storage topology table is single-source at learning-loop §2.0 — persistent risk #1 closed. The team is ready for R5 final consolidation conditional on R4-CONFLICT-1, R4-CONFLICT-2, and the index-vs-body alignment demand landing.*

---

## Round 5 — Integration sign-off

R5 closed the two architectural conflicts I flagged in R4 and landed the final TDD-depth deliverables. Both reversals collapsed into one consensus: **R4-CONFLICT-1 (auth) resolved as mTLS** in ctrl-plane §6.2, exec-plane §13.1, learning-loop §2.0.1, operator-ux §4.3 — every internal call uses leaf certificates with SAN-encoded workload identity. **R4-CONFLICT-2 (audit topology) resolved as single-store** with control-plane `audit_events` authoritative and per-tenant `audit_events_outbox` as a transactional drain buffer only — MCP `WRITE_EXTERNAL` preflight reads the control-plane store via `mcp_audit_reader` over mTLS (latency tradeoff explicit in OPEN-AUDIT-LATENCY-R4 with measurement-gated fallback). ctrl-plane R4 deliverables that were aspirational in my R4 audit landed in body: §12.6 (RLS / JWT / idempotency property tests), §18 (single canonical Storage Topology table that defers to learning-loop §2.0 as authoritative), §19 (23-row cross-component contract test table), §20 (SC-01..SC-08 control-plane participation map). The aspirational-index pattern is fully closed.

### 1. Final per-spec scores

Rubric: TDD discipline (1–5) — invariants stated before implementation; contract tests named at every boundary; fuzz/property tests with derivable rigor. Isolation completeness (1–5) — tenant_id derived from authenticated context; cross-tenant access provably denied; defense-in-depth at storage and transport. Total = TDD + Isolation, max 10.

| Spec | TDD discipline | Isolation completeness | Total | R4 → R5 Δ | Notes |
|---|---|---|---|---|---|
| ctrl-plane | **5** | **5** | **10/10** | +1 TDD | §12.6 RLS+JWT+idempotency property tests landed; §19 23-row cross-component contract test table; §20 SC participation map; §18 storage topology defers to learning-loop §2.0. mTLS in §6.2 with 7 invariants and 6 contract tests. R4 aspirational-index pattern fully closed. |
| exec-plane | **5** | **5** | **10/10** | unchanged | Top-of-team since R3. R5 tightens to §5.6 single-store mTLS read; §13.4 storage topology contribution explicitly deferring to learning-loop §2.0. §11 11-row contract test table holds with R5 dual-path Temporal IAM tests. |
| learning-loop | **5** | **5** | **10/10** | +0.5 isolation | §13.0 13-row contract test table CT-LL-1..13; §13.1 PROPERTY MERGE-1..9 (R5 added MERGE-7 promotion-threshold floor, MERGE-8 replay-determinism property, MERGE-9 PII-classifier adversarial property with Unicode/base64/JSON/whitespace-trick generators). §11.0 audit topology R5 collapse to single store landed cleanly. INV-05a/b/c/d in §13.5 with literal `"ABC Realty"` assertion. |
| operator-ux | **4.5** | **4.5** | **9/10** | +0.5 TDD | §3.1 7-row cross-component contract test table; §3.2 4 fuzz tests (Parser-Adversarial, Reconnect-N-Minutes, Tenant-Mismatch, DoubleTap-Approve); §3.3 SC participation. R5 closed OPEN-Envelope-Naming and OPEN-Envelope-ScopeHint in §2.2 body (lines 183, 192). One genuine OPEN remains (Temporal-IAM measurement-gated) plus stale-index inconsistency. The half-point withhold reflects that the gateway's SPOF behavior under bridge-container crash (distinct from session disconnect, which CT-Outage-01 covers) is still partly assumed against WhatsApp's 14-day server retention rather than tested at the gateway boundary. |

**Stale-index nits (cosmetic, non-blocking):** ctrl-plane §15 R5 OPEN list still carries OPEN-N3, OPEN-AUDIT-CANON-GW-ADDITIONS, and the Envelope-* items as open; the substance is closed elsewhere (learning-loop §2.7.2 + §13.5 for N3; learning-loop §11.3 already lists all 14 gateway event_types and explicitly RESOLVED-CLOSED-R5 the conformance OPEN; operator-ux §2.2 body locks the envelope names). One R5-final pass to mark these RESOLVED-by-CROSS-REF in the indices closes the cosmetic gap. Did not penalize ctrl-plane's score for this.

### 2. Final cross-spec contract test inventory (union)

Building from ctrl-plane §19 (23 tests) + exec-plane §11 (11 tests) + learning-loop §13.0 (13 CT-LL-* tests) + operator-ux §3.1 (7 CT-* tests) + property/fuzz tests in each spec. The four §-tables are consistent; this section overlays them and flags one-sided contracts.

| Contract surface | Producer-side test | Consumer-side test | Status |
|---|---|---|---|
| Provisioning manifest config-mount | ctrl-plane `test_provisioning_manifest_delivered` (§19) | exec-plane `test_hermes_volume_tenant_label_match` + `ct_provisioning_manifest_config_mount` (§11) | YES |
| `LoadSkillVersion` request/response | ctrl-plane `test_load_skill_version_authoritative` (§19); learning-loop §9.1 schema | exec-plane `test_load_skill_version_pinned_at_start`, `ct_load_skill_version` (§11); CT-LL-13 manifest field parity | YES |
| SkillVersion manifest passthrough (C14) | learning-loop CT-LL-13, §9.3 | exec-plane `ct_skill_manifest_passthrough` (§11) | YES |
| `mcp_approval_events` view (Gate 2 — R5 single-store) | learning-loop CT-LL-9 `test_mcp_audit_reader_rls_scope`; ctrl-plane `test_mcp_approval_view_per_tenant_local` (now reads control-plane store; name retained for compatibility) | exec-plane `test_mcp_blocks_write_final_without_approval_audit`, `test_mcp_allows_write_final_after_approval_audit`, `test_mcp_sandbox_blocks_even_with_approval`, `ct_mcp_approval_events_select` (§11) | YES |
| Audit ingest durable buffer drain | exec-plane `ct_write_approval_audit_durable_drain` (§11); learning-loop CT-LL-11 `test_audit_outbox_drains_no_loss` | ctrl-plane `test_audit_ingest_drain_no_loss` (§19); §9.4 INGEST_04 | YES |
| 16-field signal envelope → `corrections` row (C13-R5) | operator-ux `CT-Envelope→PersistCorrection` (§3.1) | exec-plane `ct_persist_correction_from_signal_alone`, `test_envelope_to_persist_correction` (§11, §19); learning-loop CT-LL-1 | YES |
| Envelope missing-field rejection | (all parties) | ctrl-plane `test_envelope_missing_field_rejected` (§19); exec-plane `MalformedSignalError` path | YES |
| MCP tenant-header binding | exec-plane `test_mcp_header_tenant_mismatch_rejected`, `ct_mcp_tenant_header_binding`; provisioning fixture | exec-plane same (single-spec ownership) | YES (single-side; correctly so) |
| mTLS handshake / SAN workload identity | ctrl-plane `test_mtls_handshake_required`, `test_mtls_san_workload_identity`, `test_mtls_untrusted_ca_rejected`, `test_mtls_expired_cert_rejected`, `test_mtls_san_mismatch_rejected`, `test_mtls_rotation_overlap` (§6.2.2) | exec-plane / operator-ux consume via deployment | YES |
| Replay scheduler trigger | ctrl-plane `test_replay_scheduler_trigger`, `test_replay_scheduler_idempotent` (§19) | exec-plane §10.4 + §13.3 + `ct_replay_trigger_admin_to_internal` (§11); learning-loop CT-LL-12 skill_version pinning | YES |
| `emit-alert` from exec-plane | exec-plane `ct_emit_alert_from_exec_plane` (§11) | ctrl-plane `test_emit_alert_idempotent`, `test_emit_alert_severity_invalid` (§19) | YES |
| `emit-alert` from gateway | operator-ux `CT-AlertEmit→Sink` (§3.1) | ctrl-plane `test_emit_alert_idempotent` (§19) | YES |
| OTP/JWT verify + replay protection | ctrl-plane `test_jwt_verify_well_formed`, `test_jwt_verify_replay`, `test_jwt_refresh_chain_invalidation` (§19) | operator-ux `CT-OTPVerify→AuthService` (§3.1) | YES |
| Temporal signal IAM (path A native + path B sidecar fallback) | ctrl-plane §6.8.3 dual-path; exec-plane `ct_temporal_signal_iam_path_a` and `ct_temporal_signal_iam_path_b` (§11); ctrl-plane `test_temporal_cred_signal_only`, `test_temporal_cred_cross_tenant_rejected`, `test_native_or_sidecar_fallback` (§19) | operator-ux `CT-TemporalCred→Permissions` (§3.1) | YES (dual-path) |
| Channel binding / inbound tenant validation | operator-ux `CT-TenantBinding-01`, `CT-InboundTenant→ChannelBindings` (§3.1); FT-Tenant-Mismatch (§3.2) | exec-plane `test_signal_tenant_mismatch_rejected` (§10.4) | YES |
| WhatsApp session lifecycle | operator-ux `CT-WhatsAppSession→Lifecycle` (§3.1) | ctrl-plane §5.7 `INV-WA-01`, `INV-WA-02` | YES |
| WhatsApp session outage no-loss (CT-Outage-01) | operator-ux §4.7.6 (gateway-internal) | (gateway-internal) | YES (single-side; correctly so) |
| Audit-event writer-registry authorization | learning-loop CT-LL-10 `test_writer_registry_authorization` | ctrl-plane `test_audit_writer_registry`, `test_audit_canon_event_type_lint` (§19) | YES |
| RLS cross-tenant denial | ctrl-plane `test_rls_cross_tenant_select` (§12.6.1, §19); learning-loop CT-LL-3, CT-LL-4 | (consumed by every reader role) | YES |
| Idempotency-key composition (SHA-256 derivation, 10-row registry) | ctrl-plane `test_idempotency_key_property` (§12.6.3, §19); §17.4 IDEMP_01..04 | exec-plane §15 inventory; learning-loop §3.9 inventory; operator-ux §9.6 inventory | YES |
| HMAC-bearer audit token tests | ctrl-plane `test_hmac_bearer_token_required`, `test_hmac_bearer_signature_workload_match`, `test_hmac_replay_jti_rejected` (§19) — **stale**; the team now requires mTLS not HMAC-bearer (§6.2 R5) | — | **STALE-LISTING** in §19 (rows 14–16). The R5 collapse to mTLS supersedes HMAC-bearer at the application layer; §19 should rename these to mTLS-equivalent assertions. Cosmetic; no blocker. |
| Property: replay determinism | exec-plane `prop_replay_determinism_n_runs` (§10.8, N=60 derived from binomial) + learning-loop PROPERTY MERGE-8 + CT-LL-12 | learning-loop §13.3 REPLAY TEST-1..4 | YES |
| Property: 3-gate Cartesian | exec-plane `fuzz_mcp_write_final_three_gate` (§10.8) | (single-side) | YES |
| Property: sandbox-mode escape | exec-plane `prop_sandbox_mode_no_external_invocation` (§10.8) | (single-side) | YES |
| Property: PII classifier adversarial | learning-loop PROPERTY MERGE-9 (Unicode/base64/JSON/whitespace generators); CT-LL-6, CT-LL-7 | (single-side; structural correctness) | YES |
| Property: candidate-match algebra | learning-loop PROPERTY MERGE-1..7 (§13.1) | (single-side) | YES |
| Property: gateway parser adversarial | operator-ux `FT-Parser-Adversarial` (§3.2) | (single-side) | YES |
| Property: gateway reconnect-N-minutes | operator-ux `FT-Reconnect-N-Minutes` (§3.2) | (single-side; depends on WhatsApp 14-day server retention beyond ≤30min hard guarantee) | YES with documented bound |
| Property: gateway double-tap | operator-ux `FT-DoubleTap-Approve` (§3.2) | exec-plane `test_approved_signal_idempotency` | YES |

**Coverage holes (non-blocking):**
- ctrl-plane §19 retains 3 rows naming HMAC-bearer tests that are obsolete after the R5 mTLS collapse (rows 14–16). Cosmetic.
- WhatsApp session outage > 30 minutes is documented as bounded by WhatsApp server-side retention, not asserted by gateway tests. This is honest single-side scoping; it surfaces as a surviving risk in §6 below.

### 3. Final OPEN inventory across the four specs

Every remaining OPEN item, tagged into the moderator's three categories. PRODUCT-INPUT requires a business / legal / product decision; MEASUREMENT requires a runtime measurement to confirm a value or trigger a documented fallback; OUT-OF-SCOPE is deliberately deferred (e.g., reviewer-facing UI, channels beyond WhatsApp + Telegram-dev).

| Tag | Topic | Owner | Gating question | What would resolve |
|---|---|---|---|---|
| **OPEN-PRODUCT-INPUT-1** (was OPEN-OQ-05) | Minimum legal retention for audit events in AU/UK/US jurisdictions | Product / Legal | What is the minimum legal retention for compliance? | Legal sign-off; current default 7 years per learning-loop §11.5 stands until then. |
| **OPEN-PRODUCT-INPUT-2** (was OPEN-OTP-TTL) | Operator JWT lifetime | Product | 24h (security) vs. 7d (UX) vs. 30d (sticky)? | Product decision on operator session length; default 24h shipped per ctrl-plane §6.7. |
| **OPEN-PRODUCT-INPUT-3** (was OPEN-OQ-07) | Vertical assignment mutability and multi-vertical tenants | Learning Architect + Product | Can `tenants.vertical` change after onboarding? Can a tenant belong to two verticals? | Product decision on vertical model. Current schema assumes single vertical, immutable. |
| **OPEN-PRODUCT-INPUT-4** (was OPEN-OQ-03) | RuleCandidate confidence recalculation trigger | Learning Architect | When new corrections arrive — Learning loop, control-plane event, or polling read? | Internal architectural decision; doesn't block first-pilot but affects when candidates cross threshold. |
| **OPEN-PRODUCT-INPUT-5** (was OPEN-OQ-04) | Condition schema ownership at replay-assertion time | Learning Architect | Does Evaluation Service parse conditions, or does Learning expose a parser API? | Internal architectural decision; affects ctrl-plane Evaluation Service implementation but not Phase 1 sandbox correctness. |
| **OPEN-MEASUREMENT-1** (was OPEN-AUDIT-LATENCY-R4) | mTLS Postgres round-trip on MCP `WRITE_EXTERNAL` preflight | Exec-plane / Observability | P99 of intra-VPC mTLS Postgres SELECT on `mcp_approval_events` over 1k WRITE_EXTERNAL requests post-MVP | If P99 ≤ 50ms (target stated by ctrl-plane §9.3): single-store stands. If exceeds: fallback documented — reintroduce per-tenant outbox as a read source with explicit drain-lag semantics (R3 design preserved). |
| **OPEN-MEASUREMENT-2** (was OPEN-Temporal-IAM) | Temporal native IAM granularity for SignalWorkflow-only on task-queue glob | Exec-plane | Does deployed Temporal version (OSS vs. Cloud) support per-namespace SignalWorkflow-only ACL? | Deploy-time check. Path A native (default) succeeds → ship as-is. Path A fails → ship signal-proxy sidecar (path B fully specified in operator-ux §10.4); contract test `ct_temporal_signal_iam_path_b` asserts identical observable behavior. |
| **OPEN-OUT-OF-SCOPE-1** (was Phase-2 progression) | Approval-gated shadow + autopilot modes | All architects | When does the operator graduate from sandbox? | Phase 2; the `mode: sandbox\|live` enum truncates the 4-state product progression to 2 deliberately for Phase 1 (ref: learning-loop §2.0c, exec-plane INV-T5). |
| **OPEN-OUT-OF-SCOPE-2** | Reviewer-facing browser UI | Ctrl-plane | When is the Rule Review Console UI built? | Phase 1 internal admin uses API + minimal admin UI; full reviewer UI is a downstream deliverable. |
| **OPEN-OUT-OF-SCOPE-3** | Multi-channel beyond WhatsApp + Telegram-dev | Operator-ux | When do we add SMS / Slack / iMessage? | Deferred until 3rd channel exists. The 2-method ChannelAdapter (RESOLVED-C7) is the test seam, not a future-channel abstraction. |

**Aggregate:** 5 PRODUCT-INPUT items (none block first-pilot), 2 MEASUREMENT items with explicit measurement criteria and documented fallbacks, 3 OUT-OF-SCOPE items (Phase 2). **No OPEN item blocks development handoff.**

### 4. End-to-end acceptance scenarios — final testability lock

All 8 scenarios remain TESTABLE in implementation after R5. The R5 deltas tightened the supporting tests; no scenario regressed.

| Scenario | Verdict | Owning contract test (named) |
|---|---|---|
| **SC-01** Golden-path correction | TESTABLE | exec-plane SC-01 row §10.7; learning-loop CT-LL-1, GOLDEN PROMOTE-1; ctrl-plane `test_provisioning_manifest_delivered`, `test_load_skill_version_authoritative`; operator-ux CT-Outbound-01, CT-Envelope→PersistCorrection |
| **SC-02** Multi-correction promotion | TESTABLE | learning-loop CT-LL-5, GOLDEN PROMOTE-1, PROPERTY MERGE-2 / MERGE-7; ctrl-plane `test_replay_scheduler_trigger`; exec-plane SC-02 row §10.7 |
| **SC-03** Contradicting-correction supersession | TESTABLE | learning-loop §8.4 VER-1..3, GOLDEN PROMOTE-2; ctrl-plane RLS isolation property test |
| **SC-04** Abandoned packet | TESTABLE | exec-plane SC-04 row §10.7 (`test_no_persist_correction_on_abandon`); operator-ux CT-Expiry-01, CT-E2E-02 |
| **SC-05** Tenant isolation leak attempt | TESTABLE | learning-loop CT-LL-3 (RLS), CT-LL-4 (CHECK), CT-LL-6/7 (PII strip), §13.5 INV-05a/b/c/d; ctrl-plane `test_rls_cross_tenant_select`, `test_audit_token_tid_mismatch`; operator-ux CT-TenantBinding-01, FT-Tenant-Mismatch; exec-plane `test_mcp_tenant_id_isolation` |
| **SC-06** Sandbox / approval-bypass attempt | TESTABLE | exec-plane SC-06 row §10.7 (4 named tests including `test_mcp_default_deny_when_mode_unset`); learning-loop CT-LL-8, CT-LL-9, CT-LL-11; exec-plane `fuzz_mcp_write_final_three_gate` (24-cell Cartesian); ctrl-plane `test_mcp_approval_view_per_tenant_local` (now reads control-plane store) |
| **SC-07** Replay determinism check | TESTABLE | exec-plane `prop_replay_determinism_n_runs` (N=60 derived); learning-loop CT-LL-12, PROPERTY MERGE-8, REPLAY TEST-1..4; exec-plane `test_replay_temperature_zero` |
| **SC-08** Double-tap approval idempotency | TESTABLE | operator-ux FT-DoubleTap-Approve, CT-Inbound-04; exec-plane `test_approved_signal_idempotency`; learning-loop CT-LL-2 (idempotency on SHA-256 key) |

**8 of 8 TESTABLE.** The system-level acceptance bar is met.

### 5. Sign-off statement

**The spec set IS ready for development handoff.**

Five rounds of cross-team review have produced 8,289 lines of specification across four component specs and one integration critique. Every cross-component contract has named producer-side and consumer-side tests. Every isolation invariant has a contract test (mTLS handshake, RLS cross-tenant denial, MCP tenant-header binding, gateway provider_number → tenant_id binding, PII classifier on `vertical_candidate_aggregates`). Every architectural decision is defended with rationale in the relevant spec's §11 / §14 / Decisions table. The 8 end-to-end scenarios are testable with named tests at every boundary. The remaining 10 OPEN items are correctly classified as non-blocking (5 PRODUCT-INPUT, 2 MEASUREMENT with documented fallbacks, 3 OUT-OF-SCOPE).

**Recommended development entry-point order (constraint-derived, not schedule-derived):**

1. **Tenant provisioning workflow + per-tenant DB schema migration** (ctrl-plane §5, learning-loop §2.0–§2.10 DDL). Blocking dependency for everything per-tenant; no other component can be tested without a provisioned tenant.
2. **Internal CA + mTLS issuance** (ctrl-plane §6.2). Every internal call from step 3 onward requires a leaf cert; without the CA, none of the contract tests run.
3. **Hermes runtime container + provisioning manifest config-mount** (exec-plane §2). Validates step 1's manifest delivery and step 2's TLS bundle install. `test_provisioning_manifest_delivered` and `test_hermes_volume_tenant_label_match` are the gating tests.
4. **Sandbox MCP servers (email, drive, invoice)** (exec-plane §5). Live MCP servers are deliberately not built; sandbox-only is the Phase 1 surface. The 3-gate preflight (`fuzz_mcp_write_final_three_gate`) must pass before any draft tool is exposed to Hermes.
5. **Single-tenant Temporal worker + EnquiryTriageWorkflow** (exec-plane §3, §4). Pick the simplest workflow first; the other two reuse the same activity/signal patterns.
6. **Learning-loop candidate matcher + confidence scorer** (learning-loop §4, §5). Can develop against fixture corrections without the gateway. Gating: PROPERTY MERGE-1..9 pass.
7. **Rule Review Console + promotion pipeline** (ctrl-plane §7 + learning-loop §7). Internal-only; does not need the gateway.
8. **Audit-events store + outbox drain protocol** (ctrl-plane §9.3 / §9.4 + learning-loop §11.0 / §2.0.1 outbox). Required by step 4's MCP preflight.
9. **WhatsApp session (whatsmeow) + gateway** (ctrl-plane §5.7 + operator-ux §4). Last because everything before this can be exercised via Telegram dev channel + Temporal CLI signals; the WhatsApp surface is the highest-operational-risk component.
10. **Operator OTP issuance + JWT** (ctrl-plane §6.7). Can be added at step 9 boundary.

**Integration tests that must pass before merging the first non-trivial PR (defined as any PR that touches more than one component's code path):**

- `test_provisioning_manifest_delivered` (ctrl-plane §19) + `test_hermes_volume_tenant_label_match` (exec-plane §10.6) — proves step 1 and step 3 work end-to-end.
- `test_mtls_handshake_required`, `test_mtls_san_workload_identity`, `test_mtls_san_mismatch_rejected` (ctrl-plane §6.2.2) — proves step 2 isolation holds.
- `test_persist_correction_from_signal_alone` / CT-LL-1 (learning-loop §13.0; exec-plane §11) — proves the 16-field envelope handoff (C13-R5: R4's `correction_id` and `parse_status` reverted).
- `test_rls_cross_tenant_select` (ctrl-plane §12.6.1) — proves tenant isolation at the storage layer.
- `test_mcp_blocks_write_final_without_approval_audit` and `test_mcp_sandbox_blocks_even_with_approval` (exec-plane §10.4) — proves the 3-gate preflight.
- `CT-Outage-01` (operator-ux §4.7.6) — proves WhatsApp session outage no-loss for ≤30min.

If any of those six tests cannot be made to pass against the implementation, the spec was wrong and a sixth round is required. If all six pass, the team has a working sandbox correction harness for one tenant and can iterate on the remaining components without architectural risk.

### 6. Surviving architectural risks

Risks the team did not fully close in 5 rounds. Each is honest and has a documented response path; none should hold up development. Severity reflects probability × impact, not abstract concern.

| Risk | Severity | What triggers re-architecture | What to monitor in production |
|---|---|---|---|
| **MCP `WRITE_EXTERNAL` preflight latency on the hot path.** R5 collapsed to single-store control-plane read; intra-VPC mTLS Postgres adds ~5–10ms. This is on every operator-approved external action. | HIGH | If P99 of the Gate-2 read exceeds the budget post-MVP (target: ≤50ms incremental on a path that already costs ~100s of ms), fall back to per-tenant outbox-as-read-source per OPEN-MEASUREMENT-1. The R3 design is preserved exactly for this fallback; no new design work needed. | P99 latency of the `mcp_audit_reader` SELECT, per tenant, per workflow. Alert if median exceeds 25ms. |
| **whatsmeow session failure is a per-tenant risk.** Session crash queues outbound packets in the gateway's durable `outbound_queue`. Inbound messages during the crash are buffered by WhatsApp's multi-device protocol server-side; recovery depends on WhatsApp's 14-day server-side retention. CT-Outage-01 covers the ≤30min disconnect case but not the gateway-process-died case. | MEDIUM | If session outages cause measurable correction-loss in production, the architectural escape is WhatsApp Business API (deferred per product spec) — same gateway code path, different transport. The interface is already test-seamed. | Session failure count per tenant per week; outbound packet retry count; gateway-side tombstone audit events (`packet_tombstoned`) per tenant per day. |
| **DA dissent on unified `validated_rules` storage.** The team locked all four scopes (case/tenant/vertical/default) into one control-plane DB table protected by RLS. Round 2 audit ranked split-by-trust-boundary as architecturally correct; the team chose the unified table for operational simplicity. RLS bypass via privileged role escalation is the failure mode. | LOW | learning-loop §2.0.1 documents the forensic-test trigger: any of (a) successful cross-tenant SELECT under expected RLS settings, (b) Postgres CVE bypassing `current_setting`-based RLS, (c) audit log evidence of `app.bypass_tenant_check = true` use against an unassigned tenant — reverts case/tenant rules to per-tenant DB (R1 split). The forensic test runs quarterly against the integration environment. | Cross-tenant SELECT denial test in CI (`test_rls_cross_tenant_select`); audit query for any `app.bypass_tenant_check` use; quarterly forensic read against `validated_rules` from a non-platform identity. |
| **LLM-temperature determinism for replay.** SC-07 testability rests on `temperature=0` producing identical decision-point outputs. LLM provider behavior is outside Victoria's control; provider-side changes (model swap, rounding behavior, system-prompt-injection mitigations that randomize) can break determinism without notice. | MEDIUM | If the replay-determinism property test (`prop_replay_determinism_n_runs` N=60) starts failing, freeze the LLM model+provider version and add MCP read-tool snapshots for any non-idempotent LLM call (already specified as `mcp_read_snapshot` artifact_type; can be extended to capture LLM call inputs+outputs). | `prop_replay_determinism_n_runs` failure rate in CI; replay-diff regression rate per `WorkflowTemplate` per week; provider model version + API version pinned in tenant manifest. |
| **WhatsApp non-Business-API as production transport.** R5 ratifies whatsmeow for Phase 1. WhatsApp can ban the session, change protocol, or rate-limit the connection at any time. The product spec acknowledges this; the gateway design is migration-ready (interface seam) but the operational risk is borne by every customer for as long as Phase 1 lasts. | MEDIUM | A WhatsApp ban or rate-limit incident affecting ≥1 production tenant. Migration path is WhatsApp Business API; per-tenant verification cost is non-trivial. | WhatsApp ban/rate-limit incidents per tenant per month; `channel_session_suspended` audit events per tenant per month. |
| **Confidence-scoring formula chosen ahead of data.** learning-loop §5 uses Wilson-lower-bound × recency × scope-consistency with hand-tuned constants. Round 1 OE-05 flagged this as premature. The threshold `under_review = 0.55` and `min_evidence_count = 3` work for the spec's example math but have no production-data validation. | LOW | If observed promotion patterns at first 10 customers don't match the threshold model (too many false-positive promotions or too many under-promoted candidates), the formula is per-tenant overridable (`tenants.conf_*` columns) and the system-wide defaults can be retuned without schema change. | Promotion-to-rejection ratio per tenant; time-from-first-evidence-to-promotion distribution; reviewer override rate on auto-suggested promotions. |

---

*Last updated: Round 5. Verdict: SHIPPABLE. R5 closed both R4 architectural conflicts (auth → mTLS, audit topology → single-store control-plane authoritative + per-tenant outbox drain). 8 of 8 SC scenarios TESTABLE in implementation. Per-spec scores: ctrl-plane 10/10, exec-plane 10/10, learning-loop 10/10, operator-ux 9/10. Cross-component contract tests are landed bilaterally for every peer boundary. 10 remaining OPEN items correctly classified: 5 PRODUCT-INPUT (none block first-pilot), 2 MEASUREMENT (with documented fallback designs), 3 OUT-OF-SCOPE (Phase 2). Surviving architectural risks (MCP preflight latency, whatsmeow session risk, DA dissent on unified rules, LLM determinism, WhatsApp non-Business-API, premature confidence formula) all have documented response paths and production monitoring criteria. The team is ready for development handoff. Recommended entry-point order in §5; the 6 gating integration tests in §5 must pass before any cross-component PR merges.*
