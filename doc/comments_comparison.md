# Advisor Comments Comparison

## ICP / Target User

| Advisor 1 | Advisor 2 | Advisor 3 |
|---|---|---|
| Rewrite the Target User section around: trades and field-service owner-operators who run their business from their phone — calls, texts, email, photos, and maybe a pocket notebook. They don't use SaaS workflow tools and won't start. Examples: plumbers, electricians, builders, landscapers, mobile mechanics, cleaning services. The key qualifier is that their "system" is already messaging + email + memory, not that they've tried and abandoned tools. | Primary ICP: owner-operators of small service businesses who run the business mostly from their phone, calls, messages, email, and ad hoc notes, and who are not SaaS-native or process-tool-heavy. | Narrow the "Primary User" section to specifically describe the "Deskless Solo Operator." |
| | Best examples: tradespeople, field-service operators, solo contractors, and very small teams where the owner is still in the loop on quoting, scheduling, and invoicing. | Explicitly define them as operators who run 100k-1M+/yr businesses entirely via phone calls, SMS/iMessage, email, and pocket notepads. They view logging into a SaaS dashboard on a laptop as a chore they avoid until Sunday night. |
| | Functional traits: mobile-first, time-poor, reactive, artifact-heavy workflows, lots of exceptions, little appetite for dashboards or setup. | State explicitly that the goal is *not* to teach them to use a new app. The goal is to integrate into the apps they already live in. |
| | Anti-ICP: operations teams already using structured CRMs, workflow builders, project boards, or dedicated back-office staff. | Pick a single initial vertical for the first 10 users (e.g., Residential Trades: Roofers, Plumbers, Landscapers). |
| | Tighter ICP sentence: `Owner-operators of small service businesses who run quoting and customer communication from their phone, inbox, messaging apps, and informal notes, and who are not comfortable setting up or maintaining SaaS workflow tools.` | |

### Reviewed Comment
ICP: "Deskless Solo Operator."
Operators who run 100k-1M+/yr businesses entirely via phone calls, SMS/iMessage, email, and pocket notepads. They view logging into a SaaS dashboard on a laptop as a chore they avoid until Sunday night.

Functional traits: mobile-first, time-poor, reactive, artifact-heavy workflows, lots of exceptions, little appetite for dashboards or setup.

Anti-ICP: operations teams already using structured CRMs, workflow builders, project boards, or dedicated back-office staff.

## Competitive Landscape

