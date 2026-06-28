package deepseek

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jessepcc/victoria/internal/app"
)

func sampleRequest() app.AgentRequest {
	return app.AgentRequest{
		WorkflowSlug:   "enquiry_triage",
		DecisionType:   "route_or_reply",
		DefaultAction:  "draft_reply",
		AllowedActions: []string{"draft_reply", "hold_and_request_more_info", "use_corporate_template"},
		Payload:        map[string]any{"from": "jess@acme.com", "body_text": "Do you do commercial roofs?"},
	}
}

// fakeServer returns an httptest server that asserts the request shape and
// replies with an OpenAI-compatible body whose message content is modelJSON.
func fakeServer(t *testing.T, modelJSON string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %s, want /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("auth header = %q, want Bearer test-key", got)
		}
		body, _ := io.ReadAll(r.Body)
		var req chatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("request body not valid json: %v", err)
		}
		if req.Model != "deepseek-v4-pro" {
			t.Errorf("model = %q, want deepseek-v4-pro", req.Model)
		}
		if req.ResponseFormat == nil || req.ResponseFormat.Type != "json_object" {
			t.Errorf("response_format = %+v, want json_object", req.ResponseFormat)
		}
		if len(req.Messages) != 2 || req.Messages[0].Role != "system" {
			t.Errorf("messages = %+v, want [system,user]", req.Messages)
		}
		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": modelJSON}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestProposeMapsModelOutput(t *testing.T) {
	srv := fakeServer(t, `{"facts":[{"label":"intent","value":"commercial","confidence":0.8},{"label":"","value":"skip me","confidence":1}],"proposed_action":"use_corporate_template","draft_reply":"Hi! Yes, we handle commercial roofs — I'll get our corporate team to follow up.","reasoning":"Commercial lead → corporate template"}`)
	defer srv.Close()

	agent := New(Config{APIKey: "test-key", BaseURL: srv.URL, HTTPClient: srv.Client()})
	if agent == nil {
		t.Fatal("New returned nil for a configured key")
	}
	res, err := agent.Propose(context.Background(), sampleRequest())
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if res.ProposedAction != "use_corporate_template" {
		t.Errorf("action = %q, want use_corporate_template", res.ProposedAction)
	}
	if !strings.Contains(res.DraftReply, "commercial roofs") {
		t.Errorf("draft = %q, want a real draft", res.DraftReply)
	}
	if res.Reasoning == "" {
		t.Error("reasoning is empty")
	}
	if len(res.Facts) != 1 { // the empty-label fact must be dropped
		t.Fatalf("facts = %d, want 1 (blank-label fact dropped)", len(res.Facts))
	}
	if res.Facts[0].Label != "intent" || res.Facts[0].Confidence != 0.8 {
		t.Errorf("fact = %+v, want intent/0.8", res.Facts[0])
	}
}

func TestProposeRejectsOutOfBandAction(t *testing.T) {
	// The model returns an action that is NOT in the allow-list; the agent must
	// fall back to the deterministic default rather than propagate it.
	srv := fakeServer(t, `{"facts":[],"proposed_action":"delete_everything","draft_reply":"x","reasoning":"y"}`)
	defer srv.Close()

	agent := New(Config{APIKey: "test-key", BaseURL: srv.URL, HTTPClient: srv.Client()})
	res, err := agent.Propose(context.Background(), sampleRequest())
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if res.ProposedAction != "draft_reply" {
		t.Errorf("action = %q, want fallback draft_reply", res.ProposedAction)
	}
}

func TestProposeErrorsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"bad key"}}`)
	}))
	defer srv.Close()

	agent := New(Config{APIKey: "test-key", BaseURL: srv.URL, HTTPClient: srv.Client()})
	if _, err := agent.Propose(context.Background(), sampleRequest()); err == nil {
		t.Fatal("expected an error on HTTP 401")
	}
}

func TestNewReturnsNilWithoutKey(t *testing.T) {
	if New(Config{APIKey: ""}) != nil {
		t.Error("New(empty key) should return nil so the engine keeps its deterministic fallback")
	}
	if New(Config{APIKey: "   "}) != nil {
		t.Error("New(whitespace key) should return nil")
	}
}

func TestNewDefaults(t *testing.T) {
	agent := New(Config{APIKey: "k"})
	if agent.Model() != DefaultModel {
		t.Errorf("model = %q, want %q", agent.Model(), DefaultModel)
	}
	if agent.baseURL != DefaultBaseURL {
		t.Errorf("baseURL = %q, want %q", agent.baseURL, DefaultBaseURL)
	}
}
