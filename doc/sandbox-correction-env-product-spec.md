# Product Spec: Victoria: Small-Business Workflow Automation with Sandbox Correction Environment OnBoarding

## Executive Summary

This product is a **chat-native correction harness** for small business owners and solo operators who do not want to design workflows in a dashboard, map processes in a wizard, or maintain a ticketing-style operations console. The core insight is that these users are far better at reacting to a concrete but imperfect workflow run than at describing tacit business rules from scratch.

The product simulates a realistic workflow inside a sandbox that mirrors the tools the business already understands, such as email, Telegram, shared drive, and invoicing systems, then asks the owner to correct specific decisions from their phone. Each correction becomes structured knowledge: a missing fact, wrong branch, exception rule, or template preference, which can be converted into reusable operating logic rather than disappearing in chat history.

Hermes is the preferred harness because its model is not only tool use but also persistent procedural learning through skills, memory, and an explicit learning loop. The safest system design is a shared control plane with an isolated execution plane per client: dedicated Hermes runtime, dedicated secrets, dedicated storage, dedicated workflow queues, and tightly scoped MCP servers per tenant.

## Background and Problem

The motivating example is the HN case shared by **mjsweet**, where a tradesperson runs an end-to-end quoting workflow from a phone while getting in and out of a truck: work orders arrive by Gmail, photos are added in Telegram, Claude analyzes them, a correction form is generated, a long PDF proposal is created, a draft Gmail is prepared, and Xero is used for downstream invoicing. The important lesson is not only that LLM agents can automate meaningful business work, but that the operator never needs a separate “ops console” to participate in the loop.

Most workflow tools for non-technical users fail because they assume the user can model the process upfront in a canvas, wizard, or project-management abstraction. Prior research in this discovery area suggests that open conversation tends to elicit only generic descriptions, while corrections to a draft, decision tables, exception-focused prompts, and contextual observation surface deeper business rules more effectively.

The product therefore should not be positioned as a workflow builder. It should be positioned as a system that **runs a draft workflow, shows what it did, and lets the owner say what is wrong**. That correction loop is the onboarding experience and also the mechanism for ongoing improvement.

## Product Thesis

### One-line thesis

Small business owners should not be asked to design automation; they should be shown a draft run in familiar tools and allowed to correct it from their phone.

### Core proposition

The product creates a **sandbox correction environment** where the agent executes representative business workflows using realistic but isolated tools and artifacts. Instead of asking the user to define every step in advance, the system asks the user to review specific outputs and decisions, such as whether a quote should be sent, whether a template is wrong, or whether more information is needed.

### Why this is a better onboarding metaphor

- It matches how tacit business knowledge is usually surfaced: through exceptions, overrides, and corrections rather than through clean verbal descriptions.
- It preserves the user’s existing operating environment: chat threads, email drafts, shared documents, and lightweight approvals rather than ticket boards or workflow canvases.
- It allows progressive trust: shadow mode first, approval-gated execution second, partial autopilot later.

## Product Principles

- **Chat-native, not dashboard-native.** The primary UX for the operator is Telegram, WhatsApp, or email, with optional mobile preview pages for artifacts.
- **Correction over configuration.** The system should prefer “What is wrong with this run?” over “Describe your workflow.”
- **Decision-point granularity.** Learning should happen at individual decision points, not by asking the owner to validate or redesign an entire workflow at once.
- **Artifacts are the UI.** The user reviews draft emails, PDFs, invoice previews, extracted facts, and branch decisions—not abstract nodes or tickets.
- **Safety before autonomy.** Destructive or externally visible actions require explicit approval until the workflow has enough validated history.
- **Per-client isolation by default.** Agent memory, learned skills, tool credentials, storage, queues, and workflow state must not be shared across clients.

## Target User and Initial Use Cases

### Primary user

The first target user is the owner-operator or solo entrepreneur who runs meaningful operational workflows through messaging, email, spreadsheets, shared drives, and accounting tools, but does not want to become a workflow designer.

### Ideal first workflows

