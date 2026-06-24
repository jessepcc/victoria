# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **Customer-inbound channels** (Beta): email/Telegram ingest (tier 00),
  read-only WhatsApp on the operator's existing number (A0), and dedicated-number
  WhatsApp end-to-end (A1, via `go.mau.fi/whatsmeow`).
- WhatsApp consent gating and customer JID allowlist endpoints prior to pairing.
- PRIV-2 retention sweeper bounding decryption-key retention for non-allowlisted
  senders.
- Build-tag-gated demo/debug endpoints (`/admin/dev/*`, `-tags dev` only), with
  an e2e test asserting their absence in production builds.
- Repository scaffolding for open-source readiness: license, contributing guide,
  security policy, code of conduct, CI, linting, container build, and an
  architecture overview.

### Security
- Authenticated the privileged `/admin/*` control plane with a default-deny,
  constant-time shared-secret check (`VICTORIA_ADMIN_TOKEN`, required at boot).
  Previously these routes were unauthenticated.
- Added panic-recovery middleware and a 1 MiB request-body cap on every JSON
  endpoint; set a server read timeout.
- Stopped logging WhatsApp sender JIDs and message bodies (PII) at info level.

### Fixed
- Graceful shutdown: the server now drains on SIGINT/SIGTERM, so deferred store
  and WhatsApp `Close()` calls actually run (previously skipped by `log.Fatal`).
- Customer-message ingest no longer permanently poisons an idempotency record
  when the first `StartCase` fails — re-delivery now reprocesses.
- Fixed a data race in WhatsApp `SendOutbound` (session fields are now snapshotted
  under the per-client lock) and a pairing-timeout client/goroutine leak.
- `removeString` no longer mutates the caller's (store-owned) slice in place.
- Operator "always"/"this case" scope intent on the chat path is now honored
  (read from free text) instead of being silently dropped.
- Postgres status/case-update methods are now transactional (`SELECT … FOR
  UPDATE`), closing a lost-update/TOCTOU window under concurrency; concurrent
  rule promotions can no longer both deprecate the same active rule.
- WhatsApp self-chat (A0) echoes are filtered by message ID, so a plain-text
  send is no longer mistaken for new operator input.
- The in-memory store's `ActiveSkillVersion` now matches Postgres exactly
  (dropped a `quote_drafting` fallback + slug rewrite that masked divergence).
- Added a `-tags dev` e2e test asserting the demo `/admin/dev/*` routes are
  mounted and functional — the complement of the production-build absence test.

### Changed
- Module path is now `github.com/jessepcc/victoria` (clone-able import path).
- Split the 2,100-line `internal/app/app.go` god file into cohesive peer files
  (`rules.go`, `helpers.go`); removed a dead duplicate free-text parser.

### Core capabilities (from prior milestones)
- Sandbox correction engine: tenant provisioning, sandbox/live case execution,
  review packets, gateway reply parsing, correction persistence, rule
  candidates, human promotion, immutable skill-version pinning, replay, and audit
  events.
- MCP write three-gate preflight (tenant binding, sandbox mode, approval audit).
- Pluggable persistence: in-memory and PostgreSQL stores behind a single
  `app.Store` interface.

[Unreleased]: https://github.com/jessepcc/victoria/commits/main
