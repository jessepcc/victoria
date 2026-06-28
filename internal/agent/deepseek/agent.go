// Package deepseek implements app.DecisionAgent against DeepSeek's
// OpenAI-compatible chat-completions API using only the Go standard library.
//
// Two properties are deliberate:
//
//   - No third-party dependency. The engine's zero-dependency build and offline
//     `go test ./...` path is unaffected; this package adds nothing to go.mod.
//   - Off unless configured. New returns nil when no API key is present, which
//     is the engine's signal to stay on its deterministic drafting path, so the
//     network is only reached at runtime when a DEEPSEEK_API_KEY is set.
//
// The agent asks DeepSeek to play Victoria: read a customer enquiry, extract the
// salient facts, pick the best next action from a closed allow-list, and write a
// short customer-ready draft — i.e. produce the concrete, imperfect run the
// operator then corrects from their phone.
package deepseek

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jessepcc/victoria/internal/app"
	"github.com/jessepcc/victoria/internal/domain"
)

const (
	// DefaultBaseURL is DeepSeek's OpenAI-compatible API root. Override with
	// VICTORIA_AGENT_BASE_URL (e.g. a gateway or a self-hosted endpoint).
	DefaultBaseURL = "https://api.deepseek.com"
	// DefaultModel is the configured DeepSeek model. Override with
	// VICTORIA_AGENT_MODEL if your account exposes a different id.
	DefaultModel = "deepseek-v4-pro"

	defaultTimeout = 30 * time.Second
	maxResponse    = 1 << 20 // 1 MiB cap on the response body we read
)

// Config configures a DeepSeek-backed agent. APIKey is the only required field.
type Config struct {
	APIKey     string
	BaseURL    string        // default DefaultBaseURL
	Model      string        // default DefaultModel
	Timeout    time.Duration // default defaultTimeout
	HTTPClient *http.Client  // default http.Client{Timeout: Timeout} — injectable for tests
}

// Agent is a DeepSeek-backed app.DecisionAgent.
type Agent struct {
	apiKey  string
	baseURL string
	model   string
	client  *http.Client
}

var _ app.DecisionAgent = (*Agent)(nil)

// New returns a DeepSeek agent, or nil if no API key is configured. A nil agent
// is intentional and meaningful: callers must nil-check before wiring it (a
// typed-nil interface value would defeat the engine's deterministic fallback).
func New(cfg Config) *Agent {
	key := strings.TrimSpace(cfg.APIKey)
	if key == "" {
		return nil
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = DefaultBaseURL
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = DefaultModel
	}
	client := cfg.HTTPClient
	if client == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = defaultTimeout
		}
		client = &http.Client{Timeout: timeout}
	}
	return &Agent{apiKey: key, baseURL: base, model: model, client: client}
}

// NewFromEnv builds an agent from DEEPSEEK_API_KEY (+ optional
// VICTORIA_AGENT_BASE_URL / VICTORIA_AGENT_MODEL), returning nil when the key is
// unset so wiring can be unconditional.
func NewFromEnv() *Agent {
	return New(Config{
		APIKey:  os.Getenv("DEEPSEEK_API_KEY"),
		BaseURL: os.Getenv("VICTORIA_AGENT_BASE_URL"),
		Model:   os.Getenv("VICTORIA_AGENT_MODEL"),
	})
}

// Model reports the configured model id (for logging).
func (a *Agent) Model() string { return a.model }

const systemPrompt = "You are Victoria, an autonomous back-office assistant for a small business. " +
	"You are running a DRAFT of the operator's real workflow against an isolated sandbox so the operator " +
	"can review and correct it from their phone. Given a customer enquiry and the workflow context you must: " +
	"(1) extract the salient facts from the enquiry; " +
	"(2) choose the single best next action, which MUST be exactly one of the allowed actions; " +
	"(3) write a short, friendly, customer-ready draft reply (under 60 words); and " +
	"(4) give a one-line rationale for the operator. " +
	"Respond with ONLY a single JSON object, no prose, in this exact shape: " +
	`{"facts":[{"label":"string","value":"string","confidence":0.0}],` +
	`"proposed_action":"string","draft_reply":"string","reasoning":"string"}.`