Initial scope should focus on narrow, artifact-heavy workflows where human approval is natural and value is visible quickly.

| Workflow | Why it fits | Typical artifacts | Correction examples |
|---|---|---|---|
| New customer enquiry triage | High frequency, easy preview, obvious branch points | Draft reply, extracted facts, next-step decision | Wrong customer type, missing context, wrong response path |
| Quote/proposal drafting | Clear business value, aligns with mjsweet case | Draft email, PDF proposal, pricing summary | Use different template, request more photos, don’t send yet |
| Supplier invoice handling | Structured documents and reviewable output | Invoice object, coding suggestion, accounting draft | Wrong supplier, wrong tax treatment, hold for review |
| Job scheduling follow-up | Mobile-friendly, operational, easy to confirm | Calendar slot, reminder, customer message | Wrong priority, not enough info, wrong timing |

## User Experience and Workflow Model

### Operator experience

The operator should receive a compact review packet in the same channel they already use. A typical packet should contain:

- The trigger: “New enquiry received from ABC Realty.”
- The facts the agent extracted.
- The action the agent plans to take.
- A preview of the resulting artifact or side effect.
- A small set of correction actions.

### Recommended correction actions

The initial button set should be deliberately narrow and structured.

- Approve.
- Wrong facts.
- Wrong action.
- Missing condition.
- Use different template.
- Add note.

If the operator rejects the action, the system should ask one short follow-up question such as “Always wrong, or only this case?” or “What signal should have changed the decision?” so the correction can become a reusable rule rather than a one-off patch.

### Progression model

1. **Sandbox replay mode.** The system runs representative cases with no production side effects.
2. **Approval-gated shadow mode.** The system proposes actions and waits for approval before any write action.
3. **Partial autopilot mode.** Only low-risk actions auto-execute; high-risk or low-confidence branches still require approval.
4. **Validated skill mode.** The workflow is represented as a reusable skill and associated rules, with version history and rollback.

## Why Hermes Is the Right Harness

Hermes is suitable because it is designed around MCP-connected tool use, persistent memory, and skills that can be created and improved from repeated work patterns. In this product, each sandbox run becomes an episode that Hermes can use to improve procedural behavior over time rather than simply storing a flat transcript.

### How Hermes helps specifically

- **Tool abstraction through MCP.** Hermes officially supports MCP, which makes it a good orchestrator for sandbox email, drive, invoicing, and CRM adapters behind a controlled permission surface.
- **Skill creation and reuse.** Hermes documentation and ecosystem materials describe a learning loop where repeated successful task patterns can be converted into reusable skills.
- **Persistent procedural memory.** This is critical because the product’s core value is turning corrections into operating procedures, not merely remembering facts.
- **Migration optionality.** If future customers have existing OpenClaw setups, Hermes provides a migration path for persona, memory, skills, and provider configuration.

### Important Hermes limitation

Hermes should not be allowed to self-improve directly into production behavior without review. External discussion of learning loops notes the risk of solidifying bad habits if low-quality corrections are absorbed uncritically, so learning must be approval-gated and versioned.

## System Components

The product should be split into a **shared control plane** and a **per-client execution plane**.

### Shared control plane

The control plane is multi-tenant and contains only product management and metadata services, not customer workflow execution state.

| Component | Responsibility |
|---|---|
| API gateway / backend | Auth, tenant provisioning, workspace lifecycle, orchestration APIs |
| Tenant registry | Client metadata, deployment references, feature flags, status |
| Rule review console | Internal tooling for reviewing learned rules, approvals, and replays |
| Evaluation service | Regression tests, replay harness, rule-confidence scoring |
| Observability stack | Metrics, traces, alerts, audit review, anomaly detection |
| Billing / admin | Plans, usage, operator-level administration |

### Per-client execution plane

Each client gets a dedicated execution environment containing the agent, tools, secrets, queues, and stateful stores.

