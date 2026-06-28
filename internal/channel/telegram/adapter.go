// Package telegram provides a no-op Telegram adapter that satisfies the
// channel.Adapter contract. The existing /gateway/inbound HTTP webhook still
// drives Telegram-style replies for tests and dev.
//
// SendOutbound is a stub that returns success without contacting the Telegram
// Bot API: tests rely on the in-process Gateway path and don't need a real
// HTTP round-trip. A production Telegram deployment would replace this with a
// thin wrapper around the Bot API, keeping the interface unchanged.
package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jessepcc/victoria/internal/channel"
)

type Adapter struct{}

func New() *Adapter { return &Adapter{} }

func (*Adapter) Channel() channel.Channel { return channel.ChannelTelegram }

func (*Adapter) SendOutbound(_ context.Context, msg channel.OutboundMessage) (channel.DeliveryReceipt, error) {
	if msg.PacketID == "" {
		return channel.DeliveryReceipt{}, errors.New("telegram: empty packet id")
	}
	return channel.DeliveryReceipt{ProviderMessageID: "tg:" + msg.PacketID, SentAt: time.Now().UTC()}, nil
}

// NormalizeInboundWebhook accepts the dev/internal Telegram webhook shape used
// by /gateway/inbound. The shape mirrors a small subset of Telegram's
// callback_query/message structure — enough to map button payloads + free text.
func (*Adapter) NormalizeInboundWebhook(rawPayload []byte) ([]channel.InboundMessage, error) {
	if len(rawPayload) == 0 {
		return nil, nil
	}
	var raw struct {
		ChatID         string `json:"chat_id"`
		ProviderNumber string `json:"provider_number"`
		MessageID      string `json:"message_id"`
		ButtonPayload  string `json:"button_payload,omitempty"`
		Text           string `json:"text,omitempty"`
		Timestamp      string `json:"timestamp,omitempty"`
	}
	if err := json.Unmarshal(rawPayload, &raw); err != nil {
		return nil, err
	}
	sender := raw.ProviderNumber
	if sender == "" {
		sender = raw.ChatID
	}
	ts, _ := time.Parse(time.RFC3339, raw.Timestamp)
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	return []channel.InboundMessage{{
		SenderIdentifier:  sender,
		ProviderMessageID: raw.MessageID,
		Channel:           channel.ChannelTelegram,
		ButtonPayload:     raw.ButtonPayload,
		FreeText:          raw.Text,
		ReceivedAt:        ts,
	}}, nil
}