| Advisor 1 | Advisor 2 | Advisor 3 |
|---|---|---|
| Name the real alternatives honestly: | Direct competitors: workflow builders and AI agent tools. | *(not addressed)* |
| Zapier/Make/n8n — requires the user to think in workflows (they won't) | Indirect competitors: VAs, manual admin help, existing quoting software, doing everything in inbox/chat. | |
| Lindy AI / Relevance AI — agent-based but still dashboard-first | The real comparison is often "owner does it themselves on the fly," not another software product. | |
| DIY Claude + Zapier — what mjsweet actually did; works but fragile, no learning loop, no correction structure | Your differentiation is strongest when framed as: `works inside their current behavior`, `requires correction, not configuration`, `learns business logic from real decisions` | |
| A VA or office manager — the true incumbent; expensive, inconsistent, but requires zero tech adoption | | |
| Explain why correction-first beats all of these for this specific user. | | |

### Reviewed Comment
consolidate both comments, no conflicts

## Problem / Pain Points

| Advisor 1 | Advisor 2 | Advisor 3 |
|---|---|---|
| *(not addressed separately)* | Make the current behavior concrete: "work comes in through calls, WhatsApp/Telegram, email, and photos; decisions are made on the move; business logic lives in the owner's head." | *(not addressed separately)* |
| | Spell out the pain more sharply: `slow response times`, `missed follow-ups`, `inconsistent quoting`, `knowledge trapped in the owner`, `no clean handoff to software` | |
| | Clarify why existing workflow tools fail for this ICP: `too much upfront setup`, `too much abstraction`, `requires process clarity they do not have time to articulate`, `does not fit phone-first behavior` | |

### Reviewed Comment
Add all problems/pain points to doc

## Solution / Value Proposition

| Advisor 1 | Advisor 2 | Advisor 3 |
|---|---|---|
| *(not addressed separately)* | Make the core promise even crisper: `We do not ask owners to design a workflow. We run a realistic draft of their workflow and let them correct it from their phone.` | Keep: The "Correction over Configuration" thesis, the `RuleCandidate` JSON schema, Temporal for stateful orchestration, the progressive trust model (Sandbox -> Autopilot). |
| | Emphasize that the product learns from concrete corrections on real-looking artifacts, not generic chat. | |
| | Add a short "why this, not that" section comparing: `manual assistant`, `workflow builder`, `generic AI chatbot`, `traditional RPA/ops tooling` | |
| | Be explicit about what the product is not: not a workflow builder, not an ops dashboard, not a Zapier-for-SMBs tool. | |

### Reviewed Comment

Both Advisor 2 and 3 are on the right track but does not precisly caught the gist. It could be better decribed as dry run of the real workflow, ie "handle request for quotation from a new customer", "appointment change", "monthend account close" that dummy data are run against a default workflow in a sandboxed envrionment, ask user for input if required in the workflow, input and output are presented to users. Then user can comment and point out what is wrong.

## Beachhead / Wedge Use Case

| Advisor 1 | Advisor 2 | Advisor 3 |
|---|---|---|
| Wedge use case: Pick one — quote/proposal drafting is the strongest candidate (clear ROI, artifact-heavy, aligns with mjsweet) | Choose one initial user + one workflow combo. | Pick a single initial vertical for the first 10 users (e.g., Residential Trades: Roofers, Plumbers, Landscapers). |
| | Best candidate: `small service-business owner-operator`, `quote/proposal drafting from inbound enquiry + photos` | |
| | Why this wedge is strong: `high value per successful run`, `artifacts are easy to preview`, `approval is natural`, `mobile review fits behavior`, `ties directly to revenue` | |

### Reviewed Comment

I would like to go creative here.

A) Things to specialized in when start a wedge case

- landing page
- default workflow
- domain specific knowledge (technical terms, business cycle, specific rules and regulations) as basis

B) Common across all

- sandbox env
- workflow execution core engine

The effort of A & B should be 20%-80%. Let's build a solid B, then make good use of LLM to help build A

Then it would become a distribution problem, can try different landing addressing the different domain, draw traffic from the specific community. 

The domain/vertical of the user can be obtained from 1) landing page referral 2) a closed-end question in onboarding


## Go-to-Market

| Advisor 1 | Advisor 2 | Advisor 3 |
|---|---|---|
| First channel: Where these owners already congregate (trade forums, HN-adjacent communities, Facebook groups for tradies, Xero/MYOB partner networks) | First channel: likely founder-led sales into service-business networks, operator communities, and referrals. | *(not addressed)* |
| Acquisition motion: Concierge-first for the first 5-10, then case-study-driven word of mouth | First wedge message: `reply to enquiries and draft quotes from your phone without setting up a system` | |
| Pricing signal: Even a hypothesis (e.g., "$X/month replaces Y hours of admin") | Clarify why this segment is reachable: `concentrated local/service niches`, `clear pain`, `fast ROI`, `low need for broad org buy-in` | |

### Reviewed Comment
consolidate both comments, they refer to the same thing

## Why Now

| Advisor 1 | Advisor 2 | Advisor 3 |
|---|---|---|
| LLMs are now good enough at tool use to run realistic workflow drafts | *(not addressed separately)* | *(not addressed)* |
| MCP standardizes tool integration (no more bespoke API glue per service) | | |
| Post-COVID comfort with chat-as-business-tool among non-tech users | | |
| Cost of LLM inference has dropped enough to make per-small-business economics viable | | |

### Reviewed Comment
do not need this section

## Evidence / Validation