| Component | Responsibility |
|---|---|
| Hermes runtime | Core agent harness, skill execution, memory, planning |
| Temporal worker | Long-running workflow state, waits, retries, approvals |
| Sandbox email MCP | Drafts, inbox simulation, message threads |
| Sandbox drive MCP | File reads/writes, artifact storage paths, preview links |
| Sandbox invoice MCP | Draft invoices, supplier documents, bookkeeping previews |
| Messaging gateway | Telegram bot first; optional email and WhatsApp |
| Client Postgres DB | Workflow events, approvals, rules, artifacts, tenant state |
| Client object store | PDFs, previews, images, generated documents |
| Client secret scope | LLM provider keys, bot tokens, adapter credentials |

## High-Level Technical Stack

### Recommended stack

| Layer | Tech choice | Notes |
|---|---|---|
| Backend API / control plane | TypeScript with NestJS or Fastify | Strong productivity for product APIs and provisioning. |
| Agent harness | Hermes in Docker, pinned version tag | Hermes officially documents Docker-based deployment and recommends version pinning for reproducibility. |
| Workflow engine | Temporal | Temporal documents task queues per tenant as the main multi-tenant isolation pattern. |
| Primary relational DB | Postgres | Suitable for rules, events, approvals, tenant metadata, and audit logs. |
| Cache / queue support | Redis | Useful for transient job coordination and rate limiting. |
| Object storage | S3 or Cloudflare R2 | Store PDFs, previews, attachments, replay artifacts. |
| Tool integration | Custom MCP servers per capability | Narrowly scoped tool surface for each system. |
| Observability | OpenTelemetry + Grafana/Tempo/Loki or vendor equivalent | Per-tenant tracing and security auditability. |
| Secrets | Cloud secret manager or Vault | Dedicated secret scope per tenant. |
| Messaging | Telegram Bot API first | Best fit for phone-first operators and approval buttons. |

### Why not build everything inside Hermes

Hermes should remain the **agent runtime**, not the sole owner of workflow state, tenant provisioning, or product logic. Long-running approvals, retries, and audit-trace state are better handled by Temporal and the application layer, with Hermes invoked as a bounded reasoning and tool-using component.

## Reference Architecture

### Logical architecture

```text
Operator (Telegram / Email)
        |
Messaging Gateway
        |
Control Plane API ---------------- Internal Admin / Rule Review
        |
Tenant Router
        |
Per-Client Execution Plane
  |-- Hermes Runtime
  |-- Temporal Worker
  |-- Sandbox Email MCP
  |-- Sandbox Drive MCP
  |-- Sandbox Invoice MCP
  |-- Client Postgres
  |-- Client Object Storage
  |-- Client Secret Scope
```

### Execution flow

1. A seeded or live-like sandbox case enters the system.
2. Temporal starts a workflow instance in the client’s dedicated task queue.
3. Hermes is invoked to interpret the case, choose tools, and propose actions using MCP adapters.
4. Artifacts are generated and stored in client object storage.
5. The operator receives a compact packet in Telegram or email with approval/correction actions.
6. The correction is persisted as an event and transformed into a candidate rule.
7. Hermes re-runs the case with the updated rule set or skill context.
8. Internal reviewers may later promote the candidate rule into the validated default set.

## Data Model and Core Entities

The product should treat **corrections** as first-class data rather than as incidental chat messages.

### Suggested core entities

| Entity | Purpose |
|---|---|
| Tenant | Customer workspace and deployment boundary |
| WorkflowTemplate | Defines a workflow class such as enquiry triage or invoice handling |
| CaseRun | One execution of a workflow against an input case |
| DecisionPoint | A specific branch, fact extraction, or action proposal |
| Artifact | Email draft, PDF, invoice preview, summary card, extracted document |
| Correction | Operator feedback on a decision point or artifact |
| RuleCandidate | Structured interpretation of a correction |
| ValidatedRule | Approved rule used by default in future runs |
| SkillVersion | Hermes skill snapshot associated with a workflow |
| AuditEvent | Immutable event log of every action and decision |

### Suggested RuleCandidate shape

