package app

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jessepcc/victoria/internal/channel"
	"github.com/jessepcc/victoria/internal/domain"
)

func factsFromPayload(payload map[string]any) []domain.Fact {
	keys := make([]string, 0, len(payload))
	for key := range payload {
		if key != "sandbox" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	facts := make([]domain.Fact, 0, len(keys))
	for _, key := range keys {
		facts = append(facts, domain.Fact{Label: key, Value: fmt.Sprint(payload[key]), Confidence: 1})
	}
	return facts
}

func payloadFromIngestionEvent(event IngestionEvent) map[string]any {
	payload := map[string]any{
		"sandbox":             true,
		"source_channel":      event.Channel,
		"source_message_id":   event.SourceMessageID,
		"customer_identifier": event.CustomerIdentifier,
		"received_at":         event.ReceivedAt.UTC().Format(time.RFC3339),
		"body_text":           event.BodyText,
		"message_excerpt":     excerpt(event.BodyText, 180),
		"from":                event.CustomerIdentifier,
	}
	if strings.TrimSpace(event.Subject) != "" {
		payload["subject"] = strings.TrimSpace(event.Subject)
	}
	if len(event.Metadata) > 0 {
		payload["metadata"] = cloneMap(event.Metadata)
	}
	return payload
}

func draftBodyForRun(run domain.CaseRun) string {
	customer := stringValue(run.InputPayload, "customer_identifier", "there")
	switch run.WorkflowSlug {
	case "quote_drafting":
		return fmt.Sprintf("Hi! Thanks for reaching out. We can help with that quote, %s. Could you send 2-3 photos so we can size it correctly?", customer)
	default:
		body := stringValue(run.InputPayload, "body_text", "")
		if body == "" {
			return "Hi! Thanks for reaching out. We'll review this and get back to you shortly."
		}
		return "Hi! Thanks for reaching out. We'll review your message and get back to you shortly."
	}
}

// pickCustomerLabel chooses the most human-friendly identifier for the
// customer in operator-facing copy: prefer customer_name, fall back to
// customer_identifier (raw JID/email/etc.), then a generic placeholder.
func pickCustomerLabel(payload map[string]any) string {
	for _, key := range []string{"customer_name", "customer_identifier"} {
		if v := stringValue(payload, key, ""); v != "" {
			return v
		}
	}
	return "the customer"
}

// draftBodyForAction returns a forwardable customer-facing message tied to a
// specific recommended action. Used by the correction loop to give the
// operator a forward-ready draft after they correct Victoria — closing the
// immediate customer-reply loop alongside the rule-learning loop. Empty
// return means "no customer-facing draft applies for this action" (e.g. tax
// decisions where the operator just files the invoice internally).
func draftBodyForAction(action string, _ map[string]any) string {
	switch action {
	case "hold_and_request_more_info":
		return "Hi! Thanks for reaching out. Before I prepare a quote, could you send 2-3 photos so I can size it correctly?"
	case "send_quote":
		return "Hi! Thanks for reaching out, we can help with that. I'll send across our quote shortly."
	case "use_corporate_template":
		return "Hi! Thanks for reaching out — I'll have our corporate-accounts team get back to you with the right pack."
	default:
		return ""
	}
}

// followupPromptFor is the operator-facing prompt Victoria sends after a
// correction button is tapped, asking for the detail that becomes the rule.
func followupPromptFor(action domain.ActionButton) string {
	switch action {
	case domain.ActionWrongFacts:
		return "Got it — which fact is wrong, and what should it be?"
	case domain.ActionMissingCondition:
		return "Got it — what condition did I miss? Just type it."
	case domain.ActionUseDifferentTemplate:
		return "Got it — which template should I use instead?"
	case domain.ActionAddNote:
		return "Sure — what note should I add?"
	default:
		return "Got it — what should I have done instead? Just type the reason."
	}
}

func excerpt(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return strings.TrimSpace(value[:limit])
}

func normalizeInboundJID(msg channel.InboundMessage) string {
	if jid := normalizeWhatsAppJID(msg.SenderJID); jid != "" {
		return jid
	}
	return normalizeWhatsAppJID(msg.SenderIdentifier)
}

// operatorRecipient resolves where Victoria → operator messages should land
// for a given binding (notifications and review packets):
//
//   - WhatsApp A0 (read_only): draft_delivery_jid → operator_jid → provider_number
//   - WhatsApp A1 (full_control): first registered command identity, or "" if
//     no operator has registered yet (caller should queue + alert)
//   - Other channels: provider_number unchanged
func operatorRecipient(binding domain.ChannelBinding) string {
	if binding.Channel != string(channel.ChannelWhatsApp) {
		return binding.ProviderNumber
	}
	switch domain.ParseInboundMode(string(binding.InboundMode)) {
	case domain.InboundModeFullControl:
		if len(binding.CommandIdentities) > 0 {
			return binding.CommandIdentities[0]
		}
		return ""
	default:
		if binding.DraftDeliveryJID != "" {
			return binding.DraftDeliveryJID
		}
		if binding.OperatorJID != "" {
			return binding.OperatorJID
		}
		return binding.ProviderNumber
	}
}

func normalizeWhatsAppJID(input string) string {
	clean := strings.TrimSpace(input)
	if clean == "" {
		return ""
	}
	if strings.ContainsRune(clean, '@') {
		return clean
	}
	clean = strings.TrimPrefix(clean, "+")
	return clean + "@s.whatsapp.net"
}

// removeString returns a new slice with target removed. It allocates a fresh
// backing array rather than compacting in place: callers pass slices owned by
// the store (e.g. a channel binding's customer allowlist), and an in-place
// filter would corrupt that shared backing array.
func removeString(values []string, target string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != target {
			out = append(out, value)
		}
	}
	return out
}

func auditEvent(ids IDGenerator, now time.Time, tenantID, eventType, actorType, actorID, refType, refID string, related map[string]any, payload map[string]any, keyParts ...string) domain.AuditEvent {
	return domain.AuditEvent{
		ID:             ids.NewID("ae"),
		TenantID:       tenantID,
		EventType:      eventType,
		ActorType:      actorType,
		ActorID:        actorID,
		RefType:        refType,
		RefID:          refID,
		RelatedIDs:     cloneMap(related),
		Payload:        cloneMap(payload),
		IdempotencyKey: domain.SHA256Key(keyParts...),
		OccurredAt:     now,
	}
}

func scopePriority(scope domain.Scope) int {
	switch scope {
	case domain.ScopeCase:
		return 4
	case domain.ScopeTenant:
		return 3
	case domain.ScopeVertical:
		return 2
	default:
		return 1
	}
}

func boolValue(payload map[string]any, key string) bool {
	value, ok := payload[key]
	if !ok {
		return false
	}
	if b, ok := value.(bool); ok {
		return b
	}
	return fmt.Sprint(value) == "true"
}

func stringValue(payload map[string]any, key, fallback string) string {
	if value, ok := payload[key]; ok && fmt.Sprint(value) != "" {
		return fmt.Sprint(value)
	}
	return fallback
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func contains(values []string, value string) bool {
	for _, current := range values {
		if current == value {
			return true
		}
	}
	return false
}

func appendIfMissing(values []string, value string) []string {
	if contains(values, value) {
		return values
	}
	return append(values, value)
}
