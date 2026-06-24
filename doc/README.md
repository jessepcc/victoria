# Documentation

> **For external readers:** these are the design specs written during
> development, so they read as internal R&D artifacts and use internal section
> codes (e.g. `P0–P10`, `OQ-n`, `A1–A16`). If you're new to the project, start
> with the top-level [README](../README.md) and [ARCHITECTURE.md](../ARCHITECTURE.md)
> — then come here for depth.

## Reading Order

Start with the product spec, then the tech spec set in the order below.

### Product Spec

- **[sandbox-correction-env-product-spec.md](sandbox-correction-env-product-spec.md)** — Product thesis, ICP, correction loop design, data model, GTM, and development plan.

### Technical Spec

The overview recommends reading the component specs in dependency order, not numerical order:

1. **[00-overview.md](sandbox-tech-spec/00-overview.md)** — Navigation index across all five specs. Contains the 16-point architectural backbone (A1–A16), 8 end-to-end acceptance scenarios, recommended development entry-point order, and 6 gating integration tests.
2. **[03-correction-loop.md](sandbox-tech-spec/03-correction-loop.md)** — Canonical data model (Postgres DDL + JSON shapes) referenced by every other spec. Candidate matching, confidence scoring, rule versioning, audit event schema.
3. **[02-execution-plane.md](sandbox-tech-spec/02-execution-plane.md)** — Hermes runtime in Docker, Temporal workflow and activity contracts, sandbox MCP servers (email, drive, invoice), replay determinism.
4. **[04-operator-experience.md](sandbox-tech-spec/04-operator-experience.md)** — WhatsApp (whatsmeow) and Telegram gateway, review packet schema, correction action set, parser cascade, channel-binding tenant validation.
5. **[01-control-plane.md](sandbox-tech-spec/01-control-plane.md)** — API gateway (Go), tenant registry and provisioning, Rule Review Console, mTLS CA, observability, billing.
6. **[05-architecture-integration-critique.md](sandbox-tech-spec/05-architecture-integration-critique.md)** — Devil's advocate review. Cross-component contracts, isolation invariants, TDD audits, per-round scoring, and final SHIPPABLE sign-off.

### Working Notes

- **[comments.md](comments.md)** — Advisor feedback on the product spec.
- **[comments_comparison.md](comments_comparison.md)** — Cross-advisor comparison with reviewed responses.
