# Design Notes — what's real, what isn't, and what I'd do next

Victoria is an **architecture study**: a working, production-*shaped* harness for
a "correction loop" product, not a shipped system. This document is the honest
map a reviewer or engineer should read before judging the repo. It's written to
be useful precisely where a polished README usually goes quiet: the seams, the
shortcuts, and the miles still to walk.

The short version: the **engine and its seams are genuinely built**; the things
that make a multi-tenant SaaS *safe* and *general* are deliberately stubbed or
MVP-grade. I chose to build the interesting middle (the correction-loop ledger
and the operator surface) end-to-end rather than spread effort thin across a
plausible-but-shallow whole.

---

## What's real vs. what's stubbed

| Area | State | Notes |
|---|---|---|
| Correction-loop ledger (correction → candidate → promotion → immutable SkillVersion → replay) | **Real, end-to-end** | Proven over HTTP in `test/e2e/http_e2e_test.go`; a promoted rule changes the next case's action. |
| Confidence scoring | **Real** | Wilson lower bound + recency decay + scope consistency (`internal/domain/confidence.go`). |
| Idempotency / dedup / append-only audit | **Real, DB-enforced** | `UNIQUE` constraints + an append-only trigger in the Postgres store — not just convention. |
| Dev/prod safety boundary | **Real** | Impersonation routes are compiled out by build tag, asserted absent in `test/e2e/http_e2e_proddev_test.go`. |
| WhatsApp operator surface | **Real** | `go.mau.fi/whatsmeow`; tappable review packets now drive the correction loop. |
| Reasoning agent (DeepSeek) | **Real, optional** | Drafts facts/action/reply at unlearned decision points; off → deterministic drafts. |
| Per-tenant authentication | **Stub by design** | `Bearer tid:<tenant_id>` is identity-assertion only — see below. |
| Tenant isolation | **MVP** | One shared process + Postgres pool, scoped by `WHERE tenant_id=$1`. No RLS, no DB-per-tenant. |
| Rule extraction (correction → structured rule) | **Demo-grade** | A keyword/Levenshtein switch (`internal/app/rules.go`), tied to the seeded verticals — not a general model. |
| Email / Telegram inbound adapters | **Partial** | Telegram *inbound* normalization is real and drives the e2e correction loop via `/gateway/inbound`; Telegram *outbound* is a no-op stub; there is no email-specific adapter (the generic `/ingest` endpoint stands in — you POST the normalized event). |
| Temporal / Hermes / MCP sidecars | **Local adapters / conceptual** | The MCP "write-final" path validates + audits but invokes no tool; the tool manifest is static. |

---

## Known limitations (and the reasoning)

**1. Per-tenant auth is intentionally spoofable.**
`tenantAuth` trusts any `Bearer tid:<tenant_id>` verbatim — no secret, signature,
or expiry (`internal/httpapi/server.go`). Anyone who can reach the port can act
as any tenant. This is a *sandbox identity scheme* that assumes a trusted
authenticating gateway in front. The privileged `/admin/*` and `/gateway/inbound`
surfaces **are** properly protected (default-deny, constant-time shared secrets),
so the dangerous routes fail closed; the per-tenant routes are the deliberate
placeholder. → *Fix:* signed/verified tokens (JWT or mTLS-derived identity)
before anything is exposed.

**2. The correction operation isn't atomic.**
`StartCase` and `persistCorrection` each issue 5+ independent store writes with
no enclosing transaction. A crash or DB blip mid-sequence can leave partial state
(a case with no delivered packet; a correction recorded but no candidate updated)
with nothing to reconcile it. → *Fix:* wrap each business operation in one
transaction, or an outbox for the deliver step.

**3. The learning loop has a write race.**
`mergeCandidate` is a read-modify-write (`ListCandidatesByConditions` then
`SaveCandidate`) with no row lock and no uniqueness on `(tenant, conditions_hash,
action)`. Correction-level idempotency already prevents the *same* correction
from merging twice (the `CreateCorrection` upsert is unique on the idempotency
key, and `persistCorrection` returns early on a duplicate); the residual race is
between *distinct* concurrent corrections for the same `conditions_hash`, which
can lose evidence or duplicate candidates. → *Fix:* `SELECT … FOR UPDATE` + a
unique constraint on `(tenant, conditions_hash, action)`.

**4. One missing clone in the in-memory store.**
The in-memory store deep-clones on read almost everywhere (case runs, candidates,
decision points, artifacts, audit events) — but channel bindings are the lone
exception: they're returned by value, sharing their slice backing arrays
(`CommandIdentities`/`CustomerAllowlist`), so concurrent mutation can race. The
Postgres store avoids it because it round-trips through JSON. → *Fix:* a
`cloneBinding` on read, matching the cloning the rest of the store already does.

**5. Rule extraction is demo-grade.**
The marquee claim — "each correction becomes structured operating logic" — holds
only for a handful of hard-coded phrases in the seeded verticals. The DeepSeek
agent makes the *draft* real, but the correction→rule *translation* is still a
keyword switch. → *Fix:* have the agent emit the structured rule (conditions +
action) from the correction, validated against a schema.

**6. Tenant isolation is a single shared boundary.**
Cross-tenant safety rests entirely on correct `WHERE tenant_id` predicates — no
defense in depth. The design spec calls for DB-per-tenant + RLS; the
implementation provides neither. → *Fix:* Postgres RLS as the floor; DB/schema
separation for real isolation.

**7. Secrets at rest & operational controls.**
WhatsApp identity/session keys are persisted to Postgres in plaintext (whatsmeow
`sqlstore`); there is no rate limiting on any auth path; there is no retention/
deletion story for stored customer message bodies beyond the PRIV-2 sweeper. →
*Fix:* envelope-encrypt WhatsApp key material; add gateway rate limiting; define
retention.

**8. Conflict handling is detect-only.**
Contradiction is detected and surfaced, but `PromoteCandidate` has no engine
guard against promoting a contradicted candidate, and there is no `/reject`
endpoint or review console. → *Fix:* engine-enforce the guard; add a reject path.

---

## What I'd do next (if this became a product), in order

1. **Real tenant auth** (signed tokens) + Postgres RLS — nothing ships exposed without these.
2. **Transactional integrity** — wrap `StartCase` / `persistCorrection`; lock the candidate merge.
3. **Agent-driven rule extraction** — turn corrections into validated structured rules, replacing the keyword switch.
4. **Encrypt WhatsApp key material at rest** + rate limiting + a retention policy.
5. **A real Rule Review Console** — promote/reject/supersede over an API, instead of `psql`.
6. **Honest channel adapters** — a real email (IMAP/SMTP) and Telegram outbound adapter, or drop the claim.

None of these are hard individually; together they're the "many extra miles"
between a harness and a beta. The point of this repo is the harness.

---

## What I'm glad I built well

- **Layering with consumer-defined seams** — `Store`, `channel.Adapter`, and the
  optional `DecisionAgent` interface, each defined where it's used; `domain`
  imports nothing.
- **A statistically honest promotion gate** — Wilson lower bound + recency,
  rather than a naive "3 corrections = a rule" counter.
- **A compile-time safety boundary** — never-in-prod routes are absent from the
  binary, not just env-gated, with a test that proves it.
- **A cost-aware agent** — the LLM is consulted only at unlearned decision points,
  so the correction loop *reduces* spend as the system learns.
- **A genuinely zero-dependency test suite** — the whole engine and HTTP surface
  run and are tested with no external services.