```json
{
  "tenant_id": "t_123",
  "workflow_type": "quote_drafting",
  "decision_type": "send_or_hold",
  "conditions": [
    {"field": "photos_complete", "operator": "=", "value": false},
    {"field": "client_type", "operator": "=", "value": "new"}
  ],
  "recommended_action": "hold_and_request_more_info",
  "scope": "default",
  "confidence": 0.72,
  "source_case_run_id": "cr_456",
  "source_correction_id": "corr_789",
  "status": "candidate"
}
```

## Tenant Setup and Complete Isolation

Security-sensitive deployment should assume that **cross-tenant memory leakage, cross-tenant credential exposure, and cross-tenant workflow access are unacceptable risks**. The safest design is not a single shared Hermes with tenant labels; it is one isolated runtime and state boundary per client.

### Tenant provisioning checklist

1. Create tenant ID and server-side identity mapping.
2. Create dedicated service account / workload identity for that tenant.
3. Create dedicated Postgres database or schema, preferably database for higher security.
4. Create dedicated object-store bucket prefix, or bucket for high-security clients.
5. Create dedicated secret namespace and encryption key scope.
6. Create dedicated Temporal task queues, and optionally dedicated namespace for premium isolation.
7. Create dedicated Hermes runtime with a dedicated data volume.
8. Create dedicated MCP server instances or sidecars using that tenant’s credentials.
9. Register deployment metadata in the control plane.

### Isolation levels

| Tier | Runtime | Storage | Queueing | Recommended for |
|---|---|---|---|---|
| Shared cluster / isolated namespace | One namespace per tenant, one Hermes runtime per tenant | DB per tenant, bucket prefix per tenant | Task queues per tenant | Standard SMB tenants |
| Dedicated VM / task group | One VM or ECS task group per tenant | DB per tenant, bucket or prefix per tenant | Task queues per tenant | Security-conscious SMBs |
| Dedicated cloud project / VPC | Full infra isolation per tenant | Private DB, private bucket, dedicated keys | Dedicated Temporal namespace | Enterprise or regulated clients |

### Hermes-specific isolation requirements

Hermes persists memories and skills on disk, so the mounted data path must be dedicated per tenant and never reused across clients. Hermes security documentation also emphasizes environment filtering, credential filtering, authorization rules, and container isolation as core controls.

## Security Architecture

### Security design goals

- No cross-tenant data leakage.
- No cross-tenant credential reuse.
- No destructive tool access without explicit approval until validated.
- Full auditability of case execution, corrections, and rule promotion.
- Bounded tool permissions via narrow MCP capability surfaces.

### Core controls

#### 1. Dedicated secrets per tenant

Provider keys, bot tokens, adapter credentials, and any customer system credentials must live in tenant-scoped secret stores and be injected only into that tenant’s runtime. Secrets should never be shared via a common environment file or multi-tenant config object.

#### 2. Narrow MCP tool exposure

Hermes MCP guidance and community deployment material highlight that MCP servers can expose large surfaces if carelessly configured, so only the minimum useful tools should be exposed. For example, expose `create_draft_email` rather than “full mailbox access,” and `create_invoice_draft` rather than “accounting admin.”

#### 3. Read-only by default

Sandbox adapters should default to read-only or draft-only actions until a workflow is promoted. “Send,” “delete,” “invoice finalization,” and any external publication action should remain gated.

#### 4. Tenant context propagation

OWASP recommends binding tenant context early and carrying it through queries, cache keys, storage paths, logs, and authorization checks. Tenant ID must be derived from authenticated backend identity, not from a client-supplied request parameter.

#### 5. Audit logging and replay

Every step should be written to an immutable event trail: input received, Hermes invocation, tool request, approval packet sent, correction received, rule candidate generated, rule promoted, and any failure or override. This is essential for both security and model-quality debugging.

#### 6. Version pinning and immutable deploys

Hermes Docker guidance recommends pinning image versions instead of using floating tags, which is important for reproducibility, rollback, and forensic clarity.

### Recommended deployment hardening

