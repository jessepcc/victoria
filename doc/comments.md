# Comments from 3 advisors

## Advisor 1
Key Edits to Solidify the Spec
1. Sharpen the ICP (your direction is right)
Rewrite the Target User section around: trades and field-service owner-operators who run their business from their phone — calls, texts, email, photos, and maybe a pocket notebook. They don't use SaaS workflow tools and won't start. Examples: plumbers, electricians, builders, landscapers, mobile mechanics, cleaning services. The key qualifier is that their "system" is already messaging + email + memory, not that they've tried and abandoned tools.

2. Add a Competitive Landscape section
Name the real alternatives honestly:

Zapier/Make/n8n — requires the user to think in workflows (they won't)
Lindy AI / Relevance AI — agent-based but still dashboard-first
DIY Claude + Zapier — what mjsweet actually did; works but fragile, no learning loop, no correction structure
A VA or office manager — the true incumbent; expensive, inconsistent, but requires zero tech adoption
Explain why correction-first beats all of these for this specific user.

3. Add a Go-to-Market section
Cover at minimum:

First channel: Where these owners already congregate (trade forums, HN-adjacent communities, Facebook groups for tradies, Xero/MYOB partner networks)
Wedge use case: Pick one — quote/proposal drafting is the strongest candidate (clear ROI, artifact-heavy, aligns with mjsweet)
Acquisition motion: Concierge-first for the first 5-10, then case-study-driven word of mouth
Pricing signal: Even a hypothesis (e.g., "$X/month replaces Y hours of admin")
4. Add Metrics & Success Criteria
Define at minimum:

North star metric: Something like "corrections per workflow run trending toward zero" (proves the system is learning)
Phase 0 success criteria: e.g., 3 active tenants, first validated rule promoted within 2 weeks, operator responds to >80% of review packets
Leading indicators: Time-to-first-correction, correction-to-rule conversion rate, operator response latency
5. Add a "Why Now" section
Make explicit what's implicit:

LLMs are now good enough at tool use to run realistic workflow drafts
MCP standardizes tool integration (no more bespoke API glue per service)
Post-COVID comfort with chat-as-business-tool among non-tech users
Cost of LLM inference has dropped enough to make per-small-business economics viable
6. Trim the architecture sections
Move the detailed infra topology, isolation tiers, deployment hardening, and tenant provisioning checklist into a separate technical design doc. Keep only the high-level split (shared control plane + per-client execution plane) and the logical architecture diagram in the product spec. This rebalances the spec from ~60/40 product/architecture to ~80/20.

7. Surface the demand assumption explicitly in Risks
Add a risk row: "Unvalidated demand — we believe this user will pay for structured automation but have not tested willingness to pay or price sensitivity." Mitigation: concierge pilot before building infrastructure.

8. Add a Validation Plan before the Development Plan
Before Phase 0, insert a pre-build validation phase:

10 interviews with target ICP
2-3 concierge pilots (you act as the agent manually)
Confirm: Do they engage with correction packets? Do corrections yield reusable rules? Will they pay, and how much?
These eight edits would take the spec from "strong product intuition with premature architecture" to "validated thesis ready for engineering investment." The bones are good — it mostly needs market-facing muscle added and architecture weight shifted out.

## Advisor 2
**Core Edits To Lock In**
- Tighten the ICP to one very specific operator archetype, not “small businesses” broadly.
- Pick one beachhead workflow for v1. Based on the `mjsweet` case, `quote/proposal drafting` is the clearest first wedge.
- Reframe the spec so it reads as a product/market doc first and a system design doc second.
- Add stronger proof that this problem is real and painful, not just plausible.
- Be explicit about what the product is not: not a workflow builder, not an ops dashboard, not a Zapier-for-SMBs tool.
- Define measurable success criteria for onboarding, correction quality, and trust progression.
- Add a real GTM wedge: how we find the first users and why they will try this.
- Add competitive framing: what owners use today, why that is insufficient, and why this approach wins.
- Surface the biggest commercial assumptions, not just technical risks.

**ICP To Use**
- Primary ICP: owner-operators of small service businesses who run the business mostly from their phone, calls, messages, email, and ad hoc notes, and who are not SaaS-native or process-tool-heavy.
- Best examples: tradespeople, field-service operators, solo contractors, and very small teams where the owner is still in the loop on quoting, scheduling, and invoicing.
- Functional traits: mobile-first, time-poor, reactive, artifact-heavy workflows, lots of exceptions, little appetite for dashboards or setup.
- Anti-ICP: operations teams already using structured CRMs, workflow builders, project boards, or dedicated back-office staff.

A tighter ICP sentence you can use in the next draft:
`Owner-operators of small service businesses who run quoting and customer communication from their phone, inbox, messaging apps, and informal notes, and who are not comfortable setting up or maintaining SaaS workflow tools.`

**Problem Section Changes**
- Make the current behavior concrete: “work comes in through calls, WhatsApp/Telegram, email, and photos; decisions are made on the move; business logic lives in the owner’s head.”
- Spell out the pain more sharply:
  `slow response times`
  `missed follow-ups`
  `inconsistent quoting`
  `knowledge trapped in the owner`
  `no clean handoff to software`
- Clarify why existing workflow tools fail for this ICP:
  `too much upfront setup`
  `too much abstraction`
  `requires process clarity they do not have time to articulate`
  `does not fit phone-first behavior`

**Solution / Value Prop Changes**
- Make the core promise even crisper:
  `We do not ask owners to design a workflow. We run a realistic draft of their workflow and let them correct it from their phone.`
- Emphasize that the product learns from concrete corrections on real-looking artifacts, not generic chat.
- Add a short “why this, not that” section comparing:
  `manual assistant`
  `workflow builder`
  `generic AI chatbot`
  `traditional RPA/ops tooling`

**Beachhead Focus**
- Choose one initial user + one workflow combo.
- Best candidate:
  `small service-business owner-operator`
  `quote/proposal drafting from inbound enquiry + photos`
- Why this wedge is strong:
  `high value per successful run`
  `artifacts are easy to preview`
  `approval is natural`
  `mobile review fits behavior`
  `ties directly to revenue`

**Evidence To Add**
- Add 3-5 real anecdotes or interview findings, even if informal.
- Add proof points like:
  `how many quotes per week`
  `how long quoting takes today`
  `how often more info is needed`
  `how often follow-up is delayed because the owner is mobile`
- Add one statement about behavior change threshold:
  `the user will adopt this if it saves time immediately without forcing them into a new operating system`

**Product Definition Changes**
- Separate clearly:
  `MVP`
  `later`
- MVP should likely be:
  `Telegram-first`
  `one workflow only`
  `sandbox artifacts only`
  `approve/correct loop`
  `candidate rules`
  `human-reviewed promotion`
- Explicitly cut from MVP:
  `multiple workflows`
  `multiple channels`
  `broad live integrations`
  `autonomous execution beyond drafts`

**GTM Points To Define**
- First channel: likely founder-led sales into service-business networks, operator communities, and referrals.
- First wedge message:
  `reply to enquiries and draft quotes from your phone without setting up a system`
- Clarify why this segment is reachable:
  `concentrated local/service niches`
  `clear pain`
  `fast ROI`
  `low need for broad org buy-in`

**Competition Points To Add**
- Direct competitors: workflow builders and AI agent tools.
- Indirect competitors: VAs, manual admin help, existing quoting software, doing everything in inbox/chat.
- The real comparison is often “owner does it themselves on the fly,” not another software product.
- Your differentiation is strongest when framed as:
  `works inside their current behavior`
  `requires correction, not configuration`
  `learns business logic from real decisions`

**Assumptions To Make Explicit**
- These users will trust sandbox runs enough to engage.
- They will tolerate lightweight correction loops from phone.
- Quote drafting is frequent enough to build habit.
- Artifact previews are sufficient for confidence.
- Internal rule review can bridge quality early on without destroying margins or speed.

**Metrics To Define Before Next Draft**
- North star: `% of target workflow runs that reach acceptable draft quality with lightweight phone review`
- Launch metrics:
  `time to first useful draft`
  `approval rate`
  `correction rate by type`
  `time saved per case`
  `repeat usage per week`
  `promotion rate from sandbox to approval-gated mode`

**One Structural Recommendation**
- Move technical architecture, isolation, and Hermes-specific sections later in the document.
- Put the front of the spec in this order:
  `ICP`
  `problem`
  `why now`
  `solution`
  `beachhead workflow`
  `value proposition`
  `evidence`
  `GTM`
  `metrics`
  `risks`
  `architecture`

If you want, I can next turn this into a clean “revision checklist” with exact section-by-section prompts for the next spec draft, still without editing any files.


## Advisor 3

Here is the targeted list of edits to make to your product spec. Updating these areas will shift the document from a "theoretical architecture" to an "investable, executable MVP playbook."

### 1. Refine the ICP & Target User
**The Edit:** Narrow the "Primary User" section to specifically describe the **"Deskless Solo Operator."** 
*   **Add Context:** Explicitly define them as operators who run 100k–1M+/yr businesses entirely via phone calls, SMS/iMessage, email, and pocket notepads. They view logging into a SaaS dashboard on a laptop as a chore they avoid until Sunday night. 
*   **The Anti-Goal:** State explicitly that the goal is *not* to teach them to use a new app. The goal is to integrate into the apps they already live in.
*   **Specific Wedge:** Pick a single initial vertical for the first 10 users (e.g., Residential Trades: Roofers, Plumbers, Landscapers). 

### 2. Overhaul the Onboarding & "Cold Start" Flow
**The Edit:** Add a "Zero to First Value" onboarding section.
*   **Remove Friction:** Non-tech-savvy users cannot manage OAuth, API keys, or MCP server configurations. 
*   **White-Glove Start:** For the MVP, onboarding consists of them forwarding an email (e.g., `quotes@joesroofing.com` -> `joe@victoria-ops.com`) or adding a phone number to their iMessage/WhatsApp group with their subcontractors.
*   **Pre-Seeded Logic:** The system must not start with a blank slate. If Joe is a roofer, Victoria loads the "Standard Roofing Template" (knows what a square is, knows standard margin). The initial corrections are just fine-tuning, not teaching the agent what a roof is from scratch.

### 3. Change the Primary UX Channel
**The Edit:** Deprioritize Telegram in the "System Components" and "UX" sections.
*   **The Reality:** Developers, crypto operators, and founders use Telegram. Tradespeople and solo operators in North America/Europe use **SMS (iMessage) or WhatsApp**.
*   **Action:** Change the messaging gateway MVP to Twilio SMS or WhatsApp Business. It presents slightly more friction for you (formatting text vs. rich Telegram buttons), but it is native to your specific ICP. They need to be able to reply simply with: *"Change the price to $450 and send it."*

### 4. Downscope the Infrastructure (Logical vs. Physical Isolation)
**The Edit:** Rewrite the "Tenant Setup and Complete Isolation" section. You are over-engineering for a Seed-stage MVP and it will kill your speed.
*   **Replace Physical Isolation:** Delete the requirement for 1 VM, 1 dedicated Postgres DB, and dedicated containers per tenant for Phase 1. 
*   **Adopt Logical Isolation:** Use a **single shared Postgres cluster with Row-Level Security (RLS)**. Use a shared, stateless compute pool for the Temporal workers and agent runtime. 
*   **Runtime Secret Injection:** Keep tenant secrets in a secure vault (like Doppler or AWS Secrets Manager) and inject them dynamically into the MCP tools only at the exact moment of workflow execution.

### 5. Define the "System of Truth" (State Management)
**The Edit:** Clarify the relationship between Hermes (or the Agent) and the Database in the "Architecture" section.
*   **Agent as a Stateless Worker:** Make it explicitly clear that the Agent/Hermes local disk is *not* the source of truth for business rules. 
*   **DB as the Moat:** The Postgres database holds the `ValidatedRule` data. Before a workflow runs, the Control Plane pulls the specific rules for that tenant and injects them into the Agent's system prompt or context window. The agent runs, finishes, and dies. This prevents cross-contamination and makes debugging infinitely easier.

### 6. Add "Metrics & Success Criteria"
**The Edit:** Add a final section detailing how you measure product-market fit (PMF).
*   **North Star Metric:** **"Autopilot Promotion Rate"** — the percentage of workflow branches a user upgrades from *Approval Required* to *Autopilot*. This proves trust.
*   **MVP Success Criteria:** "5 active deskless operators, executing >20 operational loops (e.g., quotes drafted, invoices processed) per week, interacting entirely via SMS/WhatsApp, with a correction-to-approval ratio dropping over a 4-week period."

### Summary of what to Cut vs. Keep:
*   **Keep:** The "Correction over Configuration" thesis, the `RuleCandidate` JSON schema, Temporal for stateful orchestration, the progressive trust model (Sandbox -> Autopilot).
*   **Cut:** Deep Hermes/Docker architecture specifics, "multi-tenant" vs "single-tenant" physical compute debates, and references to enterprise features like dedicated VPCs. 

Make these edits, and the spec goes from a heavy engineering document to a lethal, highly investable startup playbook. Do you want to dive into drafting the SMS/WhatsApp UX flow next?