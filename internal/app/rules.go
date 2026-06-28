package app

import (
	"fmt"
	"strings"

	"github.com/jessepcc/victoria/internal/domain"
)

func defaultWorkflowTemplates(vertical string) []domain.WorkflowTemplate {
	return []domain.WorkflowTemplate{
		{ID: "wt_enquiry_" + vertical, Slug: "enquiry_triage", Vertical: vertical, DisplayName: "Enquiry triage", DecisionTypes: []string{"route_or_reply"}},
		{ID: "wt_quote_" + vertical, Slug: "quote_drafting", Vertical: vertical, DisplayName: "Quote drafting", DecisionTypes: []string{"send_or_hold"}},
		{ID: "wt_invoice_" + vertical, Slug: "invoice_handling", Vertical: vertical, DisplayName: "Invoice handling", DecisionTypes: []string{"tax_treatment"}},
	}
}

// allowedActions is the closed set of actions a DecisionAgent may choose from
// for a workflow. The engine re-validates the agent's choice against this list
// and falls back to the deterministic default if it strays, so the agent can
// never push an unknown action into the correction loop. The first entry is the
// conventional default for the workflow.
func allowedActions(workflowSlug string) []string {
	switch workflowSlug {
	case "invoice_handling":
		return []string{"apply_gst", "apply_no_gst", "hold_and_request_more_info"}
	case "enquiry_triage":
		return []string{"draft_reply", "hold_and_request_more_info", "use_corporate_template"}
	default: // quote_drafting
		return []string{"send_quote", "hold_and_request_more_info", "use_corporate_template"}
	}
}

func workflowDecision(workflowSlug string) (string, string) {
	switch workflowSlug {
	case "invoice_handling":
		return "tax_treatment", "apply_gst"
	case "enquiry_triage":
		return "route_or_reply", "draft_reply"
	default:
		return "send_or_hold", "send_quote"
	}
}

func workflowFromCaseInput(input map[string]any) string {
	if value, ok := input["workflow_slug"].(string); ok && value != "" {
		return value
	}
	return "quote_drafting"
}

func structuredRuleFromCorrection(c domain.Correction, point domain.DecisionPoint) ([]domain.Condition, string) {
	text := strings.ToLower(c.FreeText + " " + c.FollowUpAnswer)
	matchAny := func(keywords ...string) bool {
		for _, kw := range keywords {
			if containsOrFuzzy(text, kw) {
				return true
			}
		}
		return false
	}
	matchAll := func(keywords ...string) bool {
		for _, kw := range keywords {
			if !containsOrFuzzy(text, kw) {
				return false
			}
		}
		return true
	}
	switch {
	case matchAny("singapore", "no gst"):
		return []domain.Condition{{Field: "supplier_country", Operator: "!=", Value: "AU"}}, "apply_no_gst"
	case matchAll("commercial", "template"):
		return []domain.Condition{{Field: "enquiry_type", Operator: "=", Value: "commercial"}}, "use_corporate_template"
	case matchAny("send it anyway", "go ahead"):
		return []domain.Condition{
			{Field: "photos_complete", Operator: "=", Value: boolValue(point.AgentInput, "photos_complete")},
			{Field: "client_type", Operator: "=", Value: stringValue(point.AgentInput, "client_type", "new")},
		}, "send_quote"
	case matchAny("repeat", "known client"):
		return []domain.Condition{
			{Field: "photos_complete", Operator: "=", Value: boolValue(point.AgentInput, "photos_complete")},
			{Field: "client_type", Operator: "=", Value: "repeat"},
		}, "send_quote"
	case matchAny("photo", "hold", "more info"):
		return []domain.Condition{
			{Field: "photos_complete", Operator: "=", Value: false},
			{Field: "client_type", Operator: "=", Value: stringValue(point.AgentInput, "client_type", "new")},
		}, "hold_and_request_more_info"
	default:
		if c.ActionButton == domain.ActionUseDifferentTemplate {
			return []domain.Condition{{Field: "enquiry_type", Operator: "=", Value: stringValue(point.AgentInput, "enquiry_type", "commercial")}}, "use_corporate_template"
		}
		return nil, ""
	}
}

// containsOrFuzzy returns true if text contains keyword as a substring, OR
// any whitespace-separated token in text is within Levenshtein-2 of the
// keyword. The fuzzy fallback is the typo-forgiving layer (operators are
// often one-thumb typing on phones — "phtots" should still match "photos").
//
// Floors:
//   - keyword must be ≥4 chars (avoids matching short tokens like "no")
//   - token must be ≥4 chars (same reason, applied symmetrically)
//   - multi-word keywords ("no gst") match exact substring only
func containsOrFuzzy(text, keyword string) bool {
	if strings.Contains(text, keyword) {
		return true
	}
	if len(keyword) < 4 || strings.Contains(keyword, " ") {
		return false
	}
	for _, tok := range strings.Fields(text) {
		tok = strings.Trim(tok, ".,!?;:'\"")
		if len(tok) < 4 {
			continue
		}
		if levenshtein(tok, keyword) <= 2 {
			return true
		}
	}
	return false
}

// levenshtein returns the edit distance between two ASCII strings (insert,
// delete, substitute — no transpose). Pure Go, allocation-light. Used by
// the correction parser's typo-forgiving layer.
func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min3(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}

func conditionsMatch(payload map[string]any, conditions []domain.Condition) bool {
	for _, condition := range conditions {
		actual, ok := payload[condition.Field]
		switch condition.Operator {
		case "=":
			if !ok || fmt.Sprint(actual) != fmt.Sprint(condition.Value) {
				return false
			}
		case "!=":
			if ok && fmt.Sprint(actual) == fmt.Sprint(condition.Value) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func redactAggregateConditions(conditions []domain.Condition) ([]domain.Condition, bool) {
	out := make([]domain.Condition, 0, len(conditions))
	quarantine := false
	for _, condition := range conditions {
		redacted := condition
		value := fmt.Sprint(condition.Value)
		lowerField := strings.ToLower(condition.Field)
		switch {
		case strings.Contains(value, "@"):
			redacted.Value = "<email>"
		case strings.Contains(lowerField, "client_name") || strings.Contains(lowerField, "customer_name"):
			redacted.Value = "<quarantined:freetext>"
			quarantine = true
		case looksLikeFreeText(value):
			redacted.Value = "<quarantined:freetext>"
			quarantine = true
		}
		out = append(out, redacted)
	}
	return out, quarantine
}

func looksLikeFreeText(value string) bool {
	if value == "" {
		return false
	}
	allowed := map[string]struct{}{
		"true": {}, "false": {}, "new": {}, "repeat": {}, "commercial": {}, "residential": {}, "AU": {}, "SG": {},
	}
	if _, ok := allowed[value]; ok {
		return false
	}
	return strings.Contains(value, " ")
}