| Advisor 1 | Advisor 2 | Advisor 3 |
|---|---|---|
| Add a Validation Plan before the Development Plan. Before Phase 0, insert a pre-build validation phase: | Add 3-5 real anecdotes or interview findings, even if informal. | *(not addressed separately)* |
| 10 interviews with target ICP | Add proof points like: `how many quotes per week`, `how long quoting takes today`, `how often more info is needed`, `how often follow-up is delayed because the owner is mobile` | |
| 2-3 concierge pilots (you act as the agent manually) | Add one statement about behavior change threshold: `the user will adopt this if it saves time immediately without forcing them into a new operating system` | |
| Confirm: Do they engage with correction packets? Do corrections yield reusable rules? Will they pay, and how much? | Add stronger proof that this problem is real and painful, not just plausible. | |

### Reviewed Comment

skip this, we are going lottery machine mode. The need is validated, a real pain killer UI is yet to be merged in market

## Onboarding / Cold Start

| Advisor 1 | Advisor 2 | Advisor 3 |
|---|---|---|
| *(not addressed)* | *(not addressed separately)* | Add a "Zero to First Value" onboarding section. |
| | | Non-tech-savvy users cannot manage OAuth, API keys, or MCP server configurations. |
| | | White-Glove Start: For the MVP, onboarding consists of them forwarding an email or adding a phone number to their iMessage/WhatsApp group. |
| | | Pre-Seeded Logic: The system must not start with a blank slate. If Joe is a roofer, Victoria loads the "Standard Roofing Template." The initial corrections are just fine-tuning, not teaching the agent what a roof is from scratch. |

### Reviewed Comment

skip this, info are covered in other parts

## UX Channel

| Advisor 1 | Advisor 2 | Advisor 3 |
|---|---|---|
| *(not addressed)* | *(not addressed separately)* | Deprioritize Telegram in the "System Components" and "UX" sections. |
| | | Developers, crypto operators, and founders use Telegram. Tradespeople and solo operators in North America/Europe use SMS (iMessage) or WhatsApp. |
| | | Change the messaging gateway MVP to Twilio SMS or WhatsApp Business. They need to be able to reply simply with: "Change the price to $450 and send it." |

### Reviewed Comment
Whatsapp as the main channel, target is to obtain a specific number for each client. Whatsapp business is not feasible at the demo/pilot stage.
Reference how openclaw and hermes mananged to do it.
https://cloud.tencent.com/developer/article/2639118
https://hermes-agent.nousresearch.com/docs/user-guide/messaging/whatsapp 
Use telegram as a dev channel as it is quicker to setup


## Product Definition / MVP Scope

| Advisor 1 | Advisor 2 | Advisor 3 |
|---|---|---|
| *(not addressed separately)* | Separate clearly: `MVP` vs `later` | *(not addressed separately)* |
| | MVP should likely be: `Telegram-first`, `one workflow only`, `sandbox artifacts only`, `approve/correct loop`, `candidate rules`, `human-reviewed promotion` | |
| | Explicitly cut from MVP: `multiple workflows`, `multiple channels`, `broad live integrations`, `autonomous execution beyond drafts` | |

### Reviewed Comment

MVP should include Whatsapp first, also include telegram channel,  `sandbox artifacts only`, `approve/correct loop`, `candidate rules`, `human-reviewed promotion` are ok, but multiple workflows should be available
Good, Explicitly cut from MVP: `multiple workflows`, `multiple channels`, `broad live integrations`, `autonomous execution beyond drafts`


## Metrics & Success Criteria

| Advisor 1 | Advisor 2 | Advisor 3 |
|---|---|---|
| North star metric: Something like "corrections per workflow run trending toward zero" (proves the system is learning) | North star: `% of target workflow runs that reach acceptable draft quality with lightweight phone review` | North Star Metric: "Autopilot Promotion Rate" — the percentage of workflow branches a user upgrades from *Approval Required* to *Autopilot*. This proves trust. |
| Phase 0 success criteria: e.g., 3 active tenants, first validated rule promoted within 2 weeks, operator responds to >80% of review packets | Launch metrics: `time to first useful draft`, `approval rate`, `correction rate by type`, `time saved per case`, `repeat usage per week`, `promotion rate from sandbox to approval-gated mode` | MVP Success Criteria: "5 active deskless operators, executing >20 operational loops per week, interacting entirely via SMS/WhatsApp, with a correction-to-approval ratio dropping over a 4-week period." |
| Leading indicators: Time-to-first-correction, correction-to-rule conversion rate, operator response latency | | |