- Read-only container root filesystem with explicit writable volumes only for Hermes data and temporary runtime files.
- Network egress allowlist to approved LLM providers and messaging APIs only.
- No public ingress to Hermes or MCP servers; only the internal application plane may reach them.
- mTLS or equivalent service-to-service auth for Temporal and internal APIs where possible.
- Per-tenant rate limits and anomaly detection on tool actions and approvals.

## Infra Topology Recommendations

### Phase 1: Early product / first 10 customers

The most practical and safest early-stage architecture is **single-tenant deployment per client** using Docker-based runtime units. This costs more than deep multi-tenancy but radically simplifies the isolation story.

Recommended deployment unit per client:

- 1 VM, ECS task group, or equivalent isolated compute unit.
- 1 Hermes container.
- 3–4 MCP sidecars.
- 1 Temporal worker process.
- 1 Postgres database.
- 1 object-store namespace.
- 1 secret scope.

### Phase 2: Shared cluster with namespace isolation

Once tenant-boundary checks, logging, and ops maturity are proven, clients can move to a shared Kubernetes cluster with one namespace per tenant, one Hermes runtime per tenant, database-per-tenant, and Temporal task queues per tenant.

### Phase 3: High-security enterprise tier

For larger or regulated clients, use dedicated cloud projects or VPCs, dedicated KMS keys, private databases, dedicated Temporal namespace, and no shared runtime components.

## Development Plan

### Phase 0: Foundational prototype

- Telegram-only operator interface.
- One workflow: new enquiry triage or quote drafting.
- Sandbox email + artifact preview only.
- Manual internal review of rule candidates.
- Hermes invoked with tightly scoped MCP tools.

### Phase 1: Correction engine

- Structured correction actions.
- Case replay.
- RuleCandidate storage.
- Internal rule promotion workflow.
- Approval-gated side effects.

### Phase 2: Learning and skill lifecycle

- Map validated rules into Hermes skills or skill context.
- Skill versioning and rollback.
- Confidence scoring and regression replay.
- Auto-suggested rule promotion with human approval.

### Phase 3: Production-adjacent workflows

- Optional live-system connectors after sandbox success.
- Shadow mode against live-like cases.
- Partial autopilot for low-risk actions only.

## Key Risks and Mitigations

| Risk | Description | Mitigation |
|---|---|---|
| False confidence | Users approve a plausible run that still misses critical tacit logic | Force decision-point review and scenario replay before promotion |
| Correction fatigue | Early runs are too wrong and the user feels forced to “train the system” manually | Keep scope narrow, pre-seed templates, ask only one short follow-up question |
| Bad learning | Hermes or the rule system absorbs noisy corrections | Candidate/validated separation, versioning, rollback, internal review |
| Overbroad tool access | MCP tools expose too much capability | Narrow tool design, least privilege, draft-only actions |
| Cross-tenant leakage | Shared memory, storage, or credentials expose one client to another | Dedicated runtime, volume, DB, secrets, queues per tenant |
| Approval bottlenecks | Human approval slows throughput too much | Progressive trust tiers and autopilot only for validated low-risk branches |

## Open Product Questions

- Which initial workflow yields the clearest ROI with the least integration complexity: enquiry triage, quote drafting, or supplier invoice handling?
- Should validated rules live primarily in the product rule engine, in Hermes skill files, or in a hybrid model?
- What is the precise promotion threshold from sandbox-only to draft-capable to real-system shadow mode?
- Which channels beyond Telegram are most important for the first target customer segment?
- What minimum artifact set is required for a user to feel the workflow is “real enough” to critique effectively?

## Final Recommendation

The recommended product direction is a **Hermes-powered sandbox correction platform** where the customer never builds workflows directly. Instead, the system runs realistic cases in isolated tenant environments, asks the operator to correct concrete decisions from a phone-first interface, turns those corrections into structured rules and skills, and promotes only validated behavior into higher-autonomy modes.

Technically, the best starting architecture is a shared control plane with **single-tenant execution planes**, using Hermes in Docker, Temporal for durable workflow orchestration, Postgres for events and rules, object storage for artifacts, and per-client MCP sidecars guarded by least privilege and strict secret isolation.
