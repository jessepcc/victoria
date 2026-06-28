package app

import (
	"context"
	"time"

	"github.com/jessepcc/victoria/internal/domain"
)

// agentTimeout bounds a single DecisionAgent call so a slow or hung agent can
// never wedge case execution; on timeout the engine falls back to deterministic
// drafting.
const agentTimeout = 30 * time.Second

// DecisionAgent is the engine's seam to a real reasoning agent that drafts a
// workflow run for the operator to review and correct. It is the "agent" the
// product thesis is built around: it produces a concrete, imperfect draft that
// the operator reacts to, rather than asking the operator to describe rules up
// front.
//
// The seam is OPTIONAL. When no agent is wired (the default — and always in
// tests), the engine falls back to its deterministic rule/template behaviour,
// so the zero-dependency build and test path never makes a network call. The
// interface is defined here, where it is consumed, per Go convention;
// implementations (e.g. internal/agent/deepseek) import only domain and this
// package and never the other way around.
type DecisionAgent interface {
	// Propose drafts the agent's view of a sandbox case: the facts it
	// extracted, the action it would take (constrained to AllowedActions), a
	// customer-facing draft reply, and a one-line rationale for the operator.
	//
	// Implementations MUST honour the context deadline and MUST return an error
	// rather than a partial guess on any failure — the caller treats any error
	// as "agent unavailable" and falls back to deterministic behaviour, so a
	// flaky or slow agent degrades gracefully instead of blocking the loop.
	Propose(ctx context.Context, req AgentRequest) (AgentResult, error)
}

// AgentRequest is the case context handed to the agent. It is deliberately a
// plain value (no store handles) so an implementation is trivial to test and
// can run out-of-process.
type AgentRequest struct {
	WorkflowSlug   string         // e.g. "enquiry_triage"
	DecisionType   string         // e.g. "route_or_reply"
	DefaultAction  string         // deterministic fallback if the agent is unsure
	AllowedActions []string       // the actions the agent may choose from
	Payload        map[string]any // the case input (customer message, facts, …)
}

// AgentResult is the agent's draft. ProposedAction must be one of the request's
// AllowedActions; the engine re-validates it and falls back to DefaultAction if
// it isn't, so a misbehaving agent can never push an out-of-band action into
// the loop.
type AgentResult struct {
	Facts          []domain.Fact // structured facts the agent extracted
	ProposedAction string        // one of AgentRequest.AllowedActions
	DraftReply     string        // customer-facing draft (may be empty)
	Reasoning      string        // one-line rationale shown to the operator
}

// WithAgent wires a DecisionAgent into the engine. Pass a non-nil agent only;
// to keep the deterministic fallback, simply do not call this option. (Callers
// that build an agent which may be absent should nil-check before wiring — a
// typed-nil interface value would defeat the fallback.)
func WithAgent(agent DecisionAgent) Option {
	return func(a *App) { a.agent = agent }
}
