// Package channel defines the narrow ChannelAdapter test seam (operator-ux §4.5).
// Two methods only — anything wider is per-channel concrete (whatsmeow session,
// Telegram bot token, etc.).
package channel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"victoria/internal/domain"
)

type Channel string

const (
	ChannelWhatsApp Channel = "whatsapp"
	ChannelTelegram Channel = "telegram"
)

type Adapter interface {
	Channel() Channel
	SendOutbound(ctx context.Context, msg OutboundMessage) (DeliveryReceipt, error)
	NormalizeInboundWebhook(rawPayload []byte) ([]InboundMessage, error)
}

type OutboundMessage struct {
	TenantID            string
	RecipientIdentifier string
	PacketID            string
	BodyText            string
	Buttons             []ButtonSpec
	IdempotencyKey      string
}

type ButtonSpec struct {
	ID    string
	Label string
}

type DeliveryReceipt struct {
	ProviderMessageID string
	SentAt            time.Time
}

type InboundMessage struct {
	SenderIdentifier  string
	ProviderMessageID string
	Channel           Channel
	ButtonPayload     string
	FreeText          string
	ReceivedAt        time.Time
}

var (
	ErrSessionNotConnected = errors.New("channel: session not connected")
	ErrAdapterNotAvailable = errors.New("channel: adapter not available for tenant")
)

// RenderPacket converts a ReviewPacket into an OutboundMessage. Body text is plain
// (no channel-specific markdown). Buttons are derived from the packet's button_set.
func RenderPacket(packet domain.ReviewPacket, recipient string) OutboundMessage {
	body := renderBody(packet)
	buttons := make([]ButtonSpec, 0, len(packet.ButtonSet))
	for _, b := range packet.ButtonSet {
		buttons = append(buttons, ButtonSpec{ID: string(b), Label: buttonLabel(b)})
	}
	return OutboundMessage{
		TenantID:            packet.TenantID,
		RecipientIdentifier: recipient,
		PacketID:            packet.PacketID,
		BodyText:            body,
		Buttons:             buttons,
		IdempotencyKey:      packet.IdempotencyKey,
	}
}

// renderBody narrates the packet in a first-person, workflow-aware tone. The
// goal is that an operator reading the message understands:
//   1. who/what came in,
//   2. what Victoria sees about it,
//   3. what Victoria is planning to do, and
//   4. how to respond (tap or natural-language reply).
func renderBody(p domain.ReviewPacket) string {
	facts := factsToMap(p.Facts)
	switch p.WorkflowType {
	case "quote_drafting":
		return narrateQuote(p, facts)
	case "invoice_handling":
		return narrateInvoice(p, facts)
	case "enquiry_triage":
		return narrateTriage(p, facts)
	default:
		return narrateGeneric(p, facts)
	}
}

func narrateQuote(p domain.ReviewPacket, f map[string]string) string {
	customer := nonEmpty(f["customer_name"], "a new customer")
	project := nonEmpty(f["project_summary"], "their project")
	clientType := f["client_type"]
	photosComplete := f["photos_complete"] == "true"

	var b strings.Builder
	fmt.Fprintf(&b, "👋 New quote enquiry from *%s* — %s.\n\n", customer, project)
	if clientType == "repeat" {
		b.WriteString("They're a repeat customer (we've worked with them before).\n")
	} else if clientType == "commercial" {
		b.WriteString("They look like a commercial lead.\n")
	} else {
		b.WriteString("They're a new customer.\n")
	}
	if photosComplete {
		b.WriteString("Photos look complete.\n")
	} else {
		b.WriteString("⚠️ They haven't sent any photos yet.\n")
	}
	fmt.Fprintf(&b, "\n👉 I'm planning to: *%s*", humanAction(p.PlannedAction.Type))
	b.WriteString(replyPrompt())
	return b.String()
}

func narrateInvoice(p domain.ReviewPacket, f map[string]string) string {
	supplier := nonEmpty(f["supplier_name"], "a supplier")
	amount := nonEmpty(f["invoice_amount"], "an unknown amount")
	country := nonEmpty(f["supplier_country"], "")

	var b strings.Builder
	fmt.Fprintf(&b, "📄 New invoice from *%s* for %s.\n\n", supplier, amount)
	if country != "" && country != "AU" {
		fmt.Fprintf(&b, "They're based in %s (overseas).\n", country)
	} else if country == "AU" {
		b.WriteString("They're an AU-based supplier.\n")
	}
	fmt.Fprintf(&b, "\n👉 I'm planning to: *%s*", humanAction(p.PlannedAction.Type))
	b.WriteString(replyPrompt())
	return b.String()
}

func narrateTriage(p domain.ReviewPacket, f map[string]string) string {
	from := nonEmpty(f["from"], "a new contact")
	excerpt := nonEmpty(f["message_excerpt"], "")

	var b strings.Builder
	fmt.Fprintf(&b, "📨 New enquiry from *%s*.\n", from)
	if excerpt != "" {
		fmt.Fprintf(&b, "_\"%s\"_\n", excerpt)
	}
	fmt.Fprintf(&b, "\n👉 I'm planning to: *%s*", humanAction(p.PlannedAction.Type))
	b.WriteString(replyPrompt())
	return b.String()
}

func narrateGeneric(p domain.ReviewPacket, f map[string]string) string {
	var b strings.Builder
	b.WriteString("📥 Victoria has a decision waiting for you.\n\n")
	for k, v := range f {
		fmt.Fprintf(&b, "• %s: %s\n", k, v)
	}
	fmt.Fprintf(&b, "\n👉 I'm planning to: *%s*", humanAction(p.PlannedAction.Type))
	b.WriteString(replyPrompt())
	return b.String()
}

func humanAction(action string) string {
	switch action {
	case "send_quote":
		return "send our standard quote"
	case "hold_and_request_more_info":
		return "hold and ask for more info first"
	case "apply_gst":
		return "record this with the standard 10% GST applied"
	case "apply_no_gst":
		return "record this as GST-exempt"
	case "use_corporate_template":
		return "reply with our corporate-client template"
	case "draft_reply":
		return "draft a standard reply"
	default:
		return action
	}
}

func replyPrompt() string {
	return "\n\n*Approved?* Y / N"
}

func factsToMap(facts []domain.Fact) map[string]string {
	out := make(map[string]string, len(facts))
	for _, f := range facts {
		out[f.Label] = f.Value
	}
	return out
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func buttonLabel(b domain.ActionButton) string {
	switch b {
	case domain.ActionApprove:
		return "Approve"
	case domain.ActionWrongFacts:
		return "Wrong facts"
	case domain.ActionWrongAction:
		return "Wrong action"
	case domain.ActionMissingCondition:
		return "Missing condition"
	case domain.ActionUseDifferentTemplate:
		return "Use different template"
	case domain.ActionAddNote:
		return "Add note"
	default:
		return string(b)
	}
}
