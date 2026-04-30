# Victoria — Control Plane Technical Specification

**Round:** 4 (TDD-depth + R4-CONFLICT-1/2 reconciliation; mid-round reversals withdrawn after peer R4 review)  
**Author:** Control Plane Architect  
**Status:** Three R4 mid-round reversals withdrawn (auth, audit topology, gateway Temporal credential storage) after peer R4 deltas confirmed all three R3 positions are the cross-spec consensus. TDD-depth reordering; cross-component contract-test table; storage-topology lock cross-references learning-loop §2.0; SC-01..SC-08 control-plane participation map.  
**Date:** 2026-04-27

---

## Round 4 Conflict Resolution Index

### R4 self-reversal note (transparent)

Mid-round, I attempted to reverse three R3 positions believing peer R4 specs were aligning differently. After reading peer R4 deltas the moderator named, all three peers retained their R3-aligned positions. The three reversals are withdrawn; the file re-aligns to R3 and to peer R4 consensus.

| Tag | Mid-R4 reversal (now withdrawn) | Peer R4 alignment proves R3 wins | Resolution |
|---|---|---|---|
| ~~REVISED-R4-AUTH~~ | Tried HMAC-bearer | exec-plane §13.1: "Accept ctrl-plane's pick (`01-control-plane.md` §6.2): all execution-plane ↔ control-plane internal RPCs use mutual TLS with certificates issued by an internal CA. Ctrl-plane R3 §6.2 explicitly reverses the R2 HMAC framing in favor of mTLS; this spec accepts the reversal and propagates it through every inbound and outbound channel." Operator-ux R4 references "mTLS-authenticated per ctrl-plane §6.2" in §4.3. | **mTLS holds.** R3 §6.2 stands. See §6.2 below. |
| ~~REVISED-R4-AUDIT-TOPOLOGY~~ | Tried two-store with local read | learning-loop §2.0.1: "ctrl-plane's design wins. This spec collapses the dual-read topology to a single authoritative store in the control-plane DB, with MCP reads going there over mTLS." Latency tradeoff explicit at §2.0.1 point 3. R3 "local audit_events" renamed to `audit_events_outbox` (drain buffer only). | **Single-store holds with `audit_events_outbox` rename.** See §9.3 / §9.4 below. |
| ~~REVISED-R4-GW-TEMPORAL-CRED~~ | Tried per-tenant secret scope, exec-plane provisions | operator-ux R4 RESOLVED-CredentialStorage-Reconciled (§10.4 line 1068): "Per-tenant signal-only credentials are stored in the **gateway-only secret scope** ... R3 path `tenant/{tenant_id}/messaging/temporal_signal_client` is replaced with `gateway/tenants/{tenant_id}/temporal_signal_client`." | **Gateway secret scope holds; control plane provisions.** See §6.8 below. |

### R4-CONFLICT resolutions (moderator-flagged)

| Tag | Conflict | Resolution |
|---|---|---|
| RESOLVED-R4-CONFLICT-1 | Auth method (mTLS vs HMAC) | **mTLS — all four specs aligned.** Internal CA, leaf certs per workload SAN, 30-day rotation, 24-hour overlap; exec-plane §13.1 INV-RPC1..4 are the canonical invariants. |
| RESOLVED-R4-CONFLICT-2 | Audit topology (single-store vs two-store with local-read) | **Single authoritative `audit_events` (control plane) + per-tenant `audit_events_outbox` (drain buffer only) — all four specs aligned.** MCP `WRITE_EXTERNAL` preflight reads control-plane `audit_events` via `mcp_audit_reader` role + `mcp_approval_events` view over mTLS. Latency overhead (~5–10ms intra-VPC) explicit in OPEN-AUDIT-LATENCY-R4. |

### R3 reversals carried into R4 (still binding; aligned with `00-overview.md` consensus)

| Tag | What changed in R3 | Status |
|---|---|---|
| REVISED-C1 | All four scopes of `validated_rules` in one control-plane DB table, RLS-enforced (learning-loop §2.8 DDL adopted) | Carried; canonical |
| REVISED-§13.4 | Cross-tenant promotion uses learning-loop's on-demand `vertical_candidate_aggregates` view; no continuous replicator | Carried |
| REVISED-MCP-GATE | MCP `WRITE_EXTERNAL` gate is direct Postgres `SELECT` via `mcp_audit_reader`, not an HTTP endpoint. **In R4 the read target is the control-plane authoritative store over mTLS** (RESOLVED-R4-CONFLICT-2). | Carried |

### R4 cleanup items resolved this round

| Tag | Item | Section |
|---|---|---|
| RESOLVED-OPEN-OTP-TTL | 24-hour JWT TTL is the platform default; per-tenant override path documented | §6.7 |
| RESOLVED-OPEN-EMIT-ALERT-SCHEMA | Alert payload JSON schema locked with required fields and `severity` enum | §9.6.1 |
| RESOLVED-OPEN-AUDIT-CANON-CONFORMANCE | All control-plane audit-event writes use lower_snake_case `event_type` per learning-loop §11.3; remaining UPPER_SNAKE references in this spec normalized | §9.3 |
| RESOLVED-OPEN-Temporal-IAM | Native Temporal IAM is preferred; spec ships a fallback signal-proxy sidecar contract test so security holds either way | §6.8 |
| RESOLVED-STORAGE-TOPOLOGY | Storage Topology table cross-references learning-loop §2.0 as canonical for audit storage rows | §18 (new) |
| RESOLVED-CONTRACT-TESTS | Single cross-component contract test table | §19 (new) |
| RESOLVED-PROPERTY-TESTS | RLS, JWT, idempotency-key property/fuzz tests | §12.6 (new) |
| RESOLVED-SC-MAP | SC-01..SC-08 control-plane participation map | §20 (new) |

### Carried R2 resolutions (unchanged)

| Tag | Item | Section |
|---|---|---|
| RESOLVED-C3 | Approval signal transport: gateway → Temporal direct | §6.6 |
| RESOLVED-C4 | Provisioning manifest as config-mount; HealthCheck is the ack | §5.6 |
| RESOLVED-C8 | Storage-layer audit immutability (role + trigger) | §9.3 |
| RESOLVED-C12 | Unified `validated_rules` DDL+RLS | §13 |
| RESOLVED-OQ-06 | OTP/JWT issuance: control plane Auth Service is the sole issuer | §6.7 |
| RESOLVED-N4 | WhatsApp session provisioned by `TenantProvisioningWorkflow`; whatsmeow embedded in gateway | §5.2, §5.7 |
| RESOLVED-N6 | Idempotency key composition rule + system-wide registry | §17 |
| RESOLVED-ALERT-SINK | `emit-alert` API specified | §9.6.1 |
| RESOLVED-AUDIT-CANON | Reference learning-loop §11 as canonical schema | §9.3 |
| RESOLVED-REPLAY-SCHED | Evaluation Service exposes `POST /admin/replays` trigger API | §8.7 |
| RESOLVED-N1 | Mode enum `'sandbox' \| 'live'` adopted in this spec | §5.6, §10, §13 |

### Open after Round 5 (categorized; see §15 for full table)

| Category | Items | Owner |
|---|---|---|
| OPEN-PRODUCT-INPUT-X | OQ-05 (legal retention), OQ-07 (vertical mutability), N1 (sandbox-mode canonical encoding — currently confirmed pending all R4 sign-offs) | Product / Legal / All architects |
| OPEN-MEASUREMENT-X | AUDIT-LATENCY-R4 (P99 mTLS Postgres on MCP preflight), AUDIT-DRAIN-LAG (P99 outbox→authoritative drain) | Exec-plane / observability |
| OPEN-CROSS-SPEC-X | OQ-03 (confidence recalculation trigger), OQ-04 (condition-schema parsing), N3 (vertical strip rules), C13 (16-field envelope completeness for `PersistCorrection`, C13-R5), C14 (manifest field alignment), HERMES-VOL (rule-not-on-volume contract test), AUDIT-CANON-GW-ADDITIONS (7 gateway event_types) | Peer architects |

---

## 0. Reading Guide

Sections follow TDD-first ordering inside each service: **invariants → contract tests → implementation shape**. Within each service block the test strategy (invariants and contract tests) appears first, then the implementation sketch (API routes, JSON sketches). No pseudocode and no running assertions appear here.

---

## 1. Purpose and Scope

### What lives in the control plane

The control plane is the shared, multi-tenant orchestration layer. It owns everything that is *about* tenants and their execution planes rather than everything that *runs* within them.

| Belongs in control plane | Belongs in per-tenant execution DB |
|---|---|
| Tenant identity, lifecycle, and provisioning state | Hermes runtime, agent memory, skills |
| Operator and reviewer auth | Temporal worker process |
| Rule Review Console APIs | MCP servers and sandbox adapters |
| Evaluation / regression replay orchestration | `case_runs`, `decision_points`, `artifacts` |
| **`audit_events` (single authoritative store, partitioned by `tenant_id`, RLS-enforced)** | Durable upstream buffer for audit events (drains to control plane) |
| **`validated_rules` (all four scopes — `case`, `tenant`, `vertical`, `default` — single table, RLS-enforced)** | `corrections`, `rule_candidates` (operator free-text PII never crosses to control plane) |
| `skill_versions` (manifest pointing to `validated_rules.id`) | `gateway followup_session` (gateway-internal short-lived state) |
| `tenants`, `workflow_templates`, `channel_bindings` | Sandbox fixtures, MCP read snapshots (artifact store) |
| `vertical_candidate_aggregates` view (PII-stripped, materialized on promotion request) | (Source of the aggregate is per-tenant `rule_candidates`) |
| Billing events and usage aggregation | Case execution, approval packet dispatch |
| Observability control (config, retention policy) | Per-tenant metrics emission |

**Round 3 update.** R2's dual-storage / split-by-trust-boundary positions for both `validated_rules` and `audit_events` are reversed. Both tables are now single authoritative stores in the control-plane DB with RLS enforcing tenant isolation. The trust-boundary protection that R2 sought through physical separation is achieved through RLS + role-grant defense-in-depth (§3.5, §9.3). What stays per-tenant: `corrections` and `rule_candidates`, which carry operator free-text PII that must never pool centrally — those tables live in the per-tenant execution DB and are read into the control plane only via PII-stripped aggregate views (learning-loop §2.2).

### The boundary in one sentence

The control plane knows *that* things happened and *what* was decided; the execution plane knows *how* a specific run proceeded.

### Out of scope for this architect

- Hermes runtime configuration and Temporal worker code (Execution Plane Architect)
- RuleCandidate parsing, condition schema, skill versioning detail (Learning Architect)
- WhatsApp/Telegram gateway, review packet formatting, artifact preview rendering (Operator UX Architect)
- Cross-component integration test orchestration (Devil's Advocate)

---

## 2. Framework Decision: Go

**Decision:** The control plane backend is **Go** (standard library + `chi` router + `wire` compile-time DI).

**Rationale:** The control plane is a multi-service orchestration surface with auth middleware, role-based access, tenant-context propagation, dependency injection for repository and event layers, and structured package boundaries per service domain. Go's `context.Context` propagation, middleware chains (`func(http.Handler) http.Handler`), interface-based DI via `wire`, and compile-time type safety map cleanly onto this shape. Native `crypto/tls` gives first-class mTLS without third-party dependencies. The Temporal Go SDK is Temporal's primary SDK. Single-binary deployment simplifies per-tenant container images and reduces memory footprint.

The HTTP transport for internal RPCs (control plane → execution plane) uses Go's `net/http` client with mTLS; no RPC framework is used across the boundary.

---

## 3. Tenant-Context Propagation Primitives

This section is elevated to its own position because it is the hardest invariant to enforce and the easiest to break.

### 3.1 TDD invariants for tenant-context

- `TENANT_CTX_01`: A request with a valid token for tenant A must never return data rows belonging to tenant B, regardless of filter parameters.
- `TENANT_CTX_02`: A request with no Authorization header must receive 401 before any DB interaction occurs.
- `TENANT_CTX_03`: A request with a forged `X-Victoria-Tenant-Id` header (but legitimate token for tenant A) must be rejected; for internal calls `tenant_id` is derived only from mTLS SAN, not from a client-set header. For inbound JWT calls `tenant_id` is derived only from the JWT `tid` claim.
- `TENANT_CTX_04`: OTel span for every inbound handler must carry attribute `tenant_id`; spans missing this attribute are a CI lint failure.

### 3.2 Contract tests

- **Given** two tenants seeded with distinct rule sets, **then** each tenant's API responses contain only their own records (tenant-isolation smoke test).
- **Given** a control-plane→execution-plane internal mTLS call with a leaf cert whose SAN does not match the request's claimed tenant, **then** the execution plane rejects with 403 + `security_violation`.
- **Given** a JWT with `tid = T_A` and a request body containing a forged `tenant_id = T_B`, **then** the handler ignores the body field and uses the JWT `tid`; cross-tenant access returns 403.

### 3.3 Origin rule

`tenant_id` is **never** read from a client-supplied query parameter, request body field, or URL path segment visible to the client. It is derived exclusively from:

1. The authenticated session token (JWT claim `tid`) — for operator and reviewer requests arriving at the API gateway.
2. The workload-identity credential of the calling execution-plane service — for machine-to-machine calls.
3. The provisioning context embedded by the Tenant Provisioning Workflow — for internally-triggered operations.

Any handler that reads `tenant_id` from `req.query`, `req.body`, or a client-set header is a **critical security defect**.

### 3.4 Propagation chain

```
Inbound request
  └── AuthMiddleware validates JWT (operator/reviewer) OR mTLS terminator extracts SAN (internal)
        └── TenantContextMiddleware injects tenant_id into context.Context
              └── All repository calls receive tenant_id from ctx (not from call site)
                    └── OpenTelemetry span attribute: tenant_id = tenantctx.FromContext(ctx)
                          └── Outbound mTLS call to execution plane: client cert SAN encodes tenant_id
                                └── Audit event: tenant_id embedded at write time
```

### 3.5 Database query scoping

Control-plane Postgres uses a single shared database with a `tenant_id` column on every multi-tenant table. Row-level security (RLS) policies are defined on each table and activated via `SET LOCAL app.current_tenant = $1` at the start of every transaction (transaction-scoped, not connection-scoped — required for correctness under pgBouncer transaction-pooling mode). This is defense-in-depth: even if application-layer context propagation has a bug, the DB layer rejects cross-tenant reads.

**Invariant:** Every table that holds tenant-scoped data MUST have an RLS policy. The schema migration linter enforces this.

---

## 4. Service Inventory

### 4.1 Summary table

| Service | Responsibility | Data store | Blast radius if down |
|---|---|---|---|
| API Gateway | Routing, rate limiting, auth token validation, tenant-context binding | None (stateless) | All inbound traffic stops; execution planes keep running |
| Tenant Registry & Provisioning | Tenant lifecycle, execution-plane provisioning, suspension | Control-plane Postgres (`tenants`, `deployments` tables) | No new tenants; existing tenants unaffected if provisioning idempotent |
| Auth Service | Token issuance, WhatsApp-phone-bind, magic-link for reviewers, service tokens | Control-plane Postgres (`identities`, `sessions`) + Redis (token cache) | Login flow broken; active sessions survive until token expiry |
| Rule Review Console API | RuleCandidate listing (per-tenant via tenant DB connection or via PII-stripped aggregate view), promotion, rollback, replay trigger | Writes `validated_rules` (control plane DB, all scopes — see §13). Reads per-tenant `rule_candidates` directly when reviewing single-tenant candidates; reads `vertical_candidate_aggregates` (learning-loop §2.2) for cross-tenant promotion | Promotion blocked; execution plane continues with existing rules |
| Evaluation Service / Replay Scheduler | Regression replay scheduling (§8.7), replay result aggregation, replay-pass-rate read | Control-plane Postgres (`replay_runs`, `replay_assertions`) | Replay-triggered promotions blocked; does not affect live runs |
| Audit Ingest & Query | Single-store ingest of `audit_events`; query API for reviewers and observability | Control-plane Postgres `audit_events` (single authoritative store, partitioned by `tenant_id`, INSERT-only via `audit_writer` role + trigger). Idempotency on `idempotency_key` UNIQUE | Per-tenant writes buffer in execution plane (§9.4); reviewer query API unavailable until recovery |
| Observability Service | OTel collector config, per-tenant trace retention policy, PII scrub rules | Config in control-plane Postgres; traces in vendor backend (Tempo/Grafana) | Trace collection continues via local OTel collector; policy changes blocked |
| Billing & Admin | Usage event aggregation, plan enforcement, operator admin actions | Control-plane Postgres (`usage_events`, `plans`) | Usage counted on resume; plan checks fail open to last-known plan (see §8) |

### 4.2 Shared infrastructure

- **Control-plane Postgres**: single database, RLS-enforced, runs on managed Postgres (e.g., RDS, Supabase, Neon). Not per-tenant; this is the control metadata plane only.
- **Redis**: session cache, rate-limit counters, idempotency keys for audit ingest. Shared across tenants, keyed by `tenant_id:*`.
- **Temporal server**: Phase 1 uses a **single shared Temporal server** with per-tenant task queues. A dedicated Temporal namespace per tenant is an option for Phase 3 regulated clients. Temporal server downtime affects provisioning and replay workflows but not already-running execution-plane Temporal workflows.
- **Object storage**: control plane uses a separate prefix (`control/`) for audit archives and replay artifacts. Tenant execution-plane storage is entirely separate.

---

## 5. Tenant Lifecycle

### 5.1 Lifecycle states

```
pending_provisioning → active → suspended → deprovisioned
                        ↓                        ↑
                    suspended ───────────── (data retained for N days)
```

Transitions are managed by Temporal workflows in the control plane, ensuring idempotency and auditability.

### 5.2 Provisioning flow

The provisioning process is a Temporal workflow (`TenantProvisioningWorkflow`) with the following activities in order. Each activity is idempotent — retrying from any point is safe.

1. **CreateTenantRecord** — insert into `tenants` table with `status: pending_provisioning`; generate `tenant_id`.
2. **CreateIdentityBinding** — bind the operator's verified phone (or email) identity to the tenant.
3. **ProvisionSecretScope** — create a dedicated namespace in the secrets store (Vault or cloud secret manager); write initial placeholder keys. Three secret scopes are touched at provisioning:
   - **Per-tenant secret scope** at `tenant/<tenant_id>/`: operator business credentials (LLM keys, accounting API tokens), preview-JWT signing key (`messaging/preview_signing_key`), and any tenant-scoped credentials read by exec-plane.
   - **Gateway-only secret scope** at `gateway/tenants/<tenant_id>/`: the signal-only Temporal credential (`temporal_signal_client`, §6.8) and the WhatsApp session-key KMS key (`wa_session_kek`, §5.7). The gateway service identity is the sole reader; whatsmeow accesses the KEK in-process.
   - **Control-plane secret scope** at `ctrl/`: holds platform-wide secrets including the internal CA's signing key (§6.2). No per-tenant entries.
4. **ProvisionPostgresDatabase** — create a dedicated Postgres database for the execution plane (not a schema — a full database for hard isolation at Phase 1). Apply schema migrations including the per-tenant tables (`corrections`, `rule_candidates`, `case_runs`, `decision_points`, `artifacts`, `audit_events_outbox` (durable write-through buffer; see §9.4), `mcp_idempotency_log`).
5. **ProvisionObjectStoreBucket** — create tenant-scoped bucket or prefix in S3/R2.
6. **IssueServiceCertificates** — request leaf certificates from the internal Victoria CA for every workload that participates in mTLS (§6.2): exec-plane (`t_<tenant_id>.exec.victoria.internal`), each MCP server (`t_<tenant_id>.mcp.victoria.internal`), and the per-tenant resource references for the gateway's outbound calls. Certs are written into the per-tenant secret scope (for tenant-scoped workloads) and the gateway secret scope (for the gateway's per-tenant client cert if applicable). Rotation cadence and overlap window per §6.2.
7. **RegisterTemporalTaskQueues** — register `victoria.tenant.{tenant_id}.{workflow_type}` queues in the shared Temporal namespace.
8. **IssueGatewayTemporalCredential** — control plane mints the per-tenant signal-only Temporal credential and stores it in the **gateway-only secret scope** at `gateway/tenants/{tenant_id}/temporal_signal_client` (§6.8). The credential is scoped to `SignalWorkflow` on `victoria.tenant.{tenant_id}.*`.
9. **DeployHermesContainer** — write the provisioning manifest (§5.6) to the read-only config-mount; instruct the execution plane container orchestrator to start a Hermes container with the tenant's data volume; record the internal endpoint.
10. **DeployMCPSidecars** — start sandbox email, drive, invoice MCP containers; inject tenant credentials from the tenant secret scope. Each MCP server presents its `t_<tenant_id>.mcp.victoria.internal` mTLS leaf cert when connecting to the control-plane DB; the `mcp_audit_reader` Postgres role grant on the control-plane `audit_events` (per RESOLVED-R4-CONFLICT-2) admits these connections; SAN-derived `tid` sets `app.current_tenant` for RLS scoping.
11. **DeployTemporalWorker** — start the tenant's Temporal worker process, pointed at the registered queues.
12. **InitWhatsAppSession** (RESOLVED-N4) — provision the per-tenant WhatsApp session: create the `wa-session-data-{tenant_id}` volume for whatsmeow's SQLite session store; issue the session-key KEK and store at `gateway/tenants/{tenant_id}/wa_session_kek` (gateway-only scope per §5.7); register the tenant with the gateway via `POST /channel-bindings` (operator-ux §4.2). The gateway manages the whatsmeow session in-process (see §5.7).
13. **HealthCheck** — wait for the execution plane and the gateway's WhatsApp session for this tenant to return healthy responses on their internal probe endpoints (over mTLS).
14. **UpdateTenantStatus** — set `status: active`; write deployment metadata (internal endpoints, container IDs, resource ARNs, cert serial numbers, gateway-cred IDs) to the `deployments` table.
15. **EmitProvisionedAuditEvent** — write a `tenant_provisioned` event to control-plane `audit_events` (lower_snake_case per §9.3).

**Failure handling:** If any step fails after step 3 (secret scope exists), the workflow retries up to a configured limit, then transitions the tenant to `provisioning_failed`. A compensating workflow can be triggered manually to tear down partial resources. Partial provisioning is never silently swallowed — the tenant record always reflects actual state.

### 5.3 Control plane → execution plane handoff contract

The handoff point is step 13 (HealthCheck). After handshake, the control plane holds:

- `internal_base_url`: the execution plane's private-network base URL (not internet-facing).
- `task_queue_prefix`: e.g., `victoria.tenant.t_123.*`.
- `db_connection_secret_id`: ID of the per-tenant Postgres connection string in the secret store.
- `service_cert_fingerprint`: fingerprint of the exec-plane's mTLS leaf cert; the trust bundle expects SAN `t_<tid>.exec.victoria.internal` (per §6.2).
- `mcp_cert_fingerprints`: fingerprints of each MCP server's mTLS leaf cert with SAN `t_<tid>.mcp.victoria.internal` (used by the control-plane DB's `mcp_audit_reader` role to admit these peers per RESOLVED-R4-CONFLICT-2).
- `gateway_signal_cred_path`: path in the gateway-only secret scope where the control plane stored the gateway's signal-only Temporal credential (`gateway/tenants/<tid>/temporal_signal_client`); see §6.8.

The execution plane exposes the following internal API surface, served over **mTLS** on a private network. mTLS provides mutual identity, transport security, and forward secrecy in a single mechanism (§6.2).

```
POST /internal/replay                — trigger a case replay (called by Evaluation Service / Replay Scheduler, §8.7)
GET  /internal/health                — liveness/readiness probe
POST /internal/rules/cache-hint      — non-authoritative cache invalidation hint after a rule promotion (Consensus A10 / RESOLVED-C11; the authoritative read is exec-plane's pull-at-workflow-start LoadSkillVersion)
POST /internal/candidates/mark-promoted — control plane → execution plane: update per-tenant `rule_candidates.status = 'promoted'` and `promoted_to_rule_id` after a single-store `validated_rules` write commits (§13.5). Idempotent on (tenant_id, candidate_id).
```

**Note on cumulative endpoint changes (R2 → R4).** Endpoints removed under cross-team consensus: `/internal/rules/promote`, `/internal/audit/query`, `/internal/audit/check-approval`. New disposition:
- Rule promotion writes directly to control-plane `validated_rules` (§13); execution plane reads via pull-at-start.
- Audit events: single authoritative store in control plane (`audit_events`); per-tenant `audit_events_outbox` is a durable transactional buffer that drains upstream via `POST /internal/audit/events` (§9.4); not a read source.
- The MCP approval check is a direct Postgres `SELECT` against the **control-plane** `audit_events` via the `mcp_audit_reader` role and the `mcp_approval_events` view, over mTLS (§9.3 / RESOLVED-R4-CONFLICT-2).

The execution plane's outbound mTLS calls to the control plane include the audit ingest endpoint (§9.4), the LoadSkillVersion endpoint (§13.3), and the alert-emit endpoint (§9.6.1). Per-tenant MCP servers' synchronous SELECT to `mcp_approval_events` is also over mTLS (§9.3).

### 5.4 Suspension

On suspension:
- Operator auth tokens are invalidated.
- Execution plane containers are stopped (not destroyed).
- Task queues are paused (Temporal schedules suspended).
- Data is retained.

Resumption reverses these steps; container restart is handled by the same orchestrator primitives as initial deploy.

### 5.5 Deprovisioning

Deprovisioning is a separate Temporal workflow triggered only by explicit admin action (never automatic). It:
1. Suspends the tenant.
2. Exports a final audit archive to long-term object storage.
3. Destroys containers, task queues, bucket contents.
4. Drops the tenant Postgres database.
5. Revokes and deletes the secret scope.
6. Marks the tenant record `status: deprovisioned` (soft delete; row retained for audit).

### 5.6 Provisioning Manifest Delivery (RESOLVED-C4)

**Background.** R1 assumed an HTTP push of the provisioning manifest. The Execution Plane Architect specified (their §2.7 + Appendix B) that the manifest is read by the Hermes container at startup from a config-map mount at `/hermes/config/manifest.json`. The control plane yields to that contract.

**Schema.** The canonical schema is **exec-plane spec §2.7 / Appendix B**. The control plane does not redefine it. The fields the control plane is responsible for populating are listed below; the exec-plane spec is the authority on field types and allowed values.

| Field | Source in control plane |
|---|---|
| `tenant_id` | `tenants.id` (generated at provisioning) |
| `hermes_version` | `deployments.hermes_version` (admin-configured per tenant; defaults to platform-pinned version) |
| `llm_model` | `tenants.feature_flags.llm_model` or platform default |
| `sandbox_mode` | `tenants.sandbox_mode` (immutable post-provisioning) |
| `workflow_templates` | Computed from `tenants.vertical` × `workflow_templates.vertical` join |
| `mcp_endpoints` | Generated by provisioning workflow when MCP sidecars are deployed (private-network URLs) |
| `rule_endpoint` | Fixed control-plane URL for the effective-rule-set resolver: `https://control-plane.victoria.internal/internal/tenants/:tenant_id/rules/effective` |
| `vertical` | `tenants.vertical` |
| `signal_wait_timeouts_hours` | Per-workflow defaults; reconfigurable per tenant via `feature_flags` |

**Transport.**

- The provisioning workflow's `DeployHermesContainer` activity (R1 §5.2 step 7) writes the manifest as a read-only volume / config map at `/hermes/config/manifest.json` **before** the Hermes container starts.
- The container's entrypoint reads the file synchronously (per exec-plane INV-H4: a tenant_id mismatch between manifest and volume halts the container).
- The activity stores the manifest contents in the control-plane `deployments` table for audit and recoverability. Storage of the manifest is keyed by `(tenant_id, manifest_version)` and is append-only.

**Acknowledgement semantics.**

- The `HealthCheck` activity (R1 §5.2 step 10) **is** the manifest acknowledgement. The execution plane's health probe returns 200 only after Hermes has read and validated the manifest. No separate manifest-ack endpoint is required.
- A failed health check after manifest write but before container ready signals a manifest-config mismatch (e.g., `tenant_id` mismatch between manifest and data volume). The provisioning workflow logs the error and retries per Temporal activity policy.

**Retry and partial-failure behavior.**

- Manifest write is idempotent: writing the same manifest content twice produces no observable difference.
- If `DeployHermesContainer` fails after manifest write but before container ready, the next retry overwrites the manifest with the same content.
- If `HealthCheck` fails repeatedly (manifest cannot be successfully read by Hermes), Temporal retries up to a configured limit, then transitions the tenant to `provisioning_failed`. The compensating deprovision workflow (§5.5) tears down the partial deployment.
- The manifest is **never** mutated to "fix" a misconfiguration in place. Any change requires a re-provision (which writes a new manifest version, restarts the container, and re-runs health check).

**Schema versioning.** The control plane tags every emitted manifest with the schema version (`$schema`). If exec-plane introduces a breaking schema change, both the control plane (writer) and execution plane (reader) bump their schema version in lockstep; mismatched schema versions are a hard fail at health check.

**TDD invariants for manifest delivery.**

- `MANIFEST_01`: A manifest written by `DeployHermesContainer` for tenant A must contain `tenant_id: "t_A"`. Writing a manifest for tenant A with `tenant_id: "t_B"` is a critical defect; a contract test asserts the activity input matches the written file.
- `MANIFEST_02`: `HealthCheck` must time out and fail (not succeed silently) if the container does not start within the configured window — manifest-driven container failures must surface as `provisioning_failed`, not as silent partial success.
- `MANIFEST_03`: Re-provisioning a tenant overwrites the manifest atomically; a half-written manifest on disk is impossible (write to temp file + atomic rename).

**Contract test against execution plane.**

- Provision tenant T; assert the manifest exists at the agreed mount path with all required fields populated; assert the Hermes container reaches healthy state; assert the health probe responds 200 only after the manifest has been read.

### 5.7 WhatsApp Session Lifecycle (RESOLVED-N4)

**Decision.** The WhatsApp session is managed by the gateway in-process via the embedded `whatsmeow` library. The control plane's `TenantProvisioningWorkflow` provisions the session storage and secret material; the gateway operates the session. No separate bridge container exists — whatsmeow runs as a goroutine within the gateway process, eliminating the sidecar process boundary.

#### 5.7.1 Invariants

- `INV-WA-01`: After `InitWhatsAppSession` returns success, the gateway's `channel_bindings` row exists for the tenant with `session_status` ∈ `{qr_needed, connecting, active}`. A failure to register the binding is a workflow failure, not a silent skip.
- `INV-WA-02`: Deprovisioning a tenant with an active session MUST disconnect the whatsmeow session before deleting the binding. Reverse order would let the gateway attempt delivery on a cleaned-up session.
- `INV-WA-03`: A WhatsApp session key on `wa-session-data-{tenant_id}` decrypted with tenant T's KMS key must not decrypt with any other tenant's KMS key. Cross-tenant key reuse is a critical defect.
- `INV-WA-04`: The session-key encryption KMS key is held in the **gateway-only secret scope** at `gateway/tenants/<tid>/wa_session_kek` (per operator-ux R4 §4.4 RESOLVED-CredentialStorage-Reconciled, aligned with the gateway-cred placement of §6.8). The gateway service identity is the sole reader.
- `INV-WA-05`: The `wa-session-data-{tenant_id}` volume is mounted only into the gateway container serving tenant T. Cross-mounting into any other container is a critical defect.

#### 5.7.2 Contract tests

- `test_session_provisioned_with_binding`: Run `TenantProvisioningWorkflow` for a fresh tenant; assert the gateway's `channel_bindings` row exists and the whatsmeow session is initialized. Tear down between runs to verify idempotency.
- `test_deprovision_order`: Deprovision a tenant; assert the whatsmeow session disconnect event precedes the `channel_bindings` row deletion in audit trail timestamps.
- `test_kek_isolation`: Provision tenants T_A and T_B; pull a sample encrypted blob from T_A's volume; attempt decryption with T_B's KEK; expect failure (`integrity_check_failed`).
- `test_session_crash_loop`: Force 5 whatsmeow session failures within 10 minutes; assert the gateway halts further reconnection attempts and an alert with `code: "wa_session_crash_loop"` reaches the alert sink (§9.6.1).

#### 5.7.3 Mechanism

**Provisioning** (workflow step 12 in §5.2). The `InitWhatsAppSession` activity:
1. Creates a per-tenant Docker volume `wa-session-data-{tenant_id}` for the whatsmeow SQLite session store (matches operator-ux §4.4).
2. Issues a per-tenant KMS Key Encryption Key (KEK) and stores its reference at `gateway/tenants/<tid>/wa_session_kek` in the **gateway-only secret scope** (re-aligned to operator-ux R4 §4.4 / ctrl-plane R3 §5.7).
3. Calls the gateway's `POST /channel-bindings` (operator-ux §4.2) to register the tenant binding and initialize the whatsmeow session within the gateway process.
4. Returns the session metadata to the workflow for the deployment record.

The session starts in `qr_needed` state. The QR-pairing flow that follows is entirely owned by operator-ux §4.3; the control plane does not participate beyond providing the QR-link URL to the admin UI.

**Restart.** On gateway restart, each tenant's whatsmeow session re-initializes from the persisted SQLite store on `wa-session-data-{tenant_id}`. whatsmeow re-establishes the WebSocket connection without operator re-pairing in the common case. If 5 session failures occur within 10 minutes, the gateway halts further reconnection for that tenant and emits an alert via the §9.6.1 emit-alert API.

**Secret rotation.** The session-key KEK has a 90-day rotation cadence managed by the control plane's secret-rotation scheduler. Rotation is online: a new key version is added; the gateway re-encrypts the session store on the next idle window; the old version is retained for the overlap window then revoked. Rotation does not require operator re-pairing.

**Deprovisioning** (extends §5.5). Disconnect the whatsmeow session; destroy `wa-session-data-{tenant_id}` (after exporting the encrypted blob to long-term archive if compliance retention applies); revoke the KEK; call the gateway's `DELETE /channel-bindings/:tenant_id`. Session-disconnect precedes binding-delete.

**Suspension** (extends §5.4). On suspension the whatsmeow session is disconnected (volume retained), and the gateway's `channel_bindings.session_status` is updated to `suspended` via a control plane → gateway internal call. Resumption re-initializes the session from the persisted store.

---

## 6. Auth Model

### 6.1 Two distinct identity populations

**Operators** are SMB owners. They authenticate via WhatsApp-bound phone identity. At provisioning, the operator's phone number is verified (OTP over WhatsApp) and bound to their `tenant_id` in the `identities` table. Subsequent API calls from the operator carry a short-lived JWT issued by the Auth Service, where the `sub` is the operator's identity ID and the `tid` claim is their `tenant_id`. The JWT is issued to the messaging gateway (Operator UX domain) after WhatsApp verification; the operator never directly calls the control plane API from a browser.

**Reviewers** are internal Victoria staff. They authenticate via magic-link email (Phase 1) or SSO (Phase 2). Reviewer JWTs carry `role: reviewer` and optionally a `tenant_assignment` claim scoping them to specific tenants. A reviewer without `tenant_assignment` can see all tenants; one with it sees only assigned tenants.

**No operator ever receives reviewer-level access and vice versa.** The identity types are disjoint.

### 6.2 Service-to-service auth: mTLS (RESOLVED-R4-CONFLICT-1; carried R3)

**Decision.** All service-to-service calls between control plane, execution plane, messaging gateway, and per-tenant MCP servers use **mutual TLS** with leaf certificates issued by an internal Victoria CA. R3's mTLS choice stands; the mid-R4 reversal to HMAC-bearer is withdrawn. This matches exec-plane §13.1 ("Accept ctrl-plane's pick ... use mutual TLS with certificates issued by an internal CA"), operator-ux §4.3 ("mTLS-authenticated per ctrl-plane §6.2"), and learning-loop §2.0.1 (MCP audit reads "going there over mTLS").

**Why mTLS.** Mutual identity, transport security, and forward secrecy in a single mechanism. Identity is in the certificate (SAN), not in JWT-style claims at the application layer. Cert rotation is online via short-lived leaves. No bearer-token or HMAC-secret distribution problem; no replay-protection cache; no per-call signing.

#### 6.2.1 Invariants

- `INV-AUTH-S2S-01`: Every internal call between control plane and execution plane terminates mTLS at the listener. A non-mTLS connection is rejected at the TLS listener before any handler runs (TLS layer rejection — no auth filter equivalent at the application layer is required).
- `INV-AUTH-S2S-02`: The leaf certificate's SAN encodes the workload identity and tenant binding. Examples:
  - Exec-plane workload for tenant `t_123`: `t_123.exec.victoria.internal`
  - Per-tenant MCP server for tenant `t_123`: `t_123.mcp.victoria.internal`
  - Control-plane services: `<service>.ctrl.victoria.internal` (e.g., `audit-ingest.ctrl.victoria.internal`, `evaluation.ctrl.victoria.internal`, `auth.ctrl.victoria.internal`)
  - Gateway: `gateway.victoria.internal`
- `INV-AUTH-S2S-03`: Tenant scoping for per-tenant peers is derived from the SAN. The verifier extracts the `tid` from the SAN (the `t_<tenant_id>` prefix) and sets `app.current_tenant = tid` for the handler's DB session. RLS enforces row visibility.
- `INV-AUTH-S2S-04`: A peer presenting cert for tenant T_A cannot access resources of tenant T_B. The handler's authorization rule asserts `request_target_tenant_id == sanExtract(peerCert).tid` for tenant-scoped endpoints; mismatch returns 403 and emits `security_violation`.
- `INV-AUTH-S2S-05`: Cert rotation is online. Each workload has at most two active leaves at any time (current + previous, 24-hour overlap). The verifier trusts both during the overlap; issuers always present current.
- `INV-AUTH-S2S-06`: A failed mTLS handshake (expired cert, untrusted CA, SAN mismatch) is a non-retryable error. The execution plane and gateway do NOT fall back to plaintext, bearer tokens, or any other mechanism. (Mode-default-deny per exec-plane §13.1.)
- `INV-AUTH-S2S-07`: Public-edge requests (operator/reviewer browsers, gateway public webhook) terminate edge TLS at the LB plus JWT (operator/reviewer) or provider-specific HMAC on the webhook payload (gateway). Public-edge requests do not present client certs; they never enter the internal mTLS trust circle.

#### 6.2.2 Contract tests

- `test_mtls_handshake_required`: Open a plaintext TCP connection to any internal listener; assert connection rejected (no TLS) or TLS without client cert rejected (handshake failure). No DB write occurs.
- `test_mtls_san_workload_identity`: Present a leaf cert with SAN `t_A.exec.victoria.internal` to the audit-ingest endpoint; submit an event for tenant `t_A`; expect 200. Submit an event for tenant `t_B` with the same cert; expect 403 + `security_violation`.
- `test_mtls_untrusted_ca_rejected`: Present a cert signed by a non-Victoria CA; assert handshake failure.
- `test_mtls_expired_cert_rejected`: Present a leaf with `notAfter` in the past; assert handshake failure with `cert_expired`.
- `test_mtls_san_mismatch_rejected`: Present a cert whose SAN does not match the request's claimed tenant; assert 403 + `security_violation`.
- `test_mtls_rotation_overlap`: Rotate a workload's cert; assert connections using either the previous or current leaf succeed during the 24-hour overlap; assert connections using neither fail.

#### 6.2.3 Mechanism

