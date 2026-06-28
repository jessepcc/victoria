# Security Policy

## Reporting a vulnerability

Please report security issues **privately** so they can be fixed before public
disclosure. Use GitHub's private vulnerability reporting — open
**[Security → Report a vulnerability](https://github.com/jessepcc/victoria/security/advisories/new)**
on this repository — and include:

- a description of the issue and its impact,
- steps to reproduce (a proof of concept if you have one), and
- any suggested remediation.

You can expect an acknowledgement within a few business days. Please do not file
public GitHub issues for vulnerabilities.

## Security model and current posture

Victoria is pre-1.0 software under active development. The threat model is
documented here so that operators and contributors understand the guarantees —
and the explicit, intentional limitations — of the current implementation.

### Authentication & tenancy

- **Tenant context** is carried by `Authorization: Bearer tid:<tenant_id>`. This
  is a **development/sandbox identity scheme**, not a cryptographic credential —
  it is trivially spoofable and is intended to run behind a trusted gateway that
  performs real authentication and rewrites this header. **Do not expose the
  Victoria API directly to untrusted networks.** Replacing this with signed
  tokens (e.g. JWT) is tracked for the control-plane hardening milestone.
- **Tenant isolation** is enforced in the data layer: every store query is scoped
  by `tenant_id`. Cross-tenant reads are a class of bug we treat as
  high-severity — please report any you find.
- The **`/gateway/inbound`** webhook is authenticated by a shared secret
  (`VICTORIA_GATEWAY_INBOUND_TOKEN`, required at startup). Rotate it like any
  other secret.
- The privileged **`/admin/*`** control-plane routes (tenant provisioning,
  candidate promotion, replays, command-secret reissue, audit reads) are gated
  by a separate shared secret (`VICTORIA_ADMIN_TOKEN`, required at startup,
  constant-time compared). They are **default-deny**: with no token configured
  every admin route returns 503.

### Demo / debug endpoints

`/admin/dev/*` routes exist **only** in binaries built with `-tags dev`. A
production build (the default) does not compile them in at all, and
`test/e2e/http_e2e_proddev_test.go` asserts their absence. Never ship a
`-tags dev` binary to production.

### Messaging channels & privacy

- WhatsApp pairing is gated on a recorded **consent** step and a mode selection
  before any session is established.
- For non-allowlisted senders, decryption-key material is purged on a bounded
  cadence (the PRIV-2 retention sweeper, ~15 min) to limit the worst-case
  retention window.

### Secrets

All secrets (database DSN, gateway token, channel credentials) are supplied via
environment variables and are never committed. Avoid logging secret values or
customer PII at non-debug levels.

## Supported versions

As pre-1.0 software, only the `main` branch receives security fixes.