### Reviewed Comment

North Star Metric: "Autopilot Promotion Rate" — the percentage of workflow branches a user upgrades from Approval Required to Autopilot. This proves trust.

Launch Success criteria: 3 active deskless operators, executing >10 operational loops per week, interacting entirely via WhatsApp, with a correction-to-approval ratio dropping over a 4-week period


## Risks / Assumptions

| Advisor 1 | Advisor 2 | Advisor 3 |
|---|---|---|
| Surface the demand assumption explicitly in Risks. Add a risk row: "Unvalidated demand — we believe this user will pay for structured automation but have not tested willingness to pay or price sensitivity." Mitigation: concierge pilot before building infrastructure. | Surface the biggest commercial assumptions, not just technical risks. | *(not addressed separately)* |
| | These users will trust sandbox runs enough to engage. | |
| | They will tolerate lightweight correction loops from phone. | |
| | Quote drafting is frequent enough to build habit. | |
| | Artifact previews are sufficient for confidence. | |
| | Internal rule review can bridge quality early on without destroying margins or speed. | |

### Reviewed Comment

Remove this part, does not provide useful information

## Architecture / Infrastructure

| Advisor 1 | Advisor 2 | Advisor 3 |
|---|---|---|
| Trim the architecture sections. Move the detailed infra topology, isolation tiers, deployment hardening, and tenant provisioning checklist into a separate technical design doc. Keep only the high-level split (shared control plane + per-client execution plane) and the logical architecture diagram in the product spec. Rebalance the spec from ~60/40 product/architecture to ~80/20. | *(not addressed separately)* | Rewrite the "Tenant Setup and Complete Isolation" section. You are over-engineering for a Seed-stage MVP and it will kill your speed. |
| | | Replace Physical Isolation: Delete the requirement for 1 VM, 1 dedicated Postgres DB, and dedicated containers per tenant for Phase 1. |
| | | Adopt Logical Isolation: Use a single shared Postgres cluster with Row-Level Security (RLS). Use a shared, stateless compute pool for the Temporal workers and agent runtime. |
| | | Runtime Secret Injection: Keep tenant secrets in a secure vault (like Doppler or AWS Secrets Manager) and inject them dynamically into the MCP tools only at the exact moment of workflow execution. |

### Reviewed Comment

ignore comments from advisor 3, complete isolation is essential

## State Management

| Advisor 1 | Advisor 2 | Advisor 3 |
|---|---|---|
| *(not addressed)* | *(not addressed)* | Clarify the relationship between Hermes (or the Agent) and the Database in the "Architecture" section. |
| | | Agent as a Stateless Worker: Make it explicitly clear that the Agent/Hermes local disk is *not* the source of truth for business rules. |
| | | DB as the Moat: The Postgres database holds the `ValidatedRule` data. Before a workflow runs, the Control Plane pulls the specific rules for that tenant and injects them into the Agent's system prompt or context window. The agent runs, finishes, and dies. |

### Reviewed Comment 

Ignore this section

## Document Structure

| Advisor 1 | Advisor 2 | Advisor 3 |
|---|---|---|
| *(implied in "Trim the architecture sections")* | Reframe the spec so it reads as a product/market doc first and a system design doc second. | Cut: Deep Hermes/Docker architecture specifics, "multi-tenant" vs "single-tenant" physical compute debates, and references to enterprise features like dedicated VPCs. |
| | Move technical architecture, isolation, and Hermes-specific sections later in the document. | |
| | Put the front of the spec in this order: `ICP`, `problem`, `why now`, `solution`, `beachhead workflow`, `value proposition`, `evidence`, `GTM`, `metrics`, `risks`, `architecture` | |

### Reviewed Comment

Adopt comments from advisor 2