| Layer | Detail |
|---|---|
| Trust anchor | Internal Victoria CA (issued + held in the control-plane secret scope; root cert is preloaded into every workload's trust bundle at provisioning) |
| Leaf cert profile | X.509 with SAN encoding workload identity + tenant binding (per `INV-AUTH-S2S-02`) |
| Issuance | The provisioning workflow's `IssueServiceCertificates` activity (§5.2 step 6) requests leaves for exec-plane, MCP servers, and the gateway from the CA. Certs are written into the appropriate secret scope (per-tenant for tenant-scoped workloads; gateway scope for the gateway). |
| TTL | 30 days (short enough to bound revocation risk; long enough that rotation overhead is modest at ≤10 tenants) |
| Rotation | 30-day cadence (control-plane scheduler); 24-hour overlap window during which both old and new leaves are accepted |
| Tenant scoping | SAN-derived; the auth filter (TLS layer + lightweight middleware) extracts the `tid` from SAN and sets `app.current_tenant = tid` for the request's DB session |
| Trust bundle | Each workload's trust bundle contains the CA root + an explicit allow-list of expected peer SANs (defense in depth: even with a valid leaf, an unexpected SAN is rejected) |

**Per-call lifecycle (caller side):**
1. Caller loads its current leaf cert + private key from its workload-identity-scoped secret store at startup; refreshes on a 1-hour schedule.
2. Caller initiates a TLS connection presenting the leaf during handshake.

**Per-call lifecycle (verifier side):**
1. TLS listener requires client cert; rejects connection on missing/invalid cert (handshake failure).
2. The accepted cert chain is validated against the trust bundle; SAN extracted.
3. Auth filter middleware sets `app.current_tenant` from SAN-derived `tid` for the handler's DB session.
4. Authorization rule asserts the request's target tenant_id matches the SAN-derived `tid` (where applicable).
5. Handler runs.

**Application:**

| Internal call | Authentication |
|---|---|
| Execution plane → control plane: `LoadSkillVersion` (§13.3) | mTLS; client SAN `t_<tid>.exec.victoria.internal`; server SAN `skills.ctrl.victoria.internal` |
| Execution plane → control plane: audit drain to `/internal/audit/events` (§9.4) | mTLS; client SAN `t_<tid>.exec.victoria.internal`; server SAN `audit-ingest.ctrl.victoria.internal` |
| Execution plane → control plane: alert emit (§9.6.1) | mTLS; client SAN `t_<tid>.exec.victoria.internal` |
| MCP server (per-tenant) → control plane: audit-events SELECT (§9.3) | mTLS; client SAN `t_<tid>.mcp.victoria.internal`; SAN extraction sets `app.current_tenant = tid`; the `mcp_audit_reader` Postgres role grant is on the control-plane DB only (RESOLVED-R4-CONFLICT-2) |
| Control plane → execution plane: `/internal/replay` (§5.3, §8.7) | mTLS; client SAN `evaluation.ctrl.victoria.internal`; server SAN `t_<tid>.exec.victoria.internal` |
| Control plane → execution plane: `/internal/health` | mTLS; client SAN `health-check.ctrl.victoria.internal` |
| Gateway → control plane: emit-alert (§9.6.1) | mTLS; client SAN `gateway.victoria.internal` |
| Gateway → control plane: emit-audit (operator-ux §9.7) | mTLS; client SAN `gateway.victoria.internal` |
| Gateway → Temporal cluster (signal-only) | Temporal client credential per §6.8 (separate from internal mTLS); transport itself is mTLS to the Temporal listener |
| Reviewer/operator inbound to control plane | Edge TLS + JWT (§6.3 onward); not an internal mTLS path |
| Public webhook inbound to gateway (WhatsApp / Telegram) | Edge TLS + provider-specific HMAC on webhook payload (operator-ux §4); not an internal mTLS path |

### 6.3 tenant_id binding to identity

At login, the Auth Service:
1. Validates the credential (OTP verification or magic-link).
2. Looks up the `identity` record — which already contains `tenant_id` from provisioning.
3. Issues a JWT with `tid: <tenant_id>` embedded as a server-set claim.

The client receives the JWT. The client **cannot influence `tid`**; it is always derived server-side from the verified identity.

### 6.4 Role model

| Role | Auth method | Scope | Can do |
|---|---|---|---|
| `operator` | WhatsApp OTP → JWT | Single tenant (their own) | View own CaseRuns, Artifacts, submit corrections via messaging gateway |
| `reviewer` | Magic-link or SSO → JWT | All tenants OR assigned tenants | List/promote/reject RuleCandidates, trigger replays, view audit log |
| `admin` | SSO + MFA → JWT | Platform-wide | Provision/suspend/deprovision tenants, manage billing, manage reviewer assignments |
| `service` | mTLS leaf cert (per §6.2) | Tenant-scoped via SAN-derived `tid` | Write audit events, fetch SkillVersion, report health, emit alerts |

### 6.5 TDD invariants for auth

- `AUTH_01`: A JWT with `role: operator` must be rejected by any endpoint under `/internal/*` or `/review/*`.
- `AUTH_02`: A JWT with `role: reviewer` must be rejected by any endpoint that modifies operator-facing execution state (e.g., triggering a live workflow run). Reviewers can only trigger replays in the evaluation environment.
- `AUTH_03`: A request with an expired JWT must receive 401, not a stale response from cache.
- `AUTH_04`: The `tid` claim in a JWT must match the tenant owning the resource being accessed; cross-tenant access returns **403**, not 404. Rationale: 404 is the conventional security choice to avoid leaking resource existence, but at ≤10 tenants where all tenants are known to Victoria staff and all operators are authenticated to a specific tenant, the incremental information leakage of a 403 is negligible. The 403 is operationally clearer for debugging misconfigured reviewer assignments. If this reasoning does not survive Devil's Advocate challenge, this invariant flips to 404 at zero implementation cost.
- `AUTH_05`: Service tokens must not be accepted on operator-facing endpoints.

### 6.6 Approval Signal Transport: Gateway → Temporal Direct (RESOLVED-C3)

**Decision.** Operator approval signals flow **directly from the messaging gateway (Operator UX) to the execution plane's Temporal worker**. The control plane is **not** on the hot path. This matches operator-ux's §10.1 and exec-plane's §7.2 assumption.

**Defended rationale.**

1. **Availability.** The control plane must be allowed to fail without blocking operator approvals. R1 §11.1 already established that the execution plane keeps running when the control plane is down — that promise is incompatible with routing signals through the control plane.
2. **Latency.** Inserting a control-plane hop adds a network round-trip and an auth check on the critical path of every operator approval. There is no business value created by that hop.
3. **Auditability is preserved without it.** The exec-plane's Temporal worker emits an `approval_received` audit event upstream to the control-plane audit ingest endpoint as a side effect of processing the signal. The control plane sees every approval — it just doesn't proxy them. This uses the same buffering contract already established in R1 §9.4.
4. **Authorization is enforced at the gateway, not the control plane.** The gateway has already authenticated the operator (WhatsApp session bound to tenant) and derived `tenant_id` from the bot token. Re-authorizing at the control plane would duplicate work without adding security.

**Gateway → Temporal contract.**

- The gateway addresses the signal to the tenant's task queue using the `WorkflowID = case_run_id` convention (exec-plane INV-CR1).
- The gateway uses Temporal's signal API directly (not an HTTP proxy through the control plane).
- The signal envelope is the `ApprovalSignalEnvelope` shape defined in operator-ux §2.3, with `signal_id` (UUID v4) and `gateway_idempotency_key` for dedup. The control plane does not redefine this shape.

**What the control plane DOES on the audit path (asynchronous).**

- The execution plane's signal handler (within the Temporal workflow) emits an `APPROVAL_RECEIVED` audit event to the control-plane audit ingest endpoint after the signal has been processed.
- The audit event payload includes `signal_id` and `gateway_idempotency_key` for cross-system correlation back to the messaging gateway's records.
- The control plane uses these IDs to reconcile (in the audit query API) the gateway's view of "I sent this approval" with the workflow's view of "I processed this approval." A reconciliation gap (gateway sent, no audit ingested within N minutes) raises an internal alert.

**Trade-offs explicitly accepted.**

- The Temporal cluster's network must be reachable from the messaging gateway. This is a private-network connection within the same VPC; no public Temporal exposure.
- The gateway holds a per-tenant **signal-only** Temporal credential. **Provisioning is owned by the control plane** (§6.8 and §5.2 step 8); the credential lives in a dedicated **gateway secret scope** (separate from the tenant secret scope used by the execution plane) and is scoped to signal-only authorization on `victoria.tenant.{tenant_id}.*`.
- The credential's blast radius is bounded: it cannot start, terminate, cancel, or query workflows; only signal them.

**TDD invariants for signal transport.**

- `SIGNAL_01`: A control-plane outage must not prevent the gateway from delivering an approval signal to a healthy execution plane. The contract test simulates control-plane unavailability and verifies the workflow advances on signal arrival.
- `SIGNAL_02`: An `APPROVAL_RECEIVED` audit event must be ingested by the control plane within a configurable SLA (default 5 minutes) of the corresponding signal being processed. A reconciliation job alerts on missing events past SLA.
- `SIGNAL_03`: The control plane must reject any attempt to receive or proxy an approval signal directly — there is no `/approve` endpoint on the control plane API.

### 6.7 Operator OTP and JWT Issuance (RESOLVED-OQ-06)

**Decision.** The **control plane Auth Service is the sole issuer** of operator JWTs. The messaging gateway is a caller, not an issuer. This confirms operator-ux's A7 assumption.

**OTP-verify endpoint.**

```
POST /auth/operator/otp/initiate
  Body:    { tenant_id: string, phone_e164: string }
  Auth:    mTLS (per §6.2); gateway client cert SAN `gateway.victoria.internal`; gateway must be authorized for `target_tenant_id` per its IAM policy
  Returns: { otp_request_id: string, expires_at: ISO8601 }
  Effect:  Generates a 6-digit OTP, stores hash + nonce + tenant_id + phone_e164 in
           `otp_requests` table with TTL = 5 minutes. Auth Service then asks the
           messaging gateway to deliver the OTP via WhatsApp (out-of-band call).

POST /auth/operator/otp/verify
  Body:    { otp_request_id: string, otp: string }
  Auth:    mTLS (per §6.2); gateway client cert SAN `gateway.victoria.internal`
  Returns: { jwt: string, expires_at: ISO8601, refresh_token: string }
  Effect:  Validates OTP against the stored hash; on success, marks the otp_request
           as consumed (one-time use); issues JWT with claims below.
```

**JWT claims.** Server-set; the gateway has no influence over claim values:

| Claim | Type | Value |
|---|---|---|
| `iss` | string | `https://auth.victoria.internal/` |
| `sub` | string | Internal operator identity ID (e.g., `op_abc123`); never the phone number |
| `tid` | string | Bound tenant ID, looked up from the `identities` table at issuance time |
| `role` | string | `operator` |
| `scope` | string array | `["correction:write", "case_run:read_own"]` (Phase 1 fixed scope) |
| `iat` | int | Issued-at timestamp |
| `exp` | int | Expiration timestamp; **TTL = 24 hours** (default; see RESOLVED-OPEN-OTP-TTL, §6.7) |
| `jti` | string | UUID v4; used for revocation list and replay protection |
| `nonce` | string | Random; bound to the OTP request to defeat replay of a verified OTP exchange |

**TTL and refresh (RESOLVED-OPEN-OTP-TTL).**

- **Access JWT TTL: 24 hours** is the platform default. Override path: `tenants.feature_flags.access_jwt_ttl_seconds` (admin-set); allowed values 3600..604800 (1 hour to 7 days). Values outside that band require an explicit security exception flagged on the tenant record.
- **Refresh token TTL: 30 days** (default; override via `tenants.feature_flags.refresh_token_ttl_seconds`).
- Refresh tokens are opaque (not JWTs), stored hashed in `refresh_tokens` table, single-use, rotated on every refresh.
- Refresh endpoint: `POST /auth/operator/refresh` with `{refresh_token}` returns a new access JWT and a new refresh token; old refresh token is invalidated.
- A revoked operator (e.g., tenant suspension) has all refresh tokens revoked. Active access JWTs continue until expiry; for short-window revocation, the `jti` deny list in Redis (TTL = remaining JWT lifetime) is checked at every `/internal/*` and `/review/*` boundary.

**Replay protection.**

1. **OTP one-time use.** `otp_requests.consumed_at` is set on first successful verify; subsequent verify attempts with the same `otp_request_id` return `OTP_ALREADY_CONSUMED`.
2. **OTP nonce binding.** The JWT carries a `nonce` claim derived from the OTP request; if the same OTP is somehow re-played and produces a different JWT, the two JWTs have different `nonce` values, allowing audit reconciliation.
3. **JWT `jti` claim.** Revoked JWTs are added to a Redis set with TTL = JWT remaining lifetime. Every authenticated request checks `jti` against the deny list before passing the auth guard.
4. **Refresh-token rotation.** A refresh token is single-use; reusing it invalidates the entire refresh-token chain for that operator and emits a `security_violation` audit event.

**Tenant-context propagation binding.** The JWT `tid` claim is the source for the `TenantContext` set in AsyncLocalStorage by the auth guard (§3.4). No request handler reads `tenant_id` from any other source.

**TDD invariants for OTP/JWT issuance.**

- `INV-OTP-01`: An OTP submitted twice for the same `otp_request_id` succeeds at most once. The second attempt returns `OTP_ALREADY_CONSUMED` (HTTP 409).
- `INV-OTP-02`: A refresh token used twice invalidates the entire chain and emits a `security_violation` audit event.
- `INV-OTP-03`: A JWT issued for tenant T_A with a tampered `tid` claim must fail signature verification. The Auth Service signing key is held in a dedicated KMS scope and never leaves the JWT signer service.
- `INV-OTP-04`: The OTP-initiate request body's `tenant_id` must match the calling gateway's mTLS SAN-derived workload identity (§6.2). For platform-scoped gateway calls, an additional explicit `target_tenant_id` is rejected unless the gateway's authorization claim accepts that tenant. Mismatch returns 403 and emits a `security_violation` audit event.
- `INV-OTP-05`: The platform default JWT TTL is 24 hours. A `tenants.feature_flags.access_jwt_ttl_seconds` override outside `[3600, 604800]` raises a configuration validation error at provisioning or admin-update time.

### 6.8 Gateway Temporal Credentials (carried R3; aligned with operator-ux R4 §10.4)

**Decision.** The control plane provisions the gateway's per-tenant signal-only Temporal credential and stores it in a **gateway-only secret scope** at `gateway/tenants/{tenant_id}/temporal_signal_client`. The mid-R4 reversal (per-tenant secret scope, exec-plane provisions) is withdrawn. This re-aligns to R3 and matches operator-ux R4 §10.4 ("Per-tenant signal-only credentials are stored in the **gateway-only secret scope** ... R3 path `tenant/{tenant_id}/messaging/temporal_signal_client` is replaced with `gateway/tenants/{tenant_id}/temporal_signal_client`") and operator-ux RESOLVED-CredentialStorage-Reconciled.

**Why gateway-only secret scope.** The gateway is the sole consumer of this credential. A gateway-only secret scope bounds the read surface to one component; the scope is disjoint from per-tenant secret scopes used by exec-plane and from Temporal admin scopes. Provisioning ownership stays with the control plane (`TenantProvisioningWorkflow`) — symmetrical with how the control plane provisions every other per-tenant runtime resource (Hermes container, MCP sidecars, WhatsApp session storage).

#### 6.8.1 Invariants

- `INV-GW-CRED-01`: The gateway credential cannot start a Temporal workflow (`StartWorkflow`, `SignalWithStartWorkflow`).
- `INV-GW-CRED-02`: The gateway credential cannot terminate, cancel, or reset a workflow (`TerminateWorkflow`, `CancelWorkflow`, `ResetWorkflow`, `RequestCancelWorkflow`).
- `INV-GW-CRED-03`: The gateway credential cannot describe, query, or get history of a workflow (`DescribeWorkflowExecution`, `QueryWorkflow`, `GetWorkflowHistory`, namespace admin).
- `INV-GW-CRED-04`: The gateway credential for tenant T_A cannot signal any workflow on a task queue under `victoria.tenant.t_B.*`. Cross-tenant cred reuse is rejected at the Temporal authorization layer.
- `INV-GW-CRED-05`: The gateway service identity has READ access only to the `gateway/tenants/*/` subtree (where `*` ranges over provisioned tenants). Any attempt to read a per-tenant secret scope path (e.g., `tenant/<tid>/llm_provider_key`) from the gateway fails at the secret store's authorization layer.
- `INV-GW-CRED-06`: A revoked credential becomes unusable within the rotation overlap window (1 hour). The rotation schedule guarantees no gateway runs on a credential older than 25 hours.

#### 6.8.2 Contract tests

- `test_temporal_cred_signal_only`: Load tenant T_X's credential from `gateway/tenants/t_X/temporal_signal_client`; attempt 5 forbidden operations (`StartWorkflow`, `TerminateWorkflow`, `CancelWorkflow`, `ResetWorkflow`, `DescribeWorkflowExecution`); assert all 5 fail with authorization error. Then attempt `SignalWorkflow` on `victoria.tenant.t_X.quote_drafting`; assert success.
- `test_temporal_cred_cross_tenant_rejected`: Load tenant T_A's credential; attempt `SignalWorkflow` on `victoria.tenant.t_B.*`; assert authorization error.
- `test_gateway_iam_scope`: Authenticate as the gateway service identity; attempt to read `gateway/tenants/t_X/temporal_signal_client` (expect success), `tenant/t_X/llm_provider_key`, `tenant/t_X/messaging/preview_signing_key` (each expect 403 — the gateway has no read access to per-tenant secret scopes).
- `test_native_or_sidecar_fallback`: Run the contract suite both against (a) a Temporal cluster with native fine-grained IAM, and (b) the signal-proxy sidecar fallback (operator-ux §2.3). Assert identical security guarantees in both modes.

#### 6.8.3 Mechanism

| Field | Value |
|---|---|
| Provisioner | Control plane (`TenantProvisioningWorkflow`, §5.2 step 8 `IssueGatewayTemporalCredential`) |
| Storage path | `gateway/tenants/{tenant_id}/temporal_signal_client` (gateway-only secret scope) |
| Reader IAM | Gateway service identity has READ on `gateway/tenants/*/` only; no access to per-tenant secret scopes |
| Subject | `t_<tenant_id>.gateway.signal` |
| Scope | `SignalWorkflow` only on namespaces / task queues matching `victoria.tenant.{tenant_id}.*` |
| Rotation cadence | 24 hours; old credential overlap window 1 hour |
| Rotation owner | Control plane scheduler |

**Rotation flow:**
1. Control-plane scheduler fires hourly, selects credentials within 4 hours of expiry.
2. Mints a new credential with the same scope; writes the new version to `gateway/tenants/{tenant_id}/temporal_signal_client`.
3. Gateway polls its secret cache (15-min TTL); on next miss it loads the new version.
4. After 1-hour overlap, control plane revokes the old credential at the Temporal cluster.
5. `audit_events` records `gateway_temporal_cred_rotated` (writer: control plane provisioning service).

**Failure mode: rotation failure.** If control-plane rotation fails for tenant T (e.g., Temporal admin API unavailable), the existing credential continues to work until expiry. The control plane retries with backoff and emits an alert via §9.6.1. If the existing credential expires before rotation completes, the gateway cannot deliver signals for tenant T — Temporal's per-workflow `signal_wait_timeout` (exec-plane §3.5) absorbs the delay; on credential restoration the gateway's `outbound_queue` drain delivers backed-up signals.

**RESOLVED-OPEN-Temporal-IAM.** Whether Temporal's IAM model natively supports a "SignalWorkflow on task-queue glob, nothing else" permission depends on Temporal version and Cloud-vs-OSS deployment. The control plane defines the policy and either implements directly (path A) or via a fallback signal-proxy sidecar (path B):

- **If native fine-grained IAM exists (path A):** the credential is a Temporal API key with the scope in §6.8.3.
- **If not (path B):** a per-tenant signal-proxy sidecar (operator-ux §2.3) deployed in the exec-plane network exposes only `SendSignal(case_run_id, signal_name, payload)` over mTLS; the gateway calls the sidecar; the sidecar holds the unrestricted Temporal client. Cross-tenant safety is enforced at the sidecar (`workflow_id` ↔ tenant binding). The contract test `test_native_or_sidecar_fallback` runs the security-property suite in both modes so the spec ships either way.

---

## 7. Rule Review Console

### 7.1 Purpose

The Rule Review Console is an internal web tool (plus API) used by Victoria reviewers to:
- Browse `RuleCandidates` across all tenants, filterable by status, workflow type, and confidence score.
- Inspect source case runs and corrections that generated a candidate.
- Promote a candidate to a `ValidatedRule`.
- Reject a candidate (with a reason).
- Roll back a `ValidatedRule` to its previous version.
- Trigger a regression replay before promotion.
- View replay results before confirming promotion.

### 7.2 TDD invariants for Rule Review Console

- `REVIEW_01`: `POST /review/candidates/:id/promote` must fail if the candidate `status` is not `candidate` or `under_review`; promoting an already-promoted or rejected candidate is a 409.
- `REVIEW_02`: Promotion must write a `ValidatedRule` atomically with setting candidate `status: promoted`; partial states (promoted rule without closed candidate) must not persist.
- `REVIEW_03`: A reviewer assigned to tenant A must receive 403 on `GET /review/candidates/:id` where the candidate belongs to tenant B (and reviewer lacks cross-tenant scope).
- `REVIEW_04`: Rollback must not activate a rule version that was previously `rolled_back`; rollback chains must resolve to the nearest `active` ancestor.
- `REVIEW_05`: `POST /review/candidates/:id/replay` must fail if the execution plane health check for that tenant returns unhealthy.

### 7.3 Contract tests with execution plane

- Happy path: promote a rule for tenant X; assert the execution plane's next `LoadSkillVersion` call returns a manifest including the new rule and the next relevant case run reflects it.
- Cross-tenant isolation: promote a rule for tenant A; assert tenant B's `LoadSkillVersion` call does not include it.

### 7.4 API surface

All endpoints require `role: reviewer` (or `admin`).

```
GET  /review/candidates                     List RuleCandidates (filters: tenant_id, workflow_type, status, min_confidence)
GET  /review/candidates/:id                 Get a single RuleCandidate with full evidence chain
GET  /review/candidates/:id/case-runs       Get source CaseRun summaries referenced by the candidate
POST /review/candidates/:id/replay          Trigger regression replay for this candidate; returns replay_run_id
GET  /review/replays/:replay_run_id         Get replay run status and assertion results
POST /review/candidates/:id/promote         Promote to ValidatedRule (optionally requires completed replay)
POST /review/candidates/:id/reject          Reject with reason
GET  /review/rules                          List ValidatedRules (filters: tenant_id, workflow_type, scope, status)
GET  /review/rules/:id                      Get ValidatedRule with full provenance
POST /review/rules/:id/rollback             Roll back to prior version; creates new version with status rolled_back
GET  /review/tenants/:tenant_id/candidates  Scoped shortcut
GET  /review/tenants/:tenant_id/rules       Scoped shortcut
```

### 7.5 RuleCandidate fields exposed

The console exposes the full candidate shape from the product spec, plus review-layer additions:

| Field | Source | Notes |
|---|---|---|
| `id`, `tenant_id`, `workflow_type`, `decision_type` | Learning Architect's data model | Read-only in console |
| `conditions`, `recommended_action`, `scope` | Learning Architect's data model | Read-only in console |
| `confidence`, `evidence_count` | Learning Architect's data model | Displayed as progress indicator |
| `source_correction_ids`, `source_case_run_ids` | Learning Architect's data model | Used to fetch evidence chain |
| `conflicts_with` | Learning Architect's data model | Surfaced as warnings |
| `status` | Learning Architect's data model | Reviewer may transition to `under_review`, `rejected`, `promoted` |
| `review_notes` | Control plane adds | Reviewer free-text field on reject/promote |
| `replay_run_ids` | Control plane adds | IDs of any replays triggered for this candidate |
| `promoted_at`, `promoted_by` | Control plane adds at promotion time | Written to ValidatedRule, not back to candidate |

**Assumption flagged for Learning Architect:** The condition schema (`conditions` array with `field`, `operator`, `value`) is assumed to come from the Learning Architect's domain. The console renders these as read-only structured data; it does not modify condition logic.

### 7.6 Promotion flow

1. Reviewer opens candidate, reviews evidence chain and any existing replay results.
2. (Optional but recommended) Reviewer triggers replay via `POST /review/candidates/:id/replay`.
3. Replay runs against the source case run inputs using the candidate rule injected. Results show pass/fail per assertion.
4. If replay passes (or reviewer overrides), reviewer posts to `/review/candidates/:id/promote`.
5. Control plane writes `ValidatedRule` record with provenance fields.
6. (Optional, async after commit) Control plane emits a non-authoritative cache-invalidation hint to the execution plane via `POST /internal/rules/cache-hint` (§13.3). This is purely advisory; the authoritative consumption is the execution plane's next `LoadSkillVersion` call at workflow start.
7. Audit event `rule_promoted` written with reviewer identity, candidate ID, validated rule ID, and replay run ID.

The new rule becomes effective when the execution plane's next workflow start calls `LoadSkillVersion` (§13.3), which returns the updated `SkillVersion` manifest. If the cache-invalidation hint fails to deliver, no business impact occurs — the next workflow start picks up the new rule via pull.

### 7.7 Rollback flow

`POST /review/rules/:id/rollback`:
1. Creates a new `ValidatedRule` record with `rollback_of: <id>`, copying conditions from the version being rolled back to (i.e., the prior version, resolved from `supersedes` chain).
2. Sets old rule `status: rolled_back`.
3. (Optional, async after commit) Emits a non-authoritative cache-invalidation hint to the execution plane (§13.3). The rollback rule becomes effective at the execution plane's next `LoadSkillVersion` call.
4. Writes `rule_rolled_back` audit event.

---

## 8. Evaluation Service

### 8.1 TDD invariants for Evaluation Service

- `EVAL_01`: A replay must never mutate production state in the execution plane; the execution plane's replay mode must return results without persisting artifacts or sending operator packets.
- `EVAL_02`: `replay_pass_rate` must be computable even if some case runs are no longer available (mark those assertions as `skipped`, not `failed`).
- `EVAL_03`: Two replays for the same candidate triggered concurrently must not interfere; replay runs are isolated by `replay_run_id`.
- `EVAL_04`: A replay for tenant A must not invoke the execution plane of tenant B.
- `EVAL_05`: The evaluation service must not store LLM-generated content from replays beyond the assertion result and a brief summary; full replay artifacts live in the execution plane's evaluation store.

### 8.2 Contract tests with execution plane

- Replay request triggers execution and returns structured assertion data.
- Execution plane returns `replay_mode: true` flag on all events emitted during a replay; the evaluation service rejects events without this flag.
- Replay isolation: confirm production `CaseRun` records are unmodified after a replay.

### 8.3 What a regression replay is in this product

A **regression replay** takes a historical `CaseRun` (its inputs, the decision points, and expected outputs as captured from a prior approved execution) and re-executes the workflow against those exact inputs in an isolated evaluation environment, with a candidate or updated rule set injected. The replay does not send operator packets; it does not modify production state; it does not touch the live execution plane.

The purpose is to answer: *"If we promote this rule, does it cause the workflow to make the same correct decisions it made on previously-approved cases, and does it resolve the case that triggered the candidate?"*

### 8.4 What a replay asserts

Each replay run produces a set of `ReplayAssertion` records:

| Assertion type | Description | Pass condition |
|---|---|---|
| `decision_match` | Does the replayed decision at a given `decision_point_id` match the expected decision from the approved run? | Replayed decision = expected decision |
| `artifact_equivalence` | Is the generated artifact (e.g., draft email text) semantically equivalent to the approved artifact? | LLM-judge similarity above threshold (threshold is configurable, not hardcoded) |
| `correction_resolution` | Does the candidate rule, when applied, produce the decision that the source correction requested? | Replayed decision = correction's requested action |
| `no_regression` | Do all previously-approved `CaseRun` assertions still pass with the new rule set? | All prior `decision_match` assertions pass |

**Deliberate limitation:** The evaluation service runs replays but does not run live Hermes/LLM inference directly. It sends a replay instruction to the execution plane's `/internal/replay` endpoint, which runs the case in a sandboxed evaluation mode within the execution plane. The control plane collects and aggregates results. This keeps LLM inference co-located with the execution plane's Hermes instance rather than re-implementing it centrally.

### 8.5 Confidence-score boundary

This is an explicit boundary question between this spec and the Learning Architect's domain.

**What the control plane (Evaluation Service) owns:**
- `replay_pass_rate`: the fraction of `no_regression` assertions that pass for a given candidate across all replayed cases.
- `correction_resolution_rate`: the fraction of source corrections resolved by the candidate rule in replay.

**What the Learning Architect owns:**
- The `confidence` field on `RuleCandidate` (evidence aggregation from corrections, condition matching, contradiction detection).

**The control plane does NOT recompute `confidence`.** It reads the candidate's existing `confidence` and appends `replay_pass_rate` and `correction_resolution_rate` as separate fields. The reviewer sees all three and weighs them.

**Open question for Learning Architect:** Who triggers confidence recalculation when new matching corrections arrive after a replay has already been run? Is that a Learning-loop event, a control-plane webhook, or a polling read? This boundary must be defined in Round 2.

### 8.6 Replay run data model (control plane side)

```json
{
  "replay_run_id": "rpl_9f2a",
  "candidate_id": "rc_a91f",
  "tenant_id": "t_123",
  "triggered_by": "reviewer:alice@victoria.app",
  "triggered_at": "2026-04-26T15:00:00Z",
  "status": "running | completed | failed",
  "case_run_ids_under_test": ["cr_456", "cr_478", "cr_501"],
  "assertions": [
    {
      "case_run_id": "cr_456",
      "assertion_type": "correction_resolution",
      "passed": true,
      "detail": "decision_point dp_12 resolved to hold_and_request_more_info as requested"
    }
  ],
  "replay_pass_rate": 1.0,
  "correction_resolution_rate": 1.0,
  "completed_at": "2026-04-26T15:02:10Z"
}
```

### 8.7 Replay Scheduler Trigger API (RESOLVED-REPLAY-SCHED)

**Decision.** The Evaluation Service is the **scheduler** for replays; the execution plane is the **executor** (per exec-plane §4.3 and §3.4 `case_runs.replayed_from_id`). The Evaluation Service triggers replay workflows via an admin-authenticated REST API.

**Trigger endpoints.**

```
POST /admin/replays
  Auth:    JWT with role: admin OR role: reviewer (with appropriate tenant scope)
  Body:    {
             tenant_id: string,
             original_case_run_id: string,
             skill_version_id?: string,             // optional; if absent, uses current active SkillVersion for the tenant/workflow
             candidate_id?: string,                  // optional; if present, treats this as a candidate-validation replay
             replay_kind: "regression" | "candidate_validation" | "ad_hoc",
             idempotency_key: string                 // sha256(...) per §17
           }
  Returns: 202 { replay_run_id: "rpl_abc", status: "scheduled" }
  Effect:  Inserts row into control-plane `replay_runs` table; calls the
           execution plane's POST /internal/replay endpoint over mTLS (per §6.2; client SAN `evaluation.ctrl.victoria.internal`)
           to start the replay Temporal workflow.

GET /admin/replays/:replay_run_id
  Returns: { replay_run_id, status, replay_pass_rate, correction_resolution_rate,
             assertions: [...], started_at, completed_at }

POST /admin/replays/batch
  Body:    { tenant_id, candidate_id, original_case_run_ids: [...] }
  Returns: 202 { batch_id, replay_run_ids: [...] }
  Effect:  Schedules replays for all listed case runs against the candidate;
           used by the Rule Review Console for "preview impact of promoting this candidate."
```

**Idempotency.** `replay_runs.idempotency_key` is UNIQUE. A duplicate trigger with the same key returns 202 with the existing `replay_run_id` and current status — does not start a second replay.

**Pre-existing state required.** Before scheduling:
- `tenant_id` exists and has `status: active` in `tenants` table.
- `original_case_run_id` exists in the per-tenant DB (verified via a control plane → execution plane lookup over mTLS per §6.2).
- If `skill_version_id` is provided, it exists in `skill_versions` table.
- If `candidate_id` is provided, it is in `under_review` or `candidate` status in the tenant's `rule_candidates`.

A failed precondition returns 400 with `error_code` identifying which precondition failed; no replay is started.

**Authorization.**
- `admin` role: any tenant.
- `reviewer` role with `tenant_assignment` containing `tenant_id`: allowed.
- `reviewer` role without matching `tenant_assignment`: 403.
- `operator` role: 403 (operators do not trigger replays).

**TDD invariants for replay scheduling.**

- `REPLAY_SCHED_01`: A duplicate trigger with the same `idempotency_key` returns the existing replay's status; it does not create a second `replay_runs` row.
- `REPLAY_SCHED_02`: A trigger for a non-existent `original_case_run_id` returns 400 with `case_run_not_found` and emits no audit event.
- `REPLAY_SCHED_03`: A trigger for a tenant the requesting reviewer is not assigned to returns 403; emits a `security_violation` audit row.
- `REPLAY_SCHED_04`: A successful trigger emits a `replay_triggered` audit event with `replay_run_id` and `triggered_by`.

**Contract test against execution plane.** Trigger replay; assert exec-plane's `/internal/replay` is called with the correct payload (mTLS authenticated per §6.2; client SAN `evaluation.ctrl.victoria.internal`); assert exec-plane returns 202 with a Temporal `WorkflowID` matching the replay convention; assert the `replay_runs` row in control-plane DB transitions to `running`.

---

## 9. Observability

### 9.1 TDD invariants for observability

- `OBS_01`: Every HTTP handler must emit at least one span with `tenant_id`; handlers that call DB without a span are a test failure.
- `OBS_02`: Audit event insertion with a duplicate `idempotency_key` must not create a second row.
- `OBS_03`: No audit event may contain a raw phone number, email address, or message body in any non-encrypted field. A schema-level check or migration-time test enforces this.
- `OBS_04`: Audit events from tenant A must not appear in tenant B's audit query results.

### 9.2 Required OTel span attributes

Every span emitted by control-plane services must carry:

| Attribute | Type | Description |
|---|---|---|
| `tenant_id` | string | Derived from auth context; absent only on pre-auth spans |
| `case_run_id` | string (nullable) | Present when the request is associated with a specific CaseRun |
| `decision_point_id` | string (nullable) | Present when the request targets a specific decision |
| `replay_run_id` | string (nullable) | Present on spans within a replay operation |
| `service.name` | string | Go service name, e.g., `victoria.control.review` |
| `service.version` | string | Deployed image/commit SHA |
| `user.id` | string | Operator or reviewer identity ID (not phone number, not email — internal ID) |
| `user.role` | string | `operator`, `reviewer`, `admin`, `service` |

Spans missing `tenant_id` (except pre-auth spans) fail CI lint.

### 9.3 Audit log: single-store topology, schema, immutability, writer registry (RESOLVED-R4-CONFLICT-2, RESOLVED-C8, RESOLVED-AUDIT-CANON, RESOLVED-OPEN-AUDIT-CANON-CONFORMANCE)

**Topology (carried R3 + R4-aligned with learning-loop §2.0.1, §11.0).** Single authoritative store + per-tenant durable buffer:

| Store | Role | Authoritative? |
|---|---|---|
| **Control-plane `audit_events`** (single table, partitioned by `tenant_id`) | System of record; supports the audit query API for reviewers; long-term retention; **the synchronous read source for the MCP `WRITE_EXTERNAL` preflight** via `mcp_audit_reader` over mTLS | **Yes** |
| **Per-tenant `audit_events_outbox`** (in execution-plane DB; renamed from R3 "local `audit_events`") | Transactional durable buffer for outage tolerance only — written transactionally with the business INSERT; drains upstream to control-plane `audit_events`; **never a read source** | No (drain buffer) |

The mid-R4 reversal that made the per-tenant local table a read source is withdrawn (RESOLVED-R4-CONFLICT-2). Per learning-loop §2.0.1 the latency tradeoff is explicit: intra-VPC mTLS Postgres connection adds ~5–10ms vs. local read, acceptable on the WRITE_EXTERNAL hot path which already costs ~100s of ms. Surfaced as OPEN-AUDIT-LATENCY-R4 for post-MVP measurement.

#### 9.3.1 Invariants

- `INV-AUDIT-01`: Control-plane `audit_events` rejects `UPDATE`, `DELETE`, `TRUNCATE` at the storage layer (BEFORE-mutation trigger raises). Tested even with privileged migration role. The per-tenant `audit_events_outbox` reuses the same role + trigger pattern (only the `drained` flag is mutable; `BEFORE UPDATE` allows mutation only when `OLD.drained = false AND NEW.drained = true`).
- `INV-AUDIT-02`: Control-plane `audit_events` has `idempotency_key` `UNIQUE`; duplicate insert returns success without inserting a second row.
- `INV-AUDIT-03`: All `event_type` values are lower_snake_case per learning-loop §11.3. UPPER_SNAKE values from earlier rounds are explicitly forbidden; a CI lint scans the codebase for forbidden tokens.
- `INV-AUDIT-04`: An MCP server connection (as `mcp_audit_reader`, authenticated via mTLS with SAN `t_<tid>.mcp.victoria.internal`) sees only rows where `event_type = 'approval_received'` AND `tenant_id = current_setting('app.current_tenant')`. The role's grant is on a tightly scoped view (`mcp_approval_events` per learning-loop §11.5), not on the base table, and the grant is on the **control-plane** DB only.
- `INV-AUDIT-05`: Every exec-plane audit-event write inserts into `audit_events_outbox` (transactionally with the business write); the drain worker eventually inserts into control-plane `audit_events`. Drain lag exceeding 30 minutes for any tenant emits an alert (§9.6.1).
- `INV-AUDIT-06`: An audit row never carries raw phone numbers, email addresses, or message bodies in any non-encrypted field. PII is referenced indirectly via internal IDs only.
- `INV-AUDIT-07`: All control-plane services that emit audit events connect as the `audit_writer` Postgres role. The application layer knows its allowed `event_type` set per the writer registry below; the writer-registry contract test (§19) asserts authorization at the application layer (DB-level enforcement is delegated to the trigger that consults the `audit_event_writer_registry` table).

#### 9.3.2 Contract tests

- `test_audit_immutable_privileged`: Connect as a privileged migration role; attempt `UPDATE`/`DELETE`/`TRUNCATE` on control-plane `audit_events`. Assert the trigger raises; assert no row is mutated. **Critical:** testing as `audit_writer` passes vacuously since that role has no UPDATE grant; the privileged-role test is the actual gate.
- `test_audit_writer_no_select`: Connect as `audit_writer`; attempt `SELECT`; assert permission denied.
- `test_audit_idempotent_insert`: Insert the same `idempotency_key` twice; assert exactly one row exists.
- `test_audit_canon_conformance`: Lint the control-plane codebase for `event_type` literal strings; assert all match `^[a-z][a-z0-9_]*$` and appear in learning-loop §11.3 registry. Reject any UPPER_SNAKE leftover (e.g., `RULE_PROMOTED`, `CASE_RUN_STARTED`).
- `test_mcp_approval_view_scope`: Insert events `{tenant: t_A, event_type: approval_received}` and `{tenant: t_A, event_type: correction_received}` into control-plane `audit_events`; connect as `mcp_audit_reader` over mTLS with SAN `t_A.mcp.victoria.internal` (which sets `app.current_tenant = t_A`); SELECT from `mcp_approval_events` returns 1 row (`approval_received`); SELECT from `audit_events` base table returns permission denied. Repeat with `app.current_tenant = t_B` and assert the t_A approval row is not visible.
- `test_outbox_drain_reconciliation`: Stop the control-plane DB; emit 100 audit events from an exec-plane test harness (writes succeed to `audit_events_outbox`); restart control-plane DB; assert exactly 100 rows present in control-plane `audit_events` after drain; assert all outbox rows have `drained = true`.

#### 9.3.3 Mechanism

**Canonical schema** is owned by learning-loop §11.2 and adopted verbatim. Field list (excerpt):

| Field | Description |
|---|---|
| `id` (UUID v4) | Primary key |
| `tenant_id` | Partition key on the control-plane store; populated for all per-tenant events; nullable only for control-plane platform events |
| `event_type` | From the registry in `03-correction-loop.md` §11.3 — **always lower_snake_case** |
| `idempotency_key` | UNIQUE; supports the system-wide idempotency rule in §17 |
| `payload` (JSONB) | Event-specific structured data; never carries PII raw |
| `signal_id`, `gateway_idempotency_key` | When `event_type = 'approval_received'`, included in `payload` for cross-system reconciliation back to the gateway |
| `occurred_at`, `ingested_at` | Wall clock + ingest time |
| `source` | `execution_plane \| control_plane \| operator_ux` |

**Immutability mechanism (both stores).**

1. **Role split.**
   - `audit_writer`: INSERT only. Application services emitting audit events connect as `audit_writer`.
   - `audit_reader`: SELECT only. The audit query API and the Observability stack connect as `audit_reader`.
   - `mcp_audit_reader`: SELECT only on the `mcp_approval_events` view (per learning-loop §11.5). Granted on the control-plane store; MCP reads over mTLS (RESOLVED-R4-CONFLICT-2). The view restricts to `event_type = 'approval_received'` AND `tenant_id = current_setting('app.current_tenant')`.

2. **`BEFORE UPDATE OR DELETE OR TRUNCATE` trigger.** Raises an unconditional exception on any mutation attempt, including by privileged roles. Contract test runs against the privileged role (not `audit_writer`) to prove the trigger is the actual gate.

3. **WORM archive.** Partitions older than 13 months are exported to S3 with Object Lock (Compliance mode) before being detached. Retention is **7 years** (pending OPEN-OQ-05 legal sign-off).

**Writer registry** (lower_snake_case; aligned with learning-loop §11.6):

| Component | Allowed `event_type` values |
|---|---|
| Execution plane Temporal worker | `case_run_started`, `case_run_completed`, `case_abandoned`, `awaiting_timeout`, `replay_started`, `replay_completed`, `correction_received`, `correction_approve`, `approval_received` |
| Execution plane MCP servers | `sandbox_escape_blocked`, `blocked_write_attempted`, `security_violation` |
| Candidate-matcher service (learning) | `candidate_created`, `candidate_evidence_added`, `candidate_contradiction_detected`, `candidate_near_match_flagged`, `candidate_under_review`, `candidate_stale_marked`, `candidate_cap_exceeded`, `scope_escalation`, `correction_parse_failed` |
| Aggregation job (learning) | `vertical_aggregate_computed`, `aggregate_quarantined` |
| Promotion pipeline (control plane) | `rule_promoted`, `rule_deprecated`, `rule_rolled_back`, `skill_version_created`, `scope_escalation_reviewer_approved` |
| Rule Review Console (control plane) | `rule_rejected`, `rule_merged` |
| Provisioning workflow (control plane) | `tenant_provisioned`, `tenant_suspended`, `tenant_resumed`, `tenant_deprovisioned`, `gateway_temporal_cred_rotated`, `wa_session_*` |
| Operator UX gateway (via `POST /internal/audit/events`) | `packet_sent`, `packet_delivery_failed`, `packet_tombstoned`, `correction_received_at_gateway`, `correction_dead_lettered`, `correction_expired`, `stale_reply`, `wa_session_disconnected`, `wa_session_reconnected`, `wa_session_suspended`, `tenant_binding_mismatch`, `unbound_inbound_dropped`, `outbound_queue_overflow`, `signal_delivery_failed` |

**Audit-CANON conformance lint** scans this spec for any UPPER_SNAKE `event_type` literal and fails CI if any are found. R3 §9.3 had `CASE_RUN_STARTED | CORRECTION_RECEIVED | RULE_PROMOTED | RULE_ROLLED_BACK | TENANT_PROVISIONED | TENANT_SUSPENDED | APPROVAL_RECEIVED | APPROVAL_DENIED | REPLAY_TRIGGERED | REPLAY_COMPLETED | SECURITY_VIOLATION | SANDBOX_ESCAPE_BLOCKED` listed as an example shape; this round normalizes all to lower_snake_case throughout.

### 9.4 Audit ingest from execution plane: write-through + drain protocol (REVISED-R4-AUDIT-TOPOLOGY)

#### 9.4.1 Invariants

- `INV-INGEST-01`: Every exec-plane audit-event write goes first to the per-tenant `audit_events_outbox` (synchronous; immutability §9.3 applies). Concurrently, the row is enqueued for upstream drain to the control-plane `audit_events`. No exec-plane audit event reaches the control plane without first being durably persisted in the outbox.
- `INV-INGEST-02`: The drain endpoint is idempotent on `idempotency_key`. Duplicate POSTs produce one row in the control-plane store.
- `INV-INGEST-03`: A control-plane outage causes the local outbox to grow but does not block the producing exec-plane operation. Drain resumes on connectivity; the local row's `drained` flag is set true on successful ack.
- `INV-INGEST-04`: A POST to the ingest endpoint whose token `tid` does not match the event payload `tenant_id` is rejected (HTTP 403) and the rejection itself emits a `security_violation` audit row.
- `INV-INGEST-05`: Reconciliation gap (events present locally but not yet drained) exceeding 30 minutes raises an alert via §9.6.1.

#### 9.4.2 Contract tests

- `test_audit_outage_no_loss`: Stop the control-plane DB; emit 100 audit events from an exec-plane test harness (writes succeed locally); restart control-plane DB; wait for drain; assert exactly 100 rows in control-plane `audit_events`; assert all local rows have `drained = true`.
- `test_audit_idempotent_drain`: Drain the same outbox row twice; assert exactly one row in control-plane store.
- `test_audit_san_tid_mismatch_rejected`: Submit a POST with mTLS leaf SAN `t_A.exec.victoria.internal` and event body `tenant_id = T_B`; expect HTTP 403 and a `security_violation` audit row.
- `test_audit_drain_lag_alert`: Force outbox depth above the configured threshold for 30+ minutes; assert an alert with `code: "audit_drain_lag"` reaches the alert sink.
- `test_local_write_synchronous`: With the drain worker stopped, emit an audit event; assert the row is present in the per-tenant `audit_events_outbox` immediately and `drained = false` on its row.

#### 9.4.3 Mechanism

**Write-through pattern (every exec-plane audit write):**

1. INSERT into per-tenant `audit_events_outbox` (synchronous; storage-layer immutability applies).
2. Concurrently enqueue an upstream-drain task that POSTs the row to control-plane `/internal/audit/events`.
3. Drain task is idempotent on `(tenant_id, idempotency_key)`; failed deliveries retry with exponential backoff.
4. On successful ack, drain task sets the outbox row's `drained = true`.
5. The outbox row is retained for 24h post-drain per OUTBOX-SEMANTICS-R5 (learning-loop §11.0). The outbox is a durable buffer only — **not a read source**. MCP `WRITE_EXTERNAL` preflight reads the control-plane `audit_events` over mTLS (§9.3, RESOLVED-R4-CONFLICT-2).

**Drain endpoint:**

```
POST /internal/audit/events
  Auth:    mTLS (per §6.2); SAN-derived `tid` must equal event body `tenant_id`
  Body:    AuditEvent shape (learning-loop §11.2); MUST carry idempotency_key
  Returns: 200 OK on first acceptance; 200 OK with header "X-Idempotent: true" on duplicate
  Effect:  INSERT INTO audit_events ON CONFLICT (idempotency_key) DO NOTHING
```

**Failure modes.**

- **Control plane unavailable.** Local outbox grows; drain worker retries with exponential backoff. The drain worker emits an outbox-depth metric; alerting fires per §9.6.1 when depth exceeds the configured threshold.
- **Duplicate delivery.** `idempotency_key` UNIQUE constraint short-circuits; the second POST is a no-op (returns 200); the outbox marks the row drained.
- **Permanent rejection** (schema-version mismatch, malformed payload). The row is moved to a dead-letter table with `last_error`; alert fires.

**Control-plane-only events** (provisioning, promotion, reviewer actions, alerts) write **only** to the control-plane store; they have no local-buffer equivalent because no exec-plane component reads them synchronously.

### 9.5 Per-tenant retention and PII

- Default retention: audit events retained for 7 years (per learning-loop §11.5, pending legal sign-off — OPEN-OQ-05); trace spans retained for 90 days.
- PII in audit payloads: operator phone numbers are stored only in the `identities` table under encryption at rest. Audit events MUST NOT embed raw phone numbers, email addresses, or message bodies. They reference `actor_id` (internal identity ID only).
- The observability service holds per-tenant scrub rules that the OTel collector applies before forwarding to the trace backend. Configurable by admin, not hardcoded.
- On deprovisioning, audit events are archived (WORM) then dropped from live partitions per the retention window.

**Open question:** What is the minimum legal retention period for audit events given target jurisdictions (AU, UK, US initially)? Tracked as OPEN-OQ-05.

### 9.6 Audit query API (single authoritative store, RLS-enforced)

#### 9.6.1 Invariants

- `INV-QUERY-01`: A reviewer JWT with `tenant_assignment = [t_A]` returns 403 on `GET /audit/events?tenant_id=t_B`.
- `INV-QUERY-02`: A query for `tenant_id = t_A` returns rows where `audit_events.tenant_id = t_A` only (RLS-enforced); cross-tenant rows never appear regardless of the query shape.
- `INV-QUERY-03`: An operator JWT returns 403 on the audit query API; this endpoint is reviewer/admin only.
- `INV-QUERY-04`: The query reads from the **control-plane authoritative store**. The per-tenant `audit_events_outbox` is a drain buffer only — not a read source (RESOLVED-R4-CONFLICT-2, §9.4).

#### 9.6.2 Contract tests

- `test_query_tenant_assignment_enforced`: Reviewer JWT with `tenant_assignment = [t_A]`; `GET /audit/events?tenant_id=t_B`; expect 403.
- `test_query_rls_strict`: Insert events for both T_A and T_B; reviewer queries with `tenant_id=t_A`; assert only T_A rows returned regardless of query parameters.
- `test_query_operator_forbidden`: Operator JWT (any tenant); any audit query; expect 403.

#### 9.6.3 Endpoint

```
GET /audit/events?tenant_id=...&from=...&to=...&event_type=...&actor_id=...
  Auth:    Edge TLS + JWT (reviewer or admin)
  Returns: { events: [...], next_page_token?: string, total?: int }
  Effect:  SELECT against the control-plane audit_events table; RLS enforces tenant scope
```

Auth flow:
- A `reviewer` JWT with `tenant_assignment` scope queries only assigned tenants. A reviewer without `tenant_assignment` queries platform-wide.
- An `admin` JWT queries any tenant.
- The auth guard sets `app.current_tenant` and `app.bypass_tenant_check` (admin only) on the DB session before the SELECT; RLS on `audit_events` enforces row visibility.

The store is one authoritative table; there is no federation, no per-tenant proxy call, no `partial_results` flag.

### 9.6.1 Alert sink: emit-alert API (RESOLVED-ALERT-SINK, RESOLVED-OPEN-EMIT-ALERT-SCHEMA)

**Background.** Operator-ux's WhatsApp session lifecycle (§4.7), the gateway's whatsmeow session manager (§5.7), and exec-plane MCP servers all need to emit security and operational alerts to a centralized alerting system. The control plane owns the abstraction; vendor selection (PagerDuty, Slack, OpsGenie, etc.) is a deployment-time configuration, not a spec choice.

#### 9.6.1.1 Invariants

- `INV-ALERT-01`: An emit-alert call from a peer with SAN-derived `tid = T_A` with payload `tenant_id != T_A` returns HTTP 403 and emits a `security_violation` audit row. (Platform alerts with `tenant_id = null` require the caller's SAN to be a control-plane service or a gateway.)
- `INV-ALERT-02`: A duplicate `idempotency_key` increments the existing alert's `repeat_count` and does not trigger duplicate external routing within the suppression window.
- `INV-ALERT-03`: A `critical` alert is routed to all configured `critical`-severity destinations within 60 seconds of emission.
- `INV-ALERT-04`: An emit-alert call without a valid mTLS client cert (per §6.2) fails at TLS handshake before any handler runs; no DB write occurs.
- `INV-ALERT-05`: The `severity` field must be one of `info | warning | error | critical`. Other values are rejected with HTTP 400.
- `INV-ALERT-06`: Required fields (`tenant_id` nullable for platform alerts; `severity`, `category`, `source`, `code`, `summary`, `occurred_at`, `idempotency_key`) MUST all be present. Missing required fields produce HTTP 400 with the missing-field name in the error body.

#### 9.6.1.2 Contract tests

- `test_alert_token_tid_mismatch`: Submit an alert with `payload.tenant_id = T_B` while the bearer token has `tid = T_A`; expect 403 + `security_violation` audit row.
- `test_alert_idempotent`: Submit the same `idempotency_key` 3 times; assert exactly one row in `alerts`; assert `repeat_count = 3`; assert PagerDuty webhook called only once (within suppression window).
- `test_alert_critical_routing_latency`: Submit a `critical` alert; assert the stubbed PagerDuty endpoint received the dispatch within 60 seconds.
- `test_alert_severity_enum`: Submit an alert with `severity: "fatal"`; expect 400 (`severity_invalid`).
- `test_alert_required_fields`: Submit a payload missing `code`; expect 400 (`field_required: code`).
- `test_alert_no_token_rejected`: Submit without `Authorization` header; expect 401, no DB write.

#### 9.6.1.3 Endpoint and schema

```
POST /internal/alerts
  Auth:    mTLS (per §6.2); SAN-derived `tid` must match payload `tenant_id` for tenant-scoped alerts;
           platform-scoped alerts (tenant_id=null) require the caller's SAN to belong to a control-plane
           service (`*.ctrl.victoria.internal`) or the gateway (`gateway.victoria.internal`).
  Body:    AlertPayload (JSON Schema below)
  Returns: 200 { alert_id: "alrt_abc", routed_to: ["pagerduty","slack-ops"] }
  Effect:  Persists alert to control-plane `alerts` table; dispatches to configured routes.
```

**AlertPayload JSON Schema (RESOLVED-OPEN-EMIT-ALERT-SCHEMA).**

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "AlertPayload",
  "type": "object",
  "required": ["severity", "category", "source", "code", "summary", "occurred_at", "idempotency_key"],
  "properties": {
    "tenant_id":       { "type": ["string", "null"], "pattern": "^t_[A-Za-z0-9_-]+$" },
    "severity":        { "type": "string", "enum": ["info", "warning", "error", "critical"] },
    "category":        { "type": "string", "enum": ["security", "session", "provisioning", "rule", "rate_limit", "audit", "ingest", "credential_rotation", "bridge", "other"] },
    "source":          { "type": "string", "enum": ["gateway", "exec_plane", "control_plane"] },
    "code":            { "type": "string", "pattern": "^[a-z][a-z0-9_]*$", "maxLength": 64 },
    "summary":         { "type": "string", "maxLength": 256 },
    "detail":          { "type": "object" },
    "trace_id":        { "type": "string" },
    "span_id":         { "type": "string" },
    "occurred_at":     { "type": "string", "format": "date-time" },
    "idempotency_key": { "type": "string", "maxLength": 256 }
  },
  "additionalProperties": false
}
```

#### 9.6.1.4 Routing

| Severity | Default routes (vendor-agnostic abstraction) |
|---|---|
| `info` | `audit_only` (logged + persisted; no external notification) |
| `warning` | `chat_ops` (e.g., Slack ops channel) |
| `error` | `chat_ops` + `oncall_email` |
| `critical` | `pager` + `chat_ops` |

Per-tenant overrides via `alert_policy(tenant_id, category, severity, action)` can suppress `info`/`warning` for a specific category; `error` and `critical` always route. The vendor mapping (which webhook URL is `pager`, which is `chat_ops`) is configuration, not spec.

**Dedup and suppression.** `alerts.idempotency_key` UNIQUE. Repeated calls with the same key update `repeat_count` and `last_seen_at` on the existing row. The routing layer enforces a per-(tenant, code) suppression window (default 30 minutes for `critical`, 60 minutes for `error`, 4 hours for `warning`). Re-fire happens after the window only if the alert is still unresolved (resolution detection is alerter-specific and out of this spec).

---

## 10. Billing and Admin

### 10.1 TDD invariants for billing/admin

- `BILLING_01`: The monthly `case_run_started` count must not double-count due to duplicate ingest; count is derived from the idempotent audit log.
- `BILLING_02`: `POST /admin/tenants/:id/deprovision` must require explicit `confirm: true` body field; accidental calls without confirmation return 400.
- `BILLING_03`: An admin MFA session token must not be reusable after logout; logout invalidates the session in Redis.

### 10.2 Minimum viable shape

Phase 1 billing is internal tracking only — no payment processor integration yet. The goal is to capture the events that will drive future billing, not to build an invoicing system.

**Usage events collected:**

| Event | Trigger | Fields |
|---|---|---|
| `case_run_started` | Each new CaseRun | `tenant_id`, `workflow_type`, `sandbox_mode` |
| `correction_received` | Each operator correction | `tenant_id`, `correction_type` |
| `rule_promoted` | Each ValidatedRule creation | `tenant_id`, `scope` |
| `replay_triggered` | Each evaluation replay | `tenant_id`, `case_count` |
| `operator_message_sent` | Each outbound WhatsApp message | `tenant_id`, `channel` |

**Plan table:** `plans(tenant_id, plan_id, case_run_limit_monthly, active_from, active_until)`. Simple; no complex metering yet.

**Plan enforcement:** The gateway reads the current month's `case_run_started` count from the aggregate table. If the count exceeds `case_run_limit_monthly`, new CaseRun creation returns 429. On control-plane downtime, the last-known plan limit is read from a short-lived Redis cache; if that is also unavailable, the system fails **open** (allows runs) rather than blocking the operator's business. This is a deliberate decision: a small-business operator blocked by a billing outage is a worse outcome than a brief over-count.

### 10.3 Admin actions

Admin-only endpoints (require `role: admin`, MFA-verified session):

```
GET  /admin/tenants                  List all tenants with status
POST /admin/tenants/:id/suspend      Suspend tenant
POST /admin/tenants/:id/resume       Resume tenant
POST /admin/tenants/:id/deprovision  Begin deprovisioning workflow
GET  /admin/tenants/:id/usage        Usage summary for tenant
PUT  /admin/tenants/:id/plan         Update plan assignment
GET  /admin/reviewers                List reviewer accounts
POST /admin/reviewers                Create reviewer account
PUT  /admin/reviewers/:id/tenants    Update reviewer tenant assignment
```

---

## 11. Failure Modes

### 11.1 Control plane goes down

**Does the execution plane keep running? Yes, and it must.**

The execution plane is operationally autonomous for currently-running workflows. Hermes, Temporal workers, MCP sidecars, and the operator-facing messaging gateway do not depend on control-plane availability for their hot path. An operator can receive and respond to a review packet with no control-plane reachability.

What stops when the control plane is down (R4 reality after the single-store audit reconciliation):

| Function | Impact |
|---|---|
| New tenant provisioning | Blocked until control plane recovers |
| Rule promotion (any scope) | Blocked; reviewers cannot write `validated_rules` (single store in control plane DB) |
| `LoadSkillVersion` calls from execution plane | Fail; in-flight workflows already have their pinned `skill_version_id` and continue; new workflow starts must wait |
| Audit event ingest from per-tenant `audit_events_outbox` | Drain pauses; per-tenant outbox writes continue (transactional with the business INSERT, §9.4); on recovery the drain catches up. **No event loss.** |
| Audit query API | Unavailable; investigators wait for control plane recovery |
| MCP `WRITE_EXTERNAL` approval check | **Fails closed.** The MCP server's preflight reads control-plane `audit_events` over mTLS (§9.3 / RESOLVED-R4-CONFLICT-2). On control-plane outage the connection fails; the preflight cannot confirm an `approval_received` row; per the default-deny semantics of `WRITE_EXTERNAL` the call is denied. This is the constraint learning-loop §2.0.1 explicitly accepted: "Latency tradeoff explicit. Intra-VPC mTLS Postgres connection adds ~5–10ms vs. local read. Acceptable on the WRITE_EXTERNAL hot path." Read-only MCP tools are unaffected because they have no preflight. |
| Replay triggering | Blocked |
| Billing enforcement | Uses cached plan limits; fails open if cache is also unavailable |
| Operator first-time login / OTP-verify (§6.7) | Blocked; the Auth Service is in the control plane. Operators with active JWTs continue to work until expiry. |
| Operator JWT refresh (§6.7) | Blocked; refresh tokens require Auth Service. Active JWTs continue to expiry; if expiry occurs during outage, operator is logged out and must re-OTP after recovery. |
| Approval-signal delivery (gateway → Temporal) | **Unaffected.** Gateway → Temporal is a direct path that does not transit the control plane (Consensus A3, §6.6). |
| Alert emission | Buffered in caller's local queue; drains on recovery. Critical alerts that require external paging during the outage are degraded — gateway and exec-plane fall back to a local circuit-breaker log, but external paging is best-effort. |

**Hard contract requirement on execution plane.** Maintain the per-tenant `audit_events_outbox` durable transactional buffer; never block the business INSERT on control-plane reachability. Audit events written to `audit_events_outbox` are durable locally even when the control plane is unreachable; the drain worker catches up on recovery (§9.4).

**Hard contract requirement on gateway.** The gateway's outbound packet queue (operator-ux §4.7.4) is not affected by control-plane availability — the gateway only contacts the control plane for OTP/JWT issuance, alert emission, and audit emission, none of which is on the operator-approval hot path.

### 11.2 Partial provisioning failures

Covered in §5.2. The `TenantProvisioningWorkflow` is idempotent and Temporal-durable. Every activity is retryable. If provisioning fails after partial resource creation, the tenant record is set to `provisioning_failed` with the last-completed step recorded. A compensating deprovision workflow can be run by admin to clean up partial resources. No partial state is silently ignored.

### 11.3 Secrets-store unavailability

**If the secrets store is unavailable at execution plane startup:** the execution plane cannot inject credentials and must fail to start. This is a hard fail; starting with no credentials would cause silent failures.

**If the secrets store becomes unavailable mid-operation:** the execution plane should not re-request secrets mid-run if the credentials are already loaded into process memory for that run. Credentials must not be re-fetched per-request; they should be loaded at container start and refreshed on a schedule. A secrets-store outage therefore affects new container starts and scheduled credential rotation, not in-flight runs.

**If the control plane cannot reach the secrets store:** provisioning is blocked (cannot create new secret scopes); existing operations using in-memory cached credentials continue.

**No fallback to plaintext or shared credentials under any failure mode.** The system should surface a clear operational error rather than silently degrade to insecure credential handling.

### 11.4 Temporal server unavailability

Provisioning and replay workflows queue and resume when Temporal recovers. Execution-plane Temporal workers have their own retry behavior (Execution Plane Architect's domain). Control plane does not lose state as long as Temporal's own persistence (typically Postgres or Cassandra) survives.

---

## 12. Test Strategy (TDD)

### 12.1 Invariant catalog

All invariants cited in sections above are collected here for traceability. R4 normalized invariant IDs to a `INV-<DOMAIN>-NN` form for the new sections; older `XXX_NN` IDs are retained for backward reference.

| ID range | Section | Invariant family |
|---|---|---|
| `TENANT_CTX_01–04` | §3.1 | Tenant-context propagation |
| `AUTH_01–05` | §6.5 | Auth model |
| `INV-AUTH-S2S-01..07` | §6.2.1 | mTLS service-to-service auth (RESOLVED-R4-CONFLICT-1) — mirrors exec-plane §13.1 INV-RPC1..4 |
| `SIGNAL_01–03` | §6.6 | Approval signal transport |
| `REVIEW_01–05` | §7.2 | Rule Review Console |
| `EVAL_01–05` | §8.1 | Evaluation Service |
| `MANIFEST_01–03` | §5.6 | Provisioning manifest |
| `INV-WA-01..05` | §5.7.1 | WhatsApp session lifecycle (R4 expanded) |
| `INV-OTP-01..05` | §6.7 | OTP/JWT issuance (R4 expanded) |
| `INV-GW-CRED-01..06` | §6.8.1 | Gateway Temporal credential (R4 expanded) |
| `INV-AUDIT-01..07` | §9.3.1 | Audit immutability + writer registry (R4) |
| `INV-INGEST-01..05` | §9.4.1 | Audit ingest write-through + drain (R4) |
| `INV-QUERY-01..04` | §9.6.1 | Audit query API (R4) |
| `INV-ALERT-01..06` | §9.6.1.1 | Alert sink (R4) |
| `RULE_RLS_01–04` | §13.2 | Rule storage RLS |
| `REPLAY_SCHED_01–04` | §8.7 | Replay scheduler |
| `IDEMP_01–04` | §17.4 | Idempotency key |
| `OBS_01–04` | §9.1 | Observability |
| `BILLING_01–03` | §10.1 | Billing/admin |

### 12.2 Contract tests: control plane ↔ execution plane

| Contract | Test |
|---|---|
| Rule promote (any scope, single-store) | Promote a rule; assert single transaction in control-plane DB writes `validated_rules` + audit + `skill_versions`; assert `vr_scope_consistency` CHECK passes |
| Effective rule set (LoadSkillVersion) | Exec-plane calls `GET /internal/skills/load` over mTLS; verifier extracts `tid` from SAN `t_<tid>.exec.victoria.internal`; assert SAN `tid` matches the request's `tenant_id`; assert returned manifest is consistent with the single-table resolver query (mirrors exec-plane `ct_load_skill_version`) |
| Replay scheduler trigger | Control plane calls `POST /internal/replay` over mTLS (client SAN `evaluation.ctrl.victoria.internal`); assert exec-plane returns 202 with Temporal WorkflowID (mirrors exec-plane `ct_replay_trigger_admin_to_internal`) |
| Audit ingest drain | Stop control-plane DB; emit 100 events from exec-plane (writes succeed to `audit_events_outbox`); restart; assert exactly 100 rows in control-plane `audit_events` after drain; assert all `audit_events_outbox` rows have `drained = true`; mirrors learning-loop CT-LL-11 |
| MCP approval check (direct DB SELECT over mTLS to control plane) | MCP server (cert SAN `t_<tid>.mcp.victoria.internal`) connects to control-plane DB as `mcp_audit_reader`; queries `mcp_approval_events` view; assert results scoped to `event_type = 'approval_received'` AND own tenant only; assert no SELECT on the base `audit_events` table; mirrors learning-loop CT-LL-9 |
| mTLS SAN ↔ payload tenant binding | Present cert with SAN `t_A.exec.victoria.internal`; submit audit event with payload `tenant_id = T_B`; assert HTTP 403 + `security_violation` audit row written |
| mTLS handshake required | Open plaintext TCP or TLS without client cert to any `/internal/*` listener; assert TLS handshake fails (or rejected at listener) before any handler runs |
| Manifest delivery | Provision tenant; assert manifest at agreed mount path; HealthCheck blocks until container is ready |
| WhatsApp session lifecycle | Provision tenant; assert gateway `channel_bindings` row created and whatsmeow session initialized; deprovision tenant; assert session disconnected before binding deleted |
| Mark candidate promoted | After a `validated_rules` write commits in control plane, exec-plane endpoint `/internal/candidates/mark-promoted` is called over mTLS; assert per-tenant `rule_candidates.status = 'promoted'` and `promoted_to_rule_id` is set; idempotent on (tenant_id, candidate_id) |

### 12.3 Contract tests: control plane ↔ messaging gateway (Operator UX)

Per Consensus A3, the gateway → Temporal direct path means the control plane is NOT on the approval hot path. Remaining contracts:

| Contract | Test |
|---|---|
| OTP / JWT issuance | Gateway calls `POST /auth/operator/otp/initiate` over mTLS (SAN `gateway.victoria.internal`); assert OTP delivered via WhatsApp out-of-band; gateway calls `POST /auth/operator/otp/verify`; assert JWT returned with `tid` set server-side from the verified identity |
| OTP one-time-use | Submit same `otp_request_id` twice; assert second attempt returns 409 |
| Refresh-token rotation | Use refresh token twice; assert second use invalidates the chain and emits `security_violation` audit |
| Channel binding registration | Provisioning workflow calls gateway's binding API; assert gateway returns 200 with bridge metadata |
| Cross-system audit reconciliation | Gateway emits `signal_id` and `gateway_idempotency_key` per approval (operator-ux 16-field envelope §3.1, C13-R5); control plane receives matching `approval_received` event in `audit_events` via drain within SLA (default 5 min) |
| Gateway Temporal cred scope | Load gateway credential from `gateway/tenants/<tid>/temporal_signal_client`; attempt `StartWorkflow` and `TerminateWorkflow`; assert authorization error; attempt `SignalWorkflow` on own-tenant queue; assert success |
| Gateway IAM scope | Authenticate as gateway service identity; attempt to read `gateway/tenants/t_X/temporal_signal_client` (expect 200), `tenant/t_X/internal_*` (expect 403), `tenant/t_X/llm_provider_key` (expect 403) |
| Alert emission | Gateway emits `wa_session_disconnected` over mTLS; assert alert in `alerts` table; assert routing to configured destinations |
| Audit-canon gateway additions | Gateway emits each of its 7 gateway-source `event_type` values (operator-ux §9.7); assert each is accepted by the writer-registry trigger after learning-loop §11.3 registers them (RESOLVED-CLOSED-R5) |
| Messaging gateway does not call any endpoint requiring `role: reviewer` | Role boundary test |

### 12.4 Tenant isolation integration tests

These tests run against a real Postgres instance (or ephemeral Docker Compose) with two tenants seeded:

1. **Data isolation:** Query all resource endpoints for tenant A using tenant B's JWT or mTLS leaf; assert all return 403.
2. **RLS bypass attempt:** Inject `tenant_id` directly in request body or query string; assert control plane ignores it and uses JWT `tid` (or SAN-derived `tid`) only.
3. **Audit log isolation:** Write audit events for both tenants; query each tenant's audit log via the reviewer API; assert no cross-contamination at the result level.
4. **Rule isolation:** Promote a rule for tenant A; from a tenant B exec-plane mTLS leaf (SAN `t_B.exec.victoria.internal`), call `LoadSkillVersion`; assert no T_A tenant-scope rules appear in the returned manifest (vertical/default rules are visible — that is correct per RLS policy).

### 12.5 Property / fuzz tests

| Target | Property | Generator |
|---|---|---|
| Audit idempotency key generation | For any `(tenant_id, case_run_id, event_type, seq)` tuple, the key is deterministic and collision-free for distinct tuples | N=10⁵ random tuples per registered key per §12.6.3 |
| ValidatedRule version chain | For any sequence of promote/rollback operations, the version chain is a valid DAG with exactly one `active` head per `(tenant_id, workflow_template_id, decision_type, conditions_hash)` | N=200 random promote/rollback sequences over a seed of M=10 base rules |
| Tenant-context extraction | For any valid JWT or mTLS leaf, `TenantContextInterceptor` extracts exactly the `tid` (claim or SAN-derived) and never any query parameter or body field | N=10⁴ random valid+invalid claim/SAN inputs |

### 12.6 Property / fuzz tests (R4 — required by moderator)

Three property/fuzz tests added this round.

#### 12.6.1 RLS isolation property

```
PROPERTY RLS_ISOLATION:
  For any sequence of `SET LOCAL app.current_tenant = 't_X'`
  followed by ANY SELECT shape on `validated_rules` or `audit_events`,
  the returned row set MUST NOT contain any row with `tenant_id != 't_X'`,
  EXCEPT for `validated_rules` rows where `scope IN ('vertical', 'default')`
  (which by §13.1 have `tenant_id IS NULL` and are intentionally shared).

Fuzz harness:
  - Seed N tenants with M rows each, varying scope.
  - Generate random SELECT statements: WHERE clauses, JOINs, subqueries,
    UNION ALL, ORDER BY, LIMIT, GROUP BY.
  - For each statement, run as a session with `app.current_tenant = t_K`
    for random t_K.
  - Assert: no result row has `tenant_id` ∉ {t_K, NULL_for_shared_scope_only}.
  - Run with `app.bypass_tenant_check = false` (the default for non-admin).
```

This is the strongest property test in the spec — it catches RLS-policy regressions that targeted unit tests would miss because they cover only specific query shapes.

#### 12.6.2 JWT verification fuzz

```
PROPERTY JWT_FUZZ:
  For any input claim object, the JWT verifier MUST reject inputs that:
    - Have `tid` matching no provisioned tenant
    - Have `exp` in the past (any value strictly less than now())
    - Have `jti` already in the Redis dedup set within `exp` window
    - Have a tampered signature (any byte modified after signing)
    - Have `nonce` not corresponding to the `otp_request_id` it claims
    - Have `role` ∉ {operator, reviewer, admin}
  In all of the above cases the verifier MUST return 401 (or 403 for role-based)
  WITHOUT consulting any cached or precomputed result. There is never a stale-cache hit.

Generator (input space):
  - tid: drawn from {valid provisioned tenant_id, random t_<6char> not in DB} ratio 1:3
  - exp: drawn from {now()+(5..3600s), now()-(1..3600s)} ratio 1:1
  - jti: drawn from {fresh UUIDv4, jti already accepted in this run} ratio 3:1
  - signature: drawn from {valid HS256 over claims with the JWT signer key, valid HS256 with one byte flipped, valid HS256 signed with a different key} ratio 1:1:1
  - nonce: drawn from {nonce bound to the cited otp_request_id, random 16-byte string} ratio 1:1
  - role: drawn from {operator, reviewer, admin, "owner", "guest", null, ""} ratio 3:3:3:1:1:1:1
  - N = 10⁴ trials; balanced corruption + well-formed mix at 50/50

Harness:
  - Submit each generated token to the auth filter.
  - Assert: every well-formed token (all six axes valid) returns 200/202; every token with at least one corruption returns 401 (or 403 for role mismatches).
  - Assert: no stale-cache hit — bracket each request with a Redis FLUSHDB; assert the test still rejects.
  - Replay sub-property: submit a valid token; submit again with same `jti`; assert second is rejected as `replay_detected` and emits `security_violation`.
```

#### 12.6.3 Idempotency-key derivation property

```
PROPERTY IDEMPOTENCY_KEY_DERIVATION:
  Let key(c) = sha256(c[0] ‖ 0x1F ‖ c[1] ‖ 0x1F ‖ ... ‖ c[N-1])  per §17.1.
  For any randomly drawn component tuple c:
    - Determinism:    key(c) == key(c)  always.
    - Distinctness:   for any c1 ≠ c2 (componentwise), key(c1) ≠ key(c2)
                      with probability matching SHA-256 collision expectation
                      (i.e., effectively zero for any feasible test sample).
    - Order sensitivity: reordering components produces a different hash
                      (catches accidental field-order changes during refactoring).

Fuzz harness:
  - Generate 10⁵ random tuples per registered key (signal_id, packet_id, etc.).
  - Compute key(c) twice; assert equal.
  - Compute key(c1), key(c2) for distinct c1, c2; assert different.
  - Permute the component order; recompute; assert different.
```

---

## 13. ValidatedRule Storage — Unified, RLS-Enforced (RESOLVED-C12, REVISED-C1)

**Background.** Round 2 split rule storage by trust boundary: tenant/case-scope rules in per-tenant execution DBs; vertical/default-scope rules in control-plane `validated_rules_shared`. Round 3 reverses that. Consensus A1 in `00-overview.md` §2 places **all four scopes in one control-plane `validated_rules` table, RLS-enforced**. Learning-loop §2.8 specifies the canonical DDL with the `vr_scope_consistency` CHECK constraint and the RLS policy. The control plane adopts that DDL verbatim.

**Why the unified store wins under R3:**
1. The two-source merge in the execution plane's `FetchValidatedRules` activity is fragile under partial outages. A single SQL with scope ordering is simpler and more correct.
2. The control plane already enforces RLS as defense-in-depth (§3.5). Tenant-scoped rules being co-located with vertical/default rules in a single RLS-enforced table is operationally identical to having them in separate tables — the row-level policy is the boundary.
3. Cross-tenant vertical promotion is best done at the same DB where the source aggregate is materialized (learning-loop's `vertical_candidate_aggregates` view in their §2.2). Splitting storage across DBs forced a cross-DB writer; unified storage makes promotion a single transaction.

**What stays per-tenant:** `corrections` and `rule_candidates`. Operator free-text PII never crosses to the control plane. Cross-tenant promotion is supported via learning-loop's `vertical_candidate_aggregates` view, materialized on demand at promotion-request time with PII strip rules (learning-loop §13.5).

### 13.1 Adopted DDL (canonical: learning-loop §2.8)

The control plane adopts learning-loop §2.8's `validated_rules` DDL **verbatim**. The full DDL is in `03-correction-loop.md` §2.8; key points repeated here for accessibility:

- Single table `validated_rules` in the control-plane DB.
- Columns: `tenant_id` (nullable), `case_run_id` (nullable), `vertical` (nullable), `workflow_template_id`, `workflow_slug`, `decision_type`, `conditions_canonical`, `conditions_hash`, `recommended_action`, `scope`, `version`, `supersedes`, `rollback_of`, `promoted_from_candidate_id`, `promoted_by`, `promoted_at`, `rationale`, `status`, `deprecated_at`, `deprecated_by`.
- `CHECK (vr_scope_consistency)` enforces:
  - `scope = 'case'` ⇒ `tenant_id NOT NULL`, `case_run_id NOT NULL`, `vertical IS NULL`
  - `scope = 'tenant'` ⇒ `tenant_id NOT NULL`, `case_run_id IS NULL`, `vertical IS NULL`
  - `scope = 'vertical'` ⇒ `tenant_id IS NULL`, `case_run_id IS NULL`, `vertical NOT NULL`
  - `scope = 'default'` ⇒ `tenant_id IS NULL`, `case_run_id IS NULL`, `vertical IS NULL`

This CHECK is the primary defense against accidental tenant-identity leakage on shared-scope rows: it is structurally impossible to write a vertical-scope rule with a `tenant_id`.

### 13.2 RLS policy (adopted from learning-loop §2.8)

```sql
ALTER TABLE validated_rules ENABLE ROW LEVEL SECURITY;

CREATE POLICY vr_tenant_isolation ON validated_rules
  USING (
    scope IN ('vertical', 'default')                              -- shared rules visible to all
    OR tenant_id = current_setting('app.current_tenant', true)    -- own tenant only
    OR current_setting('app.bypass_tenant_check', true) = 'true'  -- reviewer/admin role
  );
```

**Read access matrix:**

| Reader | What they see |
|---|---|
| Execution plane (service identity for tenant T) | `vertical` + `default` rows AND rows where `tenant_id = T` |
| Operator (JWT `tid = T`) | Same as above (operators do not currently read `validated_rules` directly, but if they ever do via the API, this RLS applies) |
| Reviewer with `tenant_assignment = [T_a, T_b]` | `vertical` + `default` rows AND rows where `tenant_id IN (T_a, T_b)`. Application sets `app.current_tenant` per query; `app.bypass_tenant_check` set true only when the reviewer is querying the union. |
| Admin / reviewer without assignment | All rows (`bypass_tenant_check` set true) |

**Write access:**
- Promotion writes go through the Rule Review Console's `promote` API. The Console connects with elevated DB privilege (table owner or a dedicated `rule_writer` role); the application enforces tenant scoping based on reviewer's `tenant_assignment` and the candidate's tenant_id.
- The execution plane never writes to `validated_rules`. The execution plane reads via the resolver call (§13.3).

**TDD invariants for rule isolation (Consensus A1).**

- `RULE_RLS_01`: Set `app.current_tenant = 't_A'`. `SELECT * FROM validated_rules WHERE tenant_id = 't_B'` MUST return zero rows. Even if the application layer crafts a malicious WHERE clause, RLS prevents the leak.
- `RULE_RLS_02`: Set `app.current_tenant = 't_A'`. `SELECT * FROM validated_rules WHERE scope = 'vertical' AND vertical = 'roofing'` returns vertical roofing rules regardless of `tenant_id` (which is NULL for vertical-scope rows). This is the intended cross-tenant visibility for shared rules.
- `RULE_RLS_03`: An attempt to INSERT a `scope = 'vertical'` row with `tenant_id = 't_A'` (non-NULL) MUST fail at the storage layer (CHECK constraint), not at the application layer.
- `RULE_RLS_04`: Without setting `app.current_tenant` and without `app.bypass_tenant_check = true`, a SELECT returns only `vertical` and `default` rows. (No tenant context = no tenant rows.)

**Contract test.** Provision tenants T_A and T_B; insert rules at all four scopes for each (where applicable); from a connection with `app.current_tenant = 't_A'`, run `SELECT * FROM validated_rules`; assert the result set contains T_A tenant/case rows + all vertical/default rows, and NO T_B tenant/case rows. Repeat with `t_B`. Then attempt to violate `vr_scope_consistency` and assert CHECK rejection.

### 13.3 Effective-rule-set resolver: pull-at-start (Consensus A2, A10)

The execution plane's `LoadSkillVersion` activity (exec-plane §8.2) is the **authoritative** read path. It calls a control-plane endpoint that resolves the effective rule set (using the single SQL in learning-loop §2.1) and wraps it in a `SkillVersion` manifest.

**Endpoint.**

```
GET /internal/skills/load
  Auth:    mTLS (per §6.2); SAN-derived `tid` must equal the requested `tenant_id`
  Query:   ?tenant_id=...&workflow_slug=...&as_of=...   (as_of optional)
  Returns: SkillVersion manifest (per learning-loop §9.1) with rule_manifest[]
  Effect:  Server-side runs the resolver query (learning-loop §2.1) with
           app.current_tenant set to the SAN-derived `tid`; wraps
           result in a SkillVersion manifest (or returns the existing one
           if as_of resolves to an existing version).
```

Auth: mTLS per §6.2; SAN-derived `tid` must equal the requested `tenant_id`. Mismatch returns 403 and emits `security_violation`.

**Pull-at-start, pinned-for-run.** The execution plane writes the returned `skill_version_id` to `case_runs.skill_version_id` at workflow start; the workflow uses this pin throughout the run. Mid-run rule changes do not affect in-flight workflows (per exec-plane §8.4 and Consensus A4 replay determinism). New runs pick up new rules at the next `LoadSkillVersion` call.

**Cache-invalidation hint (RESOLVED-C11, downgraded from R1 push).** When the control plane writes a new rule (promotion or rollback), it MAY (best-effort) emit a non-authoritative cache-bust hint to the execution plane via:

```
POST /internal/rules/cache-hint
  Body: { tenant_id, workflow_slug, new_skill_version_id }
```

This is purely advisory — the execution plane is not required to act on it. The authoritative consumption is the next `LoadSkillVersion` call. The hint exists only to reduce the propagation lag between promotion and effect for tenants with active workflows that haven't yet started a new case. If the hint fails to deliver (control plane outage, network error), no business impact occurs — the next workflow start picks up the new rule via pull.

**Tiebreaker for same-scope conflicts.** Within the same scope, more recent `promoted_at` wins. This is implemented in the resolver SQL (`ORDER BY scope_priority(scope) DESC, promoted_at DESC`).

### 13.4 Cross-tenant vertical promotion: PII strip via aggregate view (Consensus A1, INV-05)

**Mechanism owned by learning-loop.** Cross-tenant vertical promotion uses the `vertical_candidate_aggregates` view defined in `03-correction-loop.md` §2.2 and §13.5. The view is materialized at promotion-request time only — there is no continuous replication. R2's `rule_candidates_control` continuous-replication design is removed.

**Control plane's role.**

1. The Rule Review Console requests an aggregate when a reviewer initiates a vertical-scope promotion. The request goes to learning-loop's aggregation worker.
2. The worker reads each contributing tenant's `rule_candidates` over per-tenant DB connections, applies strip rules in memory, writes only the stripped projection to `vertical_candidate_aggregates`, and returns the aggregate ID.
3. The Rule Review Console reads the aggregate and writes a `validated_rules` row with `scope = 'vertical'`, `tenant_id = NULL`, and `promoted_from_candidate_id = NULL` (the aggregate is the source, not a single candidate).
4. The `vr_scope_consistency` CHECK constraint enforces the strip at the storage layer; if the application accidentally tried to write `tenant_id = 't_A'` on a vertical row, the DB rejects it.

**Open dependency on learning-loop (OPEN-N3).** The exact strip rules — which fields pass, which are quarantined, which are auto-redacted — are owned by learning-loop. This spec depends on those being concrete by the time the Rule Review Console implements vertical promotion.

**TDD invariant (cross-spec).** Run learning-loop's `TEST INV-05` (`03-correction-loop.md` §13.5) against the control-plane DB:
- Promote tenant_A and tenant_B candidates that share `conditions_hash`.
- Reviewer requests vertical promotion via aggregate.
- Assert the resulting `validated_rules` row has `scope = 'vertical'`, `tenant_id IS NULL`, `promoted_from_candidate_id IS NULL`, and the JSON serialization contains no string matching `t_A`, `t_B`, or any `cr_*` ID from those tenants.

### 13.5 Promotion write path (single store, RLS-enforced)

| Promotion source | Writer | Target |
|---|---|---|
| Single-tenant candidate (`scope = 'tenant'` or `'case'`) | Rule Review Console (control plane) | `validated_rules` table; `tenant_id` set from candidate; `case_run_id` set if `scope = 'case'` |
| Cross-tenant aggregate (`scope = 'vertical'`) | Rule Review Console (control plane) | `validated_rules` table; `tenant_id IS NULL`; `vertical` set; `promoted_from_candidate_id IS NULL` |
| Platform-wide rule (`scope = 'default'`) | Rule Review Console (admin only) | `validated_rules` table; all tenant/vertical/case fields NULL |

All promotion writes occur in a **single transaction** in the control-plane DB:
1. INSERT into `validated_rules` (the `vr_scope_consistency` CHECK fires).
2. UPDATE prior version (if supersession): `status = 'deprecated'`, `deprecated_at = now()`, `deprecated_by = reviewer_id`.
3. INSERT into `audit_events` (`event_type = 'rule_promoted'`).
4. INSERT into `skill_versions` (new manifest including the new rule).
5. INSERT into `audit_events` (`event_type = 'skill_version_created'`).
6. (Optionally, async after commit) emit cache-invalidation hint to the execution plane (§13.3).

If any step fails, the transaction rolls back; no partial promotion state is observable.

**Updates to per-tenant `rule_candidates`.** When a candidate is promoted, its source tenant's `rule_candidates.status` must be set to `'promoted'` and `rule_candidates.promoted_to_rule_id` must be set to the new `validated_rules.id`. This crosses the per-tenant DB boundary; the control plane invokes a thin internal endpoint on the execution plane (`POST /internal/candidates/mark-promoted`) over mTLS (per §6.2; client SAN `promotion.ctrl.victoria.internal`) immediately after the control-plane transaction commits.

**Reconciliation for the cross-DB edge case.** If the control-plane transaction commits but the per-tenant `rule_candidates` update fails, a reconciliation job (control-plane scheduled task) detects the gap by joining `validated_rules.promoted_from_candidate_id` against per-tenant `rule_candidates.id` and retries. The job runs every 5 minutes and emits an alert (§9.6.1) if reconciliation lag exceeds 30 minutes for any candidate. Tracked as part of REPL/reconciliation invariants (no longer named REPL_* since the replication-job framing is gone).

---

## 14. Decisions Made and Defended

| Decision | Rationale | Round |
|---|---|---|
| Go with chi + wire | Multi-service control plane with middleware chains, compile-time DI, and `context.Context` propagation; Go's type system and single-binary deployment enforce tenant-context invariants at compile time; native `crypto/tls` for mTLS; Temporal Go SDK is the primary SDK | R1 |
| tenant_id from JWT claim only | OWASP guidance; prevents privilege escalation; any other binding point is a security defect | R1 |
| Postgres RLS as defense-in-depth | Application-layer context propagation is necessary but not sufficient; RLS catches bugs the application layer misses | R1 |
| Single Temporal server, per-tenant task queues (Phase 1) | Product spec explicitly calls out Temporal task queues per tenant; shared server is simpler and adequate at ≤10 tenants; dedicated namespace deferred to Phase 3 | R1 |
| Execution plane continues when control plane is down | Operators cannot be blocked by a control-plane outage; provisioning and promotion can wait but running workflows cannot | R1 |
| Billing fails open on infrastructure unavailability | Blocking a solo operator's business due to a billing cache miss is worse than a brief over-count at sub-10 tenant scale | R1 |
| **R2-CARRY — Storage-layer audit immutability via trigger + role split** (§9.3) | Application-only convention is insufficient; `BEFORE UPDATE OR DELETE OR TRUNCATE` trigger raises even for privileged users. From learning-loop §11.5 and devil's advocate UE-04. | R2 → R3 carry |
| **R2-CARRY — Approval signals flow Gateway → Temporal direct; control plane off the hot path** (§6.6) | Operator approvals cannot depend on control-plane availability. Audit trail preserved via async `approval_received` event ingestion. | R2 → R3 carry (Consensus A3) |
| **R2-CARRY — Provisioning manifest delivered as config-map mount; HealthCheck is the ack** (§5.6) | Adopted from exec-plane Appendix B. Manifest is read-only, written before container start, validated at HealthCheck. | R2 → R3 carry (Consensus A2/§2 of overview) |
| **R3-REVERSAL — `validated_rules` UNIFIED in control-plane DB, RLS-enforced** (§13) | R2's trust-boundary split is reversed. Single table with `vr_scope_consistency` CHECK + RLS policy (learning-loop §2.8 DDL adopted verbatim). Eliminates the two-source merge in the execution-plane resolver. Cross-tenant safety preserved via the CHECK constraint and the `vertical_candidate_aggregates` view at promotion time. | R3 (Consensus A1, RESOLVED-C12) |
| **R3-REVERSAL — Single authoritative `audit_events` store in control plane** (§9.3, §9.4) | R2's dual-store + federated-query design is reversed. One table, partitioned by `tenant_id`, RLS-enforced. Execution plane uses `audit_events_outbox` durable buffer to upstream. | R3 (Consensus A8) |
| **R3-REVERSAL — MCP WRITE_EXTERNAL gate is direct Postgres SELECT, not HTTP endpoint** (§9.3) | R2's `GET /internal/audit/check-approval` endpoint is removed. MCP server connects with `mcp_audit_reader` role; RLS scopes to `event_type = 'approval_received'` AND own tenant. Lower latency on the hot path, fewer moving parts. | R3 (Consensus A5) |
| **R3-REVERSAL — Cross-tenant promotion uses on-demand `vertical_candidate_aggregates` view, not a stripped-replica replication job** (§13.4) | R2's `rule_candidates_control` continuous-replication design is removed. Aggregate is materialized at promotion-request time only; `vr_scope_consistency` CHECK constraint enforces tenant_id strip at storage layer. | R3 (Consensus A1) |
| **R3-NEW — mTLS for all internal service-to-service calls** (§6.2) | One mechanism for transport security, mutual identity, and replay protection. Replaces R1 HMAC + R2 ambiguity. Leaf certs per tenant; 30-day rotation; fingerprint-pinned. | R3 (RESOLVED-OQ-NEW-3) |
| **R3-NEW — Control-plane Auth Service is the sole operator JWT issuer** (§6.7) | Gateway is a caller, not an issuer. OTP one-time-use + nonce + refresh-token rotation. 24h JWT TTL pending product input on session length. | R3 (RESOLVED-OQ-06) |
| **R3-NEW — Control plane provisions per-tenant signal-only Temporal credentials for the gateway** (§6.8) | Gateway secret scope is disjoint from tenant secret scope and Temporal admin. 24h rotation. Cannot StartWorkflow / TerminateWorkflow / cross-tenant signal. | R3 (RESOLVED-OQ-NEW-5) |
| **R3-NEW — `TenantProvisioningWorkflow` provisions WhatsApp sessions** (§5.7) | Session storage and KEK provisioned by ctrl-plane; whatsmeow session operated by gateway in-process. Same secret-scope model as other per-tenant resources. | R3 (RESOLVED-N4) |
| **R3-NEW — System-wide idempotency key composition rule** (§17) | Single SHA-256 derivation rule with named components; registry of all 10 keys with derivers, consumers, dedup targets. Eliminates per-team key drift. | R3 (RESOLVED-N6) |
| **R3-NEW — `emit-alert` API as the canonical alert sink** (§9.6.1) | Vendor-agnostic; severity-routed; idempotent on key. Operator-ux gateway and exec-plane MCP servers emit; control plane routes. | R3 (RESOLVED-ALERT-SINK) |
| **R3-NEW — Replay scheduler trigger API** (§8.7) | `POST /admin/replays`. Idempotent on key. Pre-existing-state validation. Reviewer or admin only. | R3 (RESOLVED-REPLAY-SCHED) |
| **R4-CONFIRM — mTLS / single-store / gateway-cred R3 positions all hold** (§6.2, §9.3/§9.4, §6.8) | Mid-R4 reversals attempted (HMAC-bearer, two-store w/ local read, per-tenant cred scope) all withdrawn after peer R4 review confirmed all four specs aligned with R3 positions. R4-CONFLICT-1/2 RESOLVED. Single mTLS substrate; one authoritative `audit_events` + per-tenant `audit_events_outbox` drain; gateway secret scope. | R4 (RESOLVED-R4-CONFLICT-1, RESOLVED-R4-CONFLICT-2) |
| **R4-CONFIRM — `audit_events_outbox` rename** (§9.3, §18) | Renamed from R3 "per-tenant local `audit_events`" to make non-authoritative role unambiguous. Aligned with learning-loop §2.0 / §11.0. | R4 (RESOLVED-R4-CONFLICT-2) |
| **R4-NEW — TDD-depth ordering invariants → contract tests → implementation across §3, §5, §6, §7, §8, §9, §13, §17** | Moderator-required reordering. Property/fuzz tests in §12.6 (RLS isolation, JWT verification, idempotency-key derivation) with explicit input-space generators. | R4 (RESOLVED-PROPERTY-TESTS) |
| **R4-NEW — Storage Topology table** (§18) | Audit-related rows defer to learning-loop §2.0 as canonical; per-tenant entry is `audit_events_outbox`. | R4 (RESOLVED-STORAGE-TOPOLOGY) |
| **R4-NEW — Cross-component contract test table** (§19) | 26 named tests with given/then framing, mTLS framing throughout, mirrors of exec-plane CT and learning-loop CT-LL where applicable. | R4 (RESOLVED-CONTRACT-TESTS) |
| **R4-NEW — SC-01..SC-08 control-plane participation map** (§20) | Each scenario mapped to control-plane-side contract tests. SC-06 framing updated under RESOLVED-R4-CONFLICT-2 (control plane is now on the WRITE_EXTERNAL hot path's read source). | R4 (RESOLVED-SC-MAP) |

---

## 15. Open Questions and Anticipated Conflicts

### Resolved (across rounds)

| # | Question | Resolution |
|---|---|---|
| ~~OQ-01, OQ-08, OQ-09~~ | Rule push transport / timing / tiebreaker | **All resolved** by Consensus A1, A2, A10 (00-overview §2). Pull-at-start is authoritative; push is a non-authoritative cache hint; same-scope tiebreaker is most recent `promoted_at`. See §13.3. |
| ~~OQ-02~~ | Audit event buffer mechanism | **Resolved** as `audit_events_outbox` (§9.4); mechanism choice (Postgres queue vs. Temporal activity) is exec-plane's. |
| ~~OQ-06~~ | Operator OTP/JWT issuance ownership | **RESOLVED-OQ-06.** Control-plane Auth Service is the sole issuer. See §6.7. |
| ~~OQ-NEW-3~~ | Exec ↔ ctrl internal-call auth | **RESOLVED-OQ-NEW-3 / RESOLVED-R4-CONFLICT-1.** mTLS for all internal calls. See §6.2. |
| ~~OQ-NEW-4~~ | `rule_candidates_control` replication ownership | **No longer applicable.** Continuous replication removed in R3 reversal; vertical promotion uses on-demand `vertical_candidate_aggregates` view (§13.4). |
| ~~OQ-NEW-5~~ | Gateway Temporal credential provisioning | **RESOLVED-OQ-NEW-5.** Control plane provisions; gateway-only secret scope. See §6.8. |
| ~~OQ-OPEN-OTP-TTL~~ | Operator session length default | **RESOLVED-OPEN-OTP-TTL.** 24-hour platform default; per-tenant override path documented. See §6.7. |
| ~~OQ-OPEN-EMIT-ALERT-SCHEMA~~ | Alert payload schema | **RESOLVED-OPEN-EMIT-ALERT-SCHEMA.** JSON Schema with required fields and `severity` enum locked. See §9.6.1. |
| ~~OQ-OPEN-AUDIT-CANON-CONFORMANCE~~ | UPPER vs lower_snake_case `event_type` literals | **RESOLVED-OPEN-AUDIT-CANON-CONFORMANCE.** All values lower_snake_case per learning-loop §11.3; CI lint enforces. See §9.3. |
| ~~OQ-OPEN-Temporal-IAM~~ | Native Temporal IAM vs sidecar fallback | **RESOLVED-OPEN-Temporal-IAM.** Native IAM is path A; signal-proxy sidecar is path B; both ship with identical security-property contract tests. See §6.8. |
| ~~R4-CONFLICT-1~~ | Auth method (mTLS vs HMAC) | **RESOLVED-R4-CONFLICT-1.** mTLS — all four specs aligned. See §6.2. |
| ~~R4-CONFLICT-2~~ | Audit topology (single-store vs two-store-with-local-read) | **RESOLVED-R4-CONFLICT-2.** Single authoritative `audit_events` (control plane) + per-tenant `audit_events_outbox` (drain buffer only); MCP `WRITE_EXTERNAL` preflight reads control plane over mTLS via `mcp_audit_reader`. See §9.3 / §9.4. |

### Open after Round 5 (categorized)

**OPEN-PRODUCT-INPUT-X** (require product / legal / business input):

| # | Question | Owned by | Impact if unresolved |
|---|---|---|---|
| OPEN-PRODUCT-INPUT-OQ-05 | Minimum legal retention period for audit events given AU/UK/US jurisdictions | Product / Legal | Determines default retention config (current: 7 years pending sign-off). The 7-year default + WORM archive carry until product/legal locks. |
| OPEN-PRODUCT-INPUT-OQ-07 | Vertical assignment mutability: where is `tenants.vertical` modified after provisioning, and is multi-vertical (a tenant in two verticals) ever a thing? | Learning Architect + Product | Affects effective-rule-set resolution; current spec assumes single-vertical immutable post-provisioning. |
| ~~OPEN-PRODUCT-INPUT-N1~~ | Sandbox-mode canonical encoding | All architects | **RESOLVED-N1.** Confirmed by all four R4 specs (§16 "Confirmed in R4" row). `mode: 'sandbox' \| 'live'` adopted across provisioning manifest, workflow input, audit event field, and skill_version metadata. |

**OPEN-MEASUREMENT-X** (depend on runtime measurement; cannot be specified ahead of empirical data):

| # | Question | Owned by | Impact if unresolved |
|---|---|---|---|
| OPEN-MEASUREMENT-AUDIT-LATENCY-R4 | P99 of intra-VPC mTLS Postgres round-trip latency on the MCP `WRITE_EXTERNAL` preflight path. Target ≤50ms incremental on a hot path that already costs ~100s of ms (LLM call → tool call → preflight → write). | Exec-plane / observability + learning-loop | If P99 exceeds budget, fallback per learning-loop §2.0.1: reintroduce per-tenant outbox as a read source with explicit drain-lag semantics (R3 design). Single-store design ships at MVP; measurement post-MVP. |
| OPEN-MEASUREMENT-AUDIT-DRAIN-LAG | P99 lag between exec-plane `audit_events_outbox` insert and control-plane `audit_events` insert under steady state. Target ≤5min for the SC-08 reconciliation gap and the SLA in §6.6 trade-off. | Exec-plane / observability | If lag exceeds SLA, reconciliation alerts (§9.4 INV-INGEST-05) fire and operations may need to tune drain-worker concurrency. |

**OPEN-CROSS-SPEC-X** (require peer-spec coordination but no architectural decision needed):

| # | Question | Owned by | Impact if unresolved |
|---|---|---|---|
| OPEN-CROSS-SPEC-OQ-03 | Who triggers RuleCandidate confidence recalculation when new corrections arrive — Learning loop, control-plane event, or polling read? | Learning Architect | Affects when candidates cross the review threshold; control plane reads `confidence` from learning-loop. |
| OPEN-CROSS-SPEC-OQ-04 | Does the Learning Architect own the condition schema, or does the Evaluation Service need to parse conditions to run replay assertions? | Learning Architect | Affects EVAL invariants and the replay assertion engine. |
| OPEN-CROSS-SPEC-N3 | `vertical_candidate_aggregates` strip rules and PII contract test concrete by the time Rule Review Console implements vertical promotion. | Learning Architect | Concrete strip rules required before vertical-scope promotion can ship; affects §13.4. |
| ~~OPEN-CROSS-SPEC-C13~~ | `ApprovalSignalEnvelope` field completeness | Operator UX (producer); Learning + Exec-plane (consumers) | **RESOLVED-C13-R5.** Envelope locked at **16 fields** per operator-ux R5 §2.2 (C13-R5). R4's `correction_id` and `parse_status` reverted. Exec-plane R5 §7.4 confirmed; operator-ux A3 ratified. |
| OPEN-CROSS-SPEC-C14 | Field-name alignment between SkillVersion `rule_manifest[]` (learning §9) and exec-plane prompt block (exec-plane §8.3). | Exec-plane (consumer); Learning (producer) | Drift creates silent rule-application bugs. |
| OPEN-CROSS-SPEC-HERMES-VOL | Contract test that rule manifests never persist on the Hermes data volume. | Exec-plane | Confirms rules-as-context-only model holds; relevant for replay determinism. |
| ~~OPEN-CROSS-SPEC-AUDIT-CANON-GW-ADDITIONS~~ | The 7 gateway-source `event_type` values added in operator-ux R4 §9.7 | Learning Architect | **RESOLVED-CLOSED-R5.** All 7 op-ux pushed event_types present in learning-loop §11.3 with channel-agnostic R5 names adopted (learning-loop §11.3 line 2536). Writer registry updated. |

---

## 16. Assumptions About Other Architects' Components

### Confirmed across rounds

| Assumption | Status |
|---|---|
| The messaging gateway handles all inbound/outbound message routing; the control plane is never on the critical path of operator approval | **Confirmed** by operator-ux §1, §10 and Consensus A3. |
| Hermes runtime configuration is the Execution Plane Architect's responsibility | **Confirmed** by exec-plane §2; control plane only writes the manifest (§5.6). |
| `RuleCandidate.conditions` schema is owned by the Learning Architect; control plane treats as read-only | **Confirmed** by learning-loop §3, §4. |
| The execution plane buffers audit events on control-plane outage | **Confirmed** in R3 reversal: single-store ingest with `audit_events_outbox` durable buffer (§9.4). |
| The control plane Auth Service issues operator JWTs (gateway is a caller, not an issuer) | **Confirmed** in R3 by §6.7; aligns with operator-ux A7. |
| Provisioning manifest is delivered via read-only volume mount | **Confirmed** by exec-plane §2.7 / Appendix B; written by `DeployHermesContainer` (§5.6). |
| Sandbox-mode `WRITE_EXTERNAL` tools are absent from the MCP `list_tools()` response in sandbox mode | **Confirmed** by exec-plane §5; supports the fail-closed posture during control-plane outage (§11.1). |

### Reversed in Round 3 (consensus moved past R2 framing)

| R2 framing | R3 reality |
|---|---|
| Audit events split into per-tenant + control-plane stores; federated query API. | **Reversed.** Single `audit_events` table in control plane, partitioned by `tenant_id`, RLS-enforced. Execution plane uses durable buffer to write upstream. (§9.3, §9.4) |
| ValidatedRules split: tenant/case in per-tenant DB, vertical/default in `validated_rules_shared`. | **Reversed.** All four scopes in one control-plane `validated_rules` table, RLS-enforced (learning-loop §2.8 DDL adopted). (§13) |
| MCP servers call `GET /internal/audit/check-approval` HTTP endpoint for WRITE_EXTERNAL gate. | **Reversed.** MCP servers do a synchronous Postgres `SELECT` via the `mcp_audit_reader` role with RLS. (§9.3) |
| `rule_candidates_control` stripped-replica replication job owned by control plane. | **Reversed.** Cross-tenant promotion uses learning-loop's `vertical_candidate_aggregates` view, materialized on demand at promotion-request time. (§13.4) |
| HMAC + workload-identity for service-to-service auth. | **Reversed.** mTLS chosen as the single mechanism for all internal service calls. (§6.2) |

### Confirmed in R4

| Assumption | Status |
|---|---|
| Operator-ux accepts the gateway-secret-scope model (control plane provisions; gateway consumes; never crosses to tenant secret scope). | **Confirmed** in operator-ux R4 §10.4 (RESOLVED-CredentialStorage-Reconciled): `gateway/tenants/{tenant_id}/temporal_signal_client`. |
| Operator-ux gateway's audit writes use exactly the canonical `audit_events` schema (learning-loop §11.2) with no field deviations. | **Confirmed** by operator-ux R4 §3.1 cross-component contract tests (CT-Envelope→PersistCorrection, CT-AuditEmit→Registry). |
| Sandbox-mode canonical encoding (`mode: 'sandbox' \| 'live'`) is adopted across all four specs. | **Confirmed** by all four R4 specs (OPEN-N1 closed). |
| Single-store audit topology with per-tenant `audit_events_outbox` drain buffer. | **Confirmed** by all four R4 specs (RESOLVED-R4-CONFLICT-2). |
| mTLS for all internal service-to-service calls. | **Confirmed** by all four R4 specs (RESOLVED-R4-CONFLICT-1). |

### Still-open assumptions (folded into §15 OPEN-CROSS-SPEC-X)

The R4-still-open items now live as `OPEN-CROSS-SPEC-X` rows in §15 above to consolidate the open list under one categorized table.

---

## 17. Idempotency Key Registry (RESOLVED-N6)

**Decision.** A single derivation rule governs every idempotency key in the system, and a system-wide registry names every key, who derives it, who consumes it, and what it dedups against.

### 17.1 Invariants (TDD-first)

- `INV-IDEMP-01`: For each key in the registry, given identical components, the derived key is bit-exact identical regardless of which service computes it.
- `INV-IDEMP-02`: A duplicate INSERT with the same idempotency key returns success without inserting a second row. Enforcement is at the Postgres UNIQUE constraint, not the application layer.
- `INV-IDEMP-03`: Components are listed in fixed order per key type; reordering produces a different hash. (Catches accidental field-order changes during refactoring.)
- `INV-IDEMP-04`: The component values are logged alongside the hash in the audit event payload so investigators can reconstruct the key without reverse-engineering the hash.

### 17.2 Derivation rule

Every idempotency key in Victoria is computed as:

```
idempotency_key = sha256(component_1 || '' || component_2 || ... || component_N)
```

- **Hash function:** SHA-256, hex-encoded (lowercase).
- **Separator:** ASCII Unit Separator (``, byte `0x1F`). Chosen because it is unprintable, never appears in valid IDs, and removes ambiguity over component boundaries (no `:` collisions if a component happens to contain a colon).
- **Components:** Always include `tenant_id` first; subsequent components are domain-specific. Components are listed in the registry table below; the order is fixed per key type.

The advantage of a single rule: every team agrees on how to recover a key from its components for debugging, reproduction, and reconciliation. The hash is one-way (no recovery from hash to components), so observability includes the structured components alongside the hash in audit events and logs.

### 17.3 Idempotency key registry (system-wide)

| Key name | Derived by | Components (in order) | Dedups against | Storage / TTL |
|---|---|---|---|---|
| `signal_id` | Operator UX gateway | `(tenant_id, packet_id, action_button, raw_inbound_message_id)` | Duplicate Temporal signal delivery; gateway-side `correction_signal_delivery` row UNIQUE | Postgres UNIQUE on the row; no TTL (permanent) |
| `gateway_idempotency_key` | Operator UX gateway | `(tenant_id, signal_id, "signal", attempt_number)` | Duplicate signal send to Temporal (gateway retry) | Redis with 24h TTL; passed to exec-plane in `ApprovalSignalEnvelope` |
| `correction.idempotency_key` | Operator UX gateway → Learning consumes | `(tenant_id, decision_point_id, packet_id, action_button)` | Duplicate `corrections` row insertion | Postgres UNIQUE constraint on `corrections.idempotency_key`; no TTL |
| `packet_id` | Execution plane Temporal worker (`SendApprovalPacket` activity) | `(tenant_id, case_run_id, decision_point_id, "packet")` | Duplicate review packet send | Postgres UNIQUE on gateway's `outbound_queue` row; no TTL |
| `source_message_id` | WhatsApp / Telegram (provider-supplied) | Provider-native (not Victoria-derived) | Duplicate inbound webhook delivery | Gateway `inbound_dedup` row with 24h TTL |
| `mcp_tool.idempotency_key` | Execution plane Temporal worker (per activity) | `(tenant_id, case_run_id, decision_point_id, tool_name)` | Duplicate MCP tool execution within a Temporal retry | MCP server `mcp_idempotency_log` row; TTL = case_run lifetime |
| `audit_event.idempotency_key` | Each event emitter (writer) | `(tenant_id, event_type, ref_id, sequence_or_timestamp)` | Duplicate audit_events INSERT | Postgres UNIQUE on `audit_events.idempotency_key`; no TTL |
| `replay_run.idempotency_key` | Evaluation Service / replay scheduler | `(tenant_id, original_case_run_id, candidate_id_or_skill_version_id, "replay")` | Duplicate replay_run row | Postgres UNIQUE on `replay_runs`; no TTL |
| `alert.idempotency_key` | Alert emitter (gateway, exec-plane, control-plane) | `(tenant_id, code, time_bucket)` where time_bucket is the alert's natural dedup window (e.g., 5-min bucket for warnings) | Duplicate alert routing within the bucket | Postgres UNIQUE on `alerts.idempotency_key`; bucket is rolling |
| `otp_request_id` | Auth Service | `(tenant_id, phone_e164, nonce)` (the nonce is random; this is the dedup against accidentally re-issuing OTPs to the same phone in flight) | Concurrent OTP requests | Postgres `otp_requests` UNIQUE; 5-min TTL |

### 17.4 Coordination with peer specs

This registry is the authoritative cross-team source. Required peer-spec updates to align:

- **Operator UX (§2.2 / §10.1):** `correction.idempotency_key` derivation per the registry; the gateway computes it before writing the `corrections` row remotely (per Consensus A9, exec-plane writes; gateway's role is to compute and pass the key in the signal envelope).
- **Execution plane (§3.4 activity catalog):** activity-level idempotency keys per the `mcp_tool.idempotency_key` row above; replace ad-hoc `{case_run_id}:{decision_point_id}:{tool_name}` patterns with the SHA-256 form.
- **Learning-loop (§2.6 corrections schema, §11.2 audit_events schema):** `idempotency_key` UNIQUE columns retain their UNIQUE constraints; the values are now SHA-256 hashes per the registry.

### 17.5 Contract tests for idempotency

- `test_key_determinism`: For each registry entry, compute the key from the same component tuple twice (different processes, different services); assert byte-equal output.
- `test_key_distinctness`: Compute keys for 10⁵ random component tuples; assert no collisions.
- `test_key_order_sensitivity`: Permute the components of a registered key; assert the permutation produces a different hash.
- `test_duplicate_insert_idempotent`: INSERT a row with `idempotency_key = K`; INSERT again with the same K; assert exactly one row exists in the target table.
- `test_components_in_audit_payload`: Emit an audit event whose key was derived from `(t_X, dp_Y, pkt_Z, "wrong_action")`; assert the audit row's `payload` contains a structured field naming each component value.

---

## 18. Storage Topology (RESOLVED-STORAGE-TOPOLOGY)

The audit-related rows defer to **learning-loop §2.0 / §2.0.1 / §11.0 as canonical** (RESOLVED-R4-CONFLICT-2). The other rows are this spec's authoritative description of every Postgres table the control plane owns or coordinates.

| Table | Database | Writer service(s) | Reader service(s) | RLS / access policy | Immutability |
|---|---|---|---|---|---|
| `tenants` | Control plane | Provisioning workflow (control plane) | All control-plane services; provisioning manifest builder | RLS off (platform-scope rows; access via JWT role) | Mutable (status updates) |
| `workflow_templates` | Control plane | Platform admin | All services that resolve rule sets | RLS off | Mutable |
| `validated_rules` | Control plane | Rule Review Console (control plane) | Resolver (`LoadSkillVersion` endpoint, control plane); reviewers via audit query | RLS via `vr_tenant_isolation` (learning-loop §2.8); `vr_scope_consistency` CHECK constraint | Append + status updates; rollback creates a new row, never mutates a prior `active` row |
| `skill_versions` | Control plane | Promotion pipeline (control plane) | Resolver | RLS via tenant_id (NULL for vertical/default) | Append-only; `status` flag mutates (active/deprecated/rolled_back) |
| `audit_events` (single authoritative store) | Control plane | `audit_writer` role: drain worker (from per-tenant `audit_events_outbox`), control-plane services (provisioning, promotion), gateway via emit-audit, MCP servers via emit-audit | `audit_reader` role: audit query API, observability stack. **`mcp_audit_reader` role over mTLS** (RESOLVED-R4-CONFLICT-2) for the MCP `WRITE_EXTERNAL` preflight via `mcp_approval_events` view. | RLS by `tenant_id` (ctrl-plane §3.5 + §9.6); `mcp_audit_reader` further restricted to `event_type='approval_received'` for the SAN-derived `app.current_tenant`. **Canonical row: learning-loop §2.0 row 127.** | Storage-layer enforced: `audit_writer` INSERT-only role + BEFORE UPDATE/DELETE/TRUNCATE trigger; WORM archive (S3 Object Lock) on partition rotation |
| `audit_events_outbox` | Per-tenant execution-plane | Exec-plane services on every audit write (transactional with the business INSERT) | Drain worker only — never read by MCP or any other consumer | Single-tenant DB connection scoping; `audit_writer` role-equivalent on the per-tenant DB | Append + `drained` flag mutates from false to true on successful upstream ack; `BEFORE UPDATE` allows the flag flip only. **Canonical row: learning-loop §2.0 row 128 + §11.0.** |
| `case_runs` | Per-tenant execution-plane | Exec-plane Temporal worker | Exec-plane services; reviewers via federated lookup | Single-tenant DB | Append + `status` and `completed_at` updates |
| `decision_points` | Per-tenant execution-plane | Exec-plane Temporal worker | Exec-plane services; reviewers | Single-tenant DB | Append-only |
| `artifacts` | Per-tenant execution-plane | Exec-plane MCP servers; replay workflow | Exec-plane services; preview server (signed URL) | Single-tenant DB | Append-only |
| `corrections` | Per-tenant execution-plane | Exec-plane `PersistCorrection` activity (sole writer per Consensus A9) | Learning candidate-matcher (Stage B parser); reviewers | Single-tenant DB | Append + `parse_status` updates |
| `rule_candidates` | Per-tenant execution-plane | Learning candidate-matcher (in tenant DB) | Aggregation job (read-only, on demand for `vertical_candidate_aggregates`); promotion pipeline | Single-tenant DB | Append + `status` updates; promoted candidates marked via `POST /internal/candidates/mark-promoted` |
| `vertical_candidate_aggregates` (view, materialized on demand) | Control plane | Learning aggregation job (writes the strip-and-aggregate result) | Rule Review Console at promotion-request time | Reviewer-only access | Truncate-and-rebuild per request; never carries source `tenant_id` / `correction_id` / `case_run_id` |
| `quarantined_aggregates` | Control plane | Learning aggregation job (when PII auto-redaction flags a candidate as needing manual review) | Reviewer admin tool | Admin-only | Append-only |
| `replay_runs` | Control plane | Evaluation Service (control plane) | Reviewers; observability | RLS by `tenant_id` (where set) | Append + `status` and result fields update on completion |
| `replay_assertions` | Control plane | Evaluation Service | Reviewers | RLS by `tenant_id` via parent `replay_runs` | Append-only |
| `deployments` | Control plane | Provisioning workflow | Admin tools; ops | Admin-only | Append + status updates |
| `otp_requests` | Control plane | Auth Service | Auth Service | RLS by `tenant_id` | Append + `consumed_at` update |
| `refresh_tokens` | Control plane | Auth Service | Auth Service | RLS by `operator_id` (and tenant) | Append + `revoked_at` update |
| `alerts` | Control plane | Alert sink (`POST /internal/alerts` from gateway, exec-plane, control-plane services) | Observability stack; on-call dashboards | RLS by `tenant_id` (NULL for platform alerts) | Append + `repeat_count`, `last_seen_at`, `resolved_at` updates |
| `alert_policy` | Control plane | Admin | Alert routing layer | Admin-only | Mutable |
| `outbound_queue` | Gateway DB | Operator-ux gateway | Operator-ux drain worker | Single-tenant key (`tenant_id` column) | Append + `status`, `attempted_at`, `delivered_at` updates |
| `inbound_dedup` | Gateway DB | Operator-ux gateway | Operator-ux gateway | n/a (provider message id is the natural dedup) | TTL-bounded |
| `channel_bindings` | Gateway DB | Operator-ux gateway (rows created via control-plane provisioning callback) | Operator-ux gateway | Single-tenant key | Mutable (`session_status`) |
| `correction_signal_delivery` | Gateway DB | Operator-ux gateway | Operator-ux gateway | Single-tenant key | Append + `signal_delivered_at` update |
| `mcp_idempotency_log` | Per-tenant execution-plane | MCP server | MCP server | Single-tenant DB | Append + result-cache rows; TTL = case_run lifetime |

**Notes:**
- "Single-tenant DB" means the database is per-tenant and structurally cannot be queried for another tenant — RLS is unnecessary because the DB connection itself is tenant-scoped.
- The control-plane `audit_events` table is partitioned by `tenant_id` (then by `occurred_at` month within each tenant) — partition keys do not affect RLS visibility, which is enforced separately.
- `validated_rules` is the only control-plane table that holds rows belonging to multiple tenants (case/tenant scope) AND rows belonging to no single tenant (vertical/default scope). The `vr_scope_consistency` CHECK constraint and the `vr_tenant_isolation` RLS policy together prevent cross-tenant leakage.
- The R2 `validated_rules_shared` table is **not present**; it has been merged into the unified `validated_rules` per Consensus A1 (REVISED-C1).
- The mid-R4 reversal that placed a parallel read source in `audit_events_outbox` is **withdrawn** (RESOLVED-R4-CONFLICT-2); the outbox is a drain buffer only. Audit-related rows defer to learning-loop §2.0 as canonical.
- The R3 `rule_candidates_control` continuous-replication target is **not present**; cross-tenant promotion uses `vertical_candidate_aggregates` materialized on demand.

---

## 19. Cross-Component Contract Test Table (RESOLVED-CONTRACT-TESTS)

The complete list of peer-boundary contract tests. Each row names the test, the producer service, the consumer service, the input fixture (given), the expected assertion (then), and which spec owns the fixture vs. the consumer. Tests are runnable in CI as the cross-component integration suite. Cross-references to peer suites: exec-plane §11 contract test table; learning-loop §13.0 CT-LL-1..13; operator-ux §3.1.

| Test name | Producer | Consumer | Input fixture (given) | Expected assertion (then) | Fixture owner | Consumer owner |
|---|---|---|---|---|---|---|
| `test_provisioning_manifest_delivered` | Control plane provisioning workflow | Exec-plane Hermes container | **Given** synthetic tenant request `{vertical: "roofing"}` and a fresh execution-plane DB | **Then** manifest exists at `/hermes/config/manifest.json` with `tenant_id` matching volume label; HealthCheck blocks until container reads it; HealthCheck returns 200 only after manifest is read | ctrl-plane §5.6 | exec-plane §2.7 |
| `test_load_skill_version_authoritative` | Control plane (resolver endpoint over mTLS) | Exec-plane Temporal worker (`LoadSkillVersion` activity) | **Given** tenant T_X with 3 active rules (1 case, 1 tenant, 1 vertical); exec-plane presents leaf SAN `t_X.exec.victoria.internal`; calls `LoadSkillVersion(t_X, "quote_drafting")` | **Then** returned manifest contains exactly 3 rules ordered by scope-priority (case > tenant > vertical > default); `skill_version_id` is recorded in `case_runs.skill_version_id` and pinned for the run | exec-plane §8.2 | exec-plane §8.2 |
| `test_audit_ingest_drain_no_loss` | Exec-plane drain worker | Control plane `POST /internal/audit/events` over mTLS | **Given** 100 audit events emitted from exec-plane test harness; control-plane DB stopped for 5 min; then restarted | **Then** exactly 100 rows in control-plane `audit_events`; all per-tenant `audit_events_outbox` rows have `drained = true`; idempotency_key UNIQUE constraint prevented any duplicate inserts | exec-plane §13 | ctrl-plane §9.4 |
| `test_audit_san_tid_mismatch` | Adversarial test harness | Control plane audit ingest | **Given** mTLS leaf with SAN `t_A.exec.victoria.internal`; payload `tenant_id = T_B` | **Then** HTTP 403 + `security_violation` audit row written with `actor_id` derived from SAN | ctrl-plane §9.4 | ctrl-plane §9.4 |
| `test_emit_alert_idempotent` | Operator-ux gateway | Control plane `POST /internal/alerts` | **Given** the same `idempotency_key` submitted 3x in 30s | **Then** exactly one row in `alerts`; `repeat_count = 3`; routing webhook called exactly once within suppression window | operator-ux §4.7 | ctrl-plane §9.6.1 |
| `test_emit_alert_severity_invalid` | Adversarial test harness | Control plane alert sink | **Given** payload with `severity: "fatal"` | **Then** HTTP 400 (`severity_invalid`); no DB write | ctrl-plane §9.6.1 | ctrl-plane §9.6.1 |
| `test_replay_scheduler_trigger` | Control plane Evaluation Service | Exec-plane `POST /internal/replay` over mTLS | **Given** `replay_kind: "candidate_validation"`, valid `original_case_run_id`, valid `candidate_id` | **Then** exec-plane returns 202 with Temporal `WorkflowID`; `replay_runs` row transitions to `running` | ctrl-plane §8.7 | exec-plane §13.3 / §3.4 |
| `test_replay_scheduler_idempotent` | Control plane Evaluation Service | Itself | **Given** same `idempotency_key` submitted twice | **Then** a single `replay_runs` row; second call returns the same `replay_run_id` with current status | ctrl-plane §8.7 | ctrl-plane §8.7 |
| `test_jwt_verify_well_formed` | Operator-ux gateway (over mTLS) | Control plane `/auth/operator/otp/verify` | **Given** valid OTP after `otp/initiate`; gateway presents leaf SAN `gateway.victoria.internal` | **Then** JWT returned with `tid` set server-side from the operator's identity record; `nonce` bound to OTP request; `aud = "operator"`; `role = "operator"` | operator-ux §10.1 | ctrl-plane §6.7 |
| `test_jwt_verify_replay` | Adversarial test harness | Control plane `/auth/operator/otp/verify` | **Given** same `otp_request_id` submitted twice | **Then** first → 200; second → 409 (`OTP_ALREADY_CONSUMED`); no second JWT issued | ctrl-plane §6.7 | ctrl-plane §6.7 |
| `test_jwt_refresh_chain_invalidation` | Adversarial test harness | Control plane `/auth/operator/refresh` | **Given** refresh token used twice | **Then** first → 200 with new tokens; second → 401; entire chain invalidated; `security_violation` audit row written | ctrl-plane §6.7 | ctrl-plane §6.7 |
| `test_temporal_cred_signal_only` | Operator-ux gateway | Temporal cluster (or signal-proxy sidecar fallback) | **Given** per-tenant signal-only credential loaded from `gateway/tenants/<tid>/temporal_signal_client` | **Then** `SignalWorkflow` succeeds on `victoria.tenant.t_X.*`; `StartWorkflow`, `TerminateWorkflow`, `CancelWorkflow`, `ResetWorkflow`, `DescribeWorkflowExecution` all fail with authorization error | ctrl-plane (mints) + operator-ux (consumes) | ctrl-plane §6.8 + operator-ux §10.4 |
| `test_temporal_cred_cross_tenant_rejected` | Adversarial test harness | Temporal cluster | **Given** tenant T_A's credential; target task queue `victoria.tenant.t_B.*` | **Then** authorization error from Temporal | ctrl-plane | operator-ux §10.4 |
| `test_native_or_sidecar_fallback` | Test runner | Either Temporal cluster (path A) or signal-proxy sidecar (path B) | **Given** the full security-property suite (`test_temporal_cred_signal_only` + `test_temporal_cred_cross_tenant_rejected`) | **Then** all security invariants hold in both modes; no test diverges between path A and path B | ctrl-plane §6.8.3 | exec-plane (sidecar) |
| `test_mtls_handshake_required` | Adversarial test harness | Any control-plane `/internal/*` endpoint | **Given** plaintext TCP connect or TLS without client cert | **Then** TLS handshake fails (or rejected at listener); no handler runs; no DB write | ctrl-plane §6.2 | ctrl-plane §6.2 |
| `test_mtls_san_workload_identity` | Adversarial test harness | Audit ingest | **Given** leaf cert with SAN `t_A.exec.victoria.internal`; submit event for `t_B` | **Then** HTTP 403 + `security_violation` audit row | ctrl-plane §6.2 | ctrl-plane §6.2 |
| `test_mtls_untrusted_ca_rejected` | Adversarial test harness | Any internal listener | **Given** leaf signed by a non-Victoria CA | **Then** TLS handshake failure | ctrl-plane §6.2 | ctrl-plane §6.2 |
| `test_mtls_expired_cert_rejected` | Adversarial test harness | Any internal listener | **Given** leaf with `notAfter` in the past | **Then** TLS handshake failure with `cert_expired` | ctrl-plane §6.2 | ctrl-plane §6.2 |
| `test_mtls_rotation_overlap` | Test harness | Any internal listener | **Given** a workload's cert is rotated; calls made with previous and current leaves during the 24h overlap | **Then** both succeed during overlap; calls with neither fail | ctrl-plane §6.2 | ctrl-plane §6.2 |
| `test_envelope_to_persist_correction` | Operator-ux gateway | Exec-plane `PersistCorrection` activity | **Given** the canonical 16-field `ApprovalSignalEnvelope` (operator-ux §2.2, C13-R5) | **Then** the `corrections` row is built from the envelope alone (no callback to gateway); every column matches the envelope→column map (operator-ux §2.2 / learning-loop §3.3) | operator-ux §2.2 | exec-plane §7.4 |
| `test_envelope_missing_field_rejected` | Adversarial test harness | Exec-plane `PersistCorrection` | **Given** envelope missing `idempotency_key` (one of 16 required fields) | **Then** `MalformedSignalError` (non-retryable); workflow transitions to `CorrectionParseFailed`; no partial `corrections` row; `correction_parse_failed` audit emitted | operator-ux §2.2 | exec-plane §7.4 |
| `test_audit_writer_registry` | Adversarial test harness | Control-plane `audit_events` | **Given** a service connecting as `audit_writer` attempts to insert an `event_type` outside its registered set per learning-loop §11.6 | **Then** application-layer rejection at the emit-audit boundary AND DB-layer rejection via the writer-registry trigger that consults `audit_event_writer_registry` | learning-loop §11.6 | ctrl-plane §9.3 |
| `test_audit_canon_event_type_lint` | CI lint | Source code (control-plane services) | **Given** repo source scan | **Then** all `event_type` literal strings match `^[a-z][a-z0-9_]*$` and appear in the learning-loop §11.3 42-entry registry | learning-loop §11.3 | ctrl-plane §9.3 |
| `test_rls_cross_tenant_select` | Property test | Control-plane `validated_rules` and `audit_events` | **Given** random SELECT shapes (WHERE / JOIN / UNION / subquery / ORDER BY) generated by the §12.6.1 fuzz harness with `app.current_tenant = t_X` for random `t_X` over N=60 trials per shape | **Then** result rows never contain `tenant_id != t_X` (excluding shared-scope rows where `tenant_id IS NULL` for `validated_rules`); `app.bypass_tenant_check = false` is the default | ctrl-plane §12.6.1 | ctrl-plane §13.2, §9.6 |
| `test_idempotency_key_property` | Property test | All consumers of the registry §17 | **Given** N=10⁵ random component tuples per registered key drawn per §12.6.3 generator (each key listed in §17.3 with its own component vocabulary) | **Then** determinism, distinctness, and order-sensitivity all hold | ctrl-plane §12.6.3 | ctrl-plane §17 |
| `test_mcp_approval_view_scope` | Test harness | Control-plane `mcp_approval_events` view (over mTLS) | **Given** insert 1 `approval_received` row + 1 `correction_received` row for `t_A`; insert 1 `approval_received` for `t_B`; MCP server connects as `mcp_audit_reader` over mTLS with SAN `t_A.mcp.victoria.internal` (which sets `app.current_tenant = t_A`) | **Then** SELECT from `mcp_approval_events` returns exactly 1 row (`t_A` `approval_received`); SELECT from `audit_events` base table returns permission denied; mirrors learning-loop CT-LL-9 | learning-loop §11.5 | ctrl-plane §9.3 |
| `test_audit_outbox_drain_idempotent` | Test harness | Control plane `POST /internal/audit/events` | **Given** the same outbox row drained twice (once succeeds, then drain worker retries believing the first ack was lost) | **Then** exactly one row in control-plane `audit_events` (idempotency_key UNIQUE); second POST returns 200 with `X-Idempotent: true` | ctrl-plane §9.4 | ctrl-plane §9.4 |

---

## 20. End-to-End Acceptance Scenario Participation (RESOLVED-SC-MAP)

Devil's Advocate's SC-01..SC-08 (`05-architecture-integration-critique.md` §5) are the system-level acceptance test charter. This table maps each scenario to the control-plane-side contract tests that own the assertions.

| Scenario | Control-plane participation | Tests that own the control-plane-side assertions |
|---|---|---|
| **SC-01 Golden-path correction** (full sandbox loop, correction → candidate → promotion → re-run) | Heavy — provisioning, audit ingest, JWT issuance, rule promotion write, skill_version creation, audit query for verification | `test_provisioning_manifest_delivered`, `test_audit_ingest_drain_no_loss`, `test_jwt_verify_well_formed`, `test_load_skill_version_authoritative` (initial + post-promotion) |
| **SC-02 Multi-correction promotion** (3 corrections converge to 1 candidate; promotion writes ValidatedRule) | Heavy — promotion atomic transaction, skill_version emission, replay scheduling | `test_replay_scheduler_trigger` (for pre-promotion validation replay), `test_load_skill_version_authoritative` (post-promotion), promotion-write contract test |
| **SC-03 Contradicting-correction supersession** (vr_0042 v1 → v2 with `supersedes` link) | Heavy — supersession transaction, rollback path, RLS enforcement on subsequent reads | RLS isolation property test, promotion-write contract test (with supersession), rollback path tests |
| **SC-04 Abandoned packet** (operator never replied; workflow times out) | Light — audit ingest of `case_abandoned` event; alert emission if SLA breached | `test_audit_ingest_drain_no_loss` (for the `case_abandoned` event type), `test_emit_alert_idempotent` (if alert is fired) |
| **SC-05 Tenant isolation leak attempt** (crafted signal/payload referencing another tenant) | Heavy — RLS, JWT `tid` enforcement, mTLS SAN-tid mismatch detection | `test_rls_cross_tenant_select`, `test_mtls_san_workload_identity`, `test_audit_san_tid_mismatch`, `test_temporal_cred_cross_tenant_rejected` |
| **SC-06 Sandbox mode escape attempt** (live tool call in sandbox mode) | **Heavy** in the R4-resolved single-store design — the MCP server's `WRITE_EXTERNAL` preflight reads control-plane `audit_events` over mTLS via `mcp_audit_reader`. The gate's *read source* is in the control plane; the *gating logic* is in the MCP server (exec-plane §5.6). Audit ingest of `sandbox_escape_blocked` follows. | `test_mcp_approval_view_scope` (proves the read source is correctly scoped to `approval_received` AND own tenant); `test_audit_ingest_drain_no_loss` for the `sandbox_escape_blocked` event type; mirrors learning-loop CT-LL-9 |
| **SC-07 Replay determinism check** (replay produces identical decision-point outcomes given pinned skill_version_id) | Heavy — replay scheduler trigger, skill_version pin lookup, replay_runs / replay_assertions writes | `test_replay_scheduler_trigger`, `test_replay_scheduler_idempotent`, `test_load_skill_version_authoritative` with `as_of` |
| **SC-08 Double-tap approval idempotency** (operator double-taps; same signal_id arrives twice) | Light — audit-side: `approval_received` event has `signal_id` and `gateway_idempotency_key` in `payload`; second arrival deduped by audit `idempotency_key` UNIQUE | `test_audit_outbox_drain_idempotent` (for the duplicate-event case); audit `idempotency_key` UNIQUE constraint test |

**Where control plane has no participation:** none of SC-01..SC-08 are entirely control-plane-free. Even SC-04 (abandoned packet) involves audit ingest of the timeout event. SC-06's framing is updated under R4-CONFLICT-2: with the MCP read source moved to the control-plane authoritative store, the control plane's role shifts from "after-the-fact audit recipient" to "synchronous read source on the WRITE_EXTERNAL hot path"; SC-06 becomes a heavy-participation scenario rather than light.

---

## 21. Implementation Handoff (R5)

A single-page summary for engineering hand-off. Cross-references are absolute (this spec §X) or peer-spec (`<file>` §X).

### 21.1 Services to build, in dependency order

| # | Service | Depends on | Spec location |
|---|---|---|---|
| 1 | **Internal CA + cert-issuance scheduler** | Postgres + secret store | §5.2 step 6 (`IssueServiceCertificates`); §6.2 mechanism + rotation |
| 2 | **Tenant Provisioning Workflow** (Temporal) | Internal CA, Postgres, secrets store, Temporal | §5.2; §5.5 (deprovision); §5.6 (manifest); §5.7 (WhatsApp session); §6.8 step 8 (gateway Temporal cred) |
| 3 | **Auth Service** (Go package) | Tenant Registry, Redis | §6.1 identity; §6.3 tenant_id binding; §6.4 roles; §6.5 invariants; §6.7 OTP/JWT |
| 4 | **API Gateway + TenantContextMiddleware + AuthMiddleware** (Go shared middleware) | Auth Service, mTLS terminator | §3 (tenant-context primitives); §6.2 (mTLS verify); §6.5 (JWT verify) |
| 5 | **Audit Ingest service + drain endpoint + writer-registry trigger** | Postgres `audit_events`, mTLS | §9.3; §9.4; learning-loop §11.2 / §11.5 / §11.6 |
| 6 | **Audit Query API + `mcp_audit_reader` role grant + view** | Audit Ingest | §9.6; §9.3 (view); learning-loop §11.5 |
| 7 | **Alert Sink** (`POST /internal/alerts`) | Audit Ingest, route configuration | §9.6.1 |
| 8 | **Rule Review Console API + promotion transaction** | Audit Ingest, `validated_rules`, `skill_versions` | §7; §13.5 (single-transaction promotion); learning-loop §2.8 (DDL) |
| 9 | **Effective-rule-set resolver + LoadSkillVersion endpoint** | `validated_rules`, mTLS | §13.3 |
| 10 | **Evaluation Service / Replay Scheduler** | `replay_runs`, exec-plane `/internal/replay` | §8; §8.7 |
| 11 | **Observability service + OTel collector config** | All other services | §9.1 / §9.2 |
| 12 | **Billing & Admin** | All other services | §10 |

### 21.2 Integration points with each peer

| Peer | Integration | Reference |
|---|---|---|
| **Exec-plane (`02-execution-plane.md`)** | mTLS internal RPCs (`/internal/replay`, `/internal/health`, `/internal/candidates/mark-promoted`) — exec-plane §13.1 INV-RPC1..4 are canonical mTLS invariants; control plane mirrors. | §5.3, §6.2, §6.2.3 application table |
| **Exec-plane** | `LoadSkillVersion` over mTLS, returning learning-loop §9.1 manifest. | §13.3 |
| **Exec-plane** | Audit drain — exec-plane writes to `audit_events_outbox`, drain worker POSTs to control-plane `/internal/audit/events`. | §9.4 |
| **Exec-plane** | MCP server (per-tenant) connects to control-plane DB as `mcp_audit_reader` over mTLS for WRITE_EXTERNAL preflight. | §9.3 (RESOLVED-R4-CONFLICT-2) |
| **Learning-loop (`03-correction-loop.md`)** | Adopts learning-loop §2.8 `validated_rules` DDL verbatim; learning-loop §2.0 / §2.0.1 / §11.0 is canonical for audit storage rows; learning-loop §11.3 is the canonical 42-entry `event_type` registry; learning-loop §11.6 is the canonical writer registry. | §13, §18, §9.3 |
| **Learning-loop** | `vertical_candidate_aggregates` view — Rule Review Console reads at promotion-request time only; learning-loop materializes on demand. | §13.4 |
| **Operator-ux (`04-operator-experience.md`)** | OTP/JWT issuance — gateway calls Auth Service over mTLS. | §6.7 |
| **Operator-ux** | Gateway Temporal credential — control plane provisions in `gateway/tenants/{tenant_id}/temporal_signal_client`; gateway reads. | §6.8 |
| **Operator-ux** | Channel binding callback — provisioning workflow calls gateway's `POST /channel-bindings` to initialize whatsmeow session. | §5.7 |
| **Operator-ux** | Alert emission — gateway calls control plane's `/internal/alerts` over mTLS. | §9.6.1 |
| **Operator-ux** | 16-field `ApprovalSignalEnvelope` (operator-ux §2.2, C13-R5) carries `signal_id` and `gateway_idempotency_key` for audit reconciliation. Control plane sees these in the `approval_received` audit payload. | §6.6 |
| **Devil's advocate (`05-architecture-integration-critique.md`)** | SC-01..SC-08 acceptance scenarios — control-plane participation mapped in §20. | §20 |

### 21.3 Test fixtures owned by control plane (vs. consumed)

**Owned (this spec produces, peers consume):**
- mTLS internal CA test root + leaf-issuance harness (§6.2.2)
- JWT issuer test signing-key + valid/well-formed-claim corpus (§12.6.2)
- `replay_runs` / `replay_assertions` schema + test fixtures (§8.6, §8.7)
- `validated_rules` fixture rows for all four scopes (§13.1)
- `audit_events` fixture rows for the writer-registry contract test (§9.3.2)
- `alerts` payload corpus including `severity_invalid` adversarial cases (§9.6.1.2)
- Idempotency-key test tuples for the 10 registered keys (§17.3)

**Consumed from peers:**
- Provisioning manifest schema fixtures: exec-plane §2.7 / Appendix B
- `ApprovalSignalEnvelope` 16-field corpus (C13-R5): operator-ux §2.2 / §3.1
- `event_type` 42-entry registry: learning-loop §11.3
- `vertical_candidate_aggregates` strip-rule fixtures: learning-loop §13.5 (pending OPEN-CROSS-SPEC-N3)
- SkillVersion `rule_manifest[]` schema: learning-loop §9.1 + §9 cross-ref to exec-plane §8.3 (pending OPEN-CROSS-SPEC-C14)

### 21.4 Pre-commit invariants the build pipeline must enforce

Each invariant runs as a hook; failure blocks the commit.

| Invariant | Tool / mechanism |
|---|---|
| No `event_type` literal outside the lower_snake_case pattern `^[a-z][a-z0-9_]*$` AND in learning-loop §11.3 registry | CI lint (`test_audit_canon_event_type_lint` from §19) |
| No INSERT into `audit_events` from any role other than `audit_writer` | Migration linter + pre-commit grep (no direct `INSERT INTO audit_events` outside the AuditEmitter module) |
| No service handler reads `tenant_id` from query params, request body, or a client-set header | `go vet` custom analyzer over HTTP handler signatures (§3.3) |
| Every Postgres table holding tenant-scoped data has an RLS policy defined | Migration linter (§3.5) |
| Every OTel-instrumented handler emits a span with `tenant_id` attribute (except pre-auth spans) | OTel-span lint (§9.2) |
| No bearer-token / HMAC-signed-token construction in code outside the JWT issuer (Auth Service) | Codebase grep for `Authorization: Bearer` outside `auth-service/` and edge-LB JWT handler |
| No fallback to plaintext on mTLS handshake failure | Test that the mTLS client throws on handshake failure and never retries on plaintext (§6.2 INV-AUTH-S2S-06) |
| Every promotion write is wrapped in a single Postgres transaction | Code-review checklist + `test_promotion_atomicity` (§13.5) |
| `vr_scope_consistency` CHECK constraint is present on `validated_rules` | Migration linter |
| `idempotency_key` UNIQUE on every table listed in §17.3 | Migration linter |
| WORM archive (S3 Object Lock) is configured on the audit-archive bucket | Infrastructure-as-code lint (§9.3.3) |

---
