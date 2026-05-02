# WhatsApp via whatsmeow — operator runbook

This document covers bringing a tenant online with WhatsApp messaging using the
embedded `whatsmeow` adapter (operator-ux spec §4.1–4.7).

## What you need

| Item | Why |
|---|---|
| Postgres database | whatsmeow's session store (tables prefixed `whatsmeow_*`) shares the same DB as Victoria's app data. Required for the WhatsApp adapter to start; without it the adapter is silently disabled. |
| A WhatsApp-enabled phone | Multi-device pairing: Victoria becomes a *linked device* of an existing WhatsApp account on a real phone. The phone must remain reachable at least every 14 days or WhatsApp auto-unlinks the device. |
| A dedicated demo number | whatsmeow uses an unofficial protocol. WhatsApp can ban accounts. Don't pair a personal or production number to a demo build. |
| `curl` and `python3` for the helper script | Used by `scripts/whatsapp-pair.sh`. |

## Bringing up the gateway

```sh
export VICTORIA_DATABASE_URL='postgres://user:pass@localhost:5432/victoria?sslmode=disable'
go run ./cmd/victoria
```

On boot you should see:

```
victoria using postgres store
whatsapp adapter active
victoria listening on :8080
```

If you see `whatsapp adapter disabled (no postgres or VICTORIA_WHATSAPP_DISABLED=1)`,
either Postgres isn't configured or you've explicitly disabled WhatsApp via
`VICTORIA_WHATSAPP_DISABLED=1` (the in-memory tests use this).

## Pairing a tenant

```sh
./scripts/whatsapp-pair.sh "Demo Roofing" "+61400000099" "op:demo"
```

Script flow:

1. `POST /admin/tenants` — provisions a tenant with the supplied number as
   `provider_number`. The provisioning step creates *both* a Telegram binding
   (status `active`, dev fallback) and a WhatsApp binding (status `qr_needed`).
2. `POST /channel-bindings/whatsapp/init` — starts a fresh whatsmeow session,
   returns the first QR string + a URL to fetch it as a 320×320 PNG.
3. The script downloads `/channel-bindings/whatsapp/qr.png` and opens it.
4. On the phone: WhatsApp → Settings → Linked Devices → Link a Device → scan.
5. The script polls `GET /channel-bindings/whatsapp/status` until it returns
   `active`, then sends a sample case so a review packet lands in your
   WhatsApp "Message Yourself" chat.

## Demo recipient model

By default each paired session sends review packets to the paired account's
own JID — they appear in WhatsApp's "Message Yourself" chat. This keeps the
demo possible with a single phone. To route packets to a different recipient
(e.g. a separate operator phone), set `recipient` on the per-tenant client
after pairing — that hook is exposed at the adapter level; see
`internal/channel/whatsapp/adapter.go` (`tenantClient.recipient`).

## HTTP surface

| Method | Path | Auth | Purpose |
|---|---|---|---|
| POST | `/channel-bindings/whatsapp/init` | tenant JWT | Start (or restart) pairing. Returns the current QR code string + PNG URL. |
| GET  | `/channel-bindings/whatsapp/qr` | tenant JWT | Re-fetch latest QR string (rotates every ~20s during pairing). |
| GET  | `/channel-bindings/whatsapp/qr.png` | tenant JWT | Same QR rendered as a PNG image (320×320). |
| GET  | `/channel-bindings/whatsapp/status` | tenant JWT | Returns one of: `qr_needed`, `connecting`, `active`, `disconnected`, `suspended`. |
| DELETE | `/channel-bindings/whatsapp` | tenant JWT | Logout + remove device from store. Tenant must re-pair afterwards. |

## Session lifecycle

The 5-state machine in `channel_bindings.session_status` matches operator-ux
§4.7.1. Audit events fired on transitions:

| From → To | Audit event | Notes |
|---|---|---|
| any → `active` | `packet_sent` (×N) | Outbound queue drains in FIFO order |
| `active` → `disconnected` | `channel_session_disconnected` | New packets queue durably in `outbound_queue` table |
| any → `suspended` | (logout flow) | Device deleted from whatsmeow store |
| (queue full while disconnected) | `packet_tombstoned` | Oldest entry dropped to make room (depth cap = 100, WA-INV-3) |

## What's deliberately NOT in this build

- **Encryption-at-rest for the whatsmeow session store.** Sessions live in
  Postgres `whatsmeow_*` tables. For production, use Postgres TDE or
  application-level envelope encryption per spec §4.4.
- **WhatsApp List Message rendering.** Packets render as plain text with
  numbered options for predictable client-side rendering. Operator replies
  with `1`/`2`/... or the action label. Spec §4.6 documents this as the
  acceptable fallback mode.
- **Multi-recipient routing.** Each tenant has one configured recipient.
- **KMS key rotation.** The 90-day rotation cadence in spec §4.4 is unwired
  in this build.

## Failure modes & responses

| Symptom | Likely cause | Action |
|---|---|---|
| `whatsapp_disabled` from any pairing endpoint | Gateway started without Postgres, or `VICTORIA_WHATSAPP_DISABLED=1` set | Set `VICTORIA_DATABASE_URL` and restart |
| QR rotates indefinitely without `active` | Phone scanned the QR but the linked-device handshake failed | Cancel from phone, retry `init` |
| `status: disconnected` shortly after `active` | whatsmeow lost connection to WhatsApp (network blip or server-side throttle) | Adapter auto-reconnects via whatsmeow's internal logic; check logs for repeated disconnects → may indicate rate-limit |
| `status: suspended` | WhatsApp logged the device out (manual logout from primary phone, or server-side ban) | Re-pair via `init`. If repeated bans → switch to a fresh number; whatsmeow is unofficial. |
| Inbound replies don't arrive | Operator replied to a different chat, or `[packet:xxx]` tag was edited out | The reply parser uses the trailing `[packet:<id>]` tag to associate replies with packets. Don't strip it. |

## Tests

- `internal/channel/whatsapp/adapter_test.go` — pure-logic tests (button mapping, JID parsing, text rendering). CI-runnable.
- `internal/app/app_test.go::TestWhatsAppOutageQueuesDurablyAndDrainsOnReconnect` — gateway-level test of the durable queue + drain semantics with a fake adapter. CI-runnable.
- `internal/app/app_test.go::TestWhatsAppQueueOverflowTombstonesOldest` — validates the 100-deep cap + oldest-tombstone overflow.
- `scripts/whatsapp-pair.sh` — manual end-to-end against a real number. NOT in CI.