// Propose implements app.DecisionAgent.
func (a *Agent) Propose(ctx context.Context, req app.AgentRequest) (app.AgentResult, error) {
	payload, err := json.Marshal(chatRequest{
		Model: a.model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt(req)},
		},
		ResponseFormat: &responseFormat{Type: "json_object"},
		Stream:         false,
	})
	if err != nil {
		return app.AgentResult{}, fmt.Errorf("deepseek: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return app.AgentResult{}, fmt.Errorf("deepseek: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+a.apiKey)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return app.AgentResult{}, fmt.Errorf("deepseek: request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return app.AgentResult{}, fmt.Errorf("deepseek: read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return app.AgentResult{}, fmt.Errorf("deepseek: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return app.AgentResult{}, fmt.Errorf("deepseek: decode response: %w", err)
	}
	if cr.Error != nil {
		return app.AgentResult{}, fmt.Errorf("deepseek: api error: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 || strings.TrimSpace(cr.Choices[0].Message.Content) == "" {
		return app.AgentResult{}, fmt.Errorf("deepseek: empty completion")
	}
	var p proposalJSON
	if err := json.Unmarshal([]byte(cr.Choices[0].Message.Content), &p); err != nil {
		return app.AgentResult{}, fmt.Errorf("deepseek: parse model JSON: %w", err)
	}
	return toResult(p, req), nil
}

func userPrompt(req app.AgentRequest) string {
	payload, _ := json.MarshalIndent(req.Payload, "", "  ")
	var b strings.Builder
	fmt.Fprintf(&b, "Workflow: %s\n", req.WorkflowSlug)
	fmt.Fprintf(&b, "Decision type: %s\n", req.DecisionType)
	fmt.Fprintf(&b, "Allowed actions (choose exactly one): %s\n", strings.Join(req.AllowedActions, ", "))
	fmt.Fprintf(&b, "Default action if you are unsure: %s\n\n", req.DefaultAction)
	fmt.Fprintf(&b, "Customer case input (JSON):\n%s\n\n", string(payload))
	b.WriteString("Return the JSON object now.")
	return b.String()
}

// toResult maps the model's JSON onto an app.AgentResult, defensively: an action
// outside the allow-list falls back to the deterministic default, and
// confidences are clamped to [0,1]. A misbehaving model can never inject an
// unknown action into the loop.
func toResult(p proposalJSON, req app.AgentRequest) app.AgentResult {
	action := strings.TrimSpace(p.ProposedAction)
	if !contains(req.AllowedActions, action) {
		action = req.DefaultAction
	}
	facts := make([]domain.Fact, 0, len(p.Facts))
	for _, f := range p.Facts {
		label := strings.TrimSpace(f.Label)
		if label == "" {
			continue
		}
		conf := f.Confidence
		switch {
		case conf <= 0:
			conf = 0.9
		case conf > 1:
			conf = 1
		}
		facts = append(facts, domain.Fact{Label: label, Value: strings.TrimSpace(f.Value), Confidence: conf})
	}
	return app.AgentResult{
		Facts:          facts,
		ProposedAction: action,
		DraftReply:     strings.TrimSpace(p.DraftReply),
		Reasoning:      strings.TrimSpace(p.Reasoning),
	}
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

// --- OpenAI-compatible wire types (DeepSeek speaks this dialect) ---

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []chatMessage   `json:"messages"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
	Stream         bool            `json:"stream"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

type proposalJSON struct {
	Facts []struct {
		Label      string  `json:"label"`
		Value      string  `json:"value"`
		Confidence float64 `json:"confidence"`
	} `json:"facts"`
	ProposedAction string `json:"proposed_action"`
	DraftReply     string `json:"draft_reply"`
	Reasoning      string `json:"reasoning"`
}
