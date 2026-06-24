#!/usr/bin/env bash
# Showcase 4 — 00 channel: customer-inbound via email and Telegram
#
# Pitch: until now the showcases hand-fed cases via /cases. In production,
# cases originate from real customer messages — emails to the operator's
# support address, Telegram bot messages, web-form submissions. Victoria's
# canonical 00-channel ingestion path normalises all of them into a single
# IngestionEvent and starts an enquiry_triage case automatically.
#
# This showcase walks the storyline end-to-end over plain HTTP — no Postgres,
# no whatsmeow, no real phone needed. Runs against an in-memory server.
#
# Required env (same values the server was started with):
#   VICTORIA_GATEWAY_INBOUND_TOKEN — authenticates /gateway/inbound
#   VICTORIA_ADMIN_TOKEN           — authenticates the /admin/* control plane
# Server start:
#   VICTORIA_GATEWAY_INBOUND_TOKEN=demo-secret VICTORIA_ADMIN_TOKEN=demo-admin go run ./cmd/victoria
# (No -tags dev needed for showcase 4 — pure HTTP, no dev shim.)

set -euo pipefail
ADDR="${VICTORIA_ADDR:-http://localhost:8090}"
GW_TOKEN="${VICTORIA_GATEWAY_INBOUND_TOKEN:?must export VICTORIA_GATEWAY_INBOUND_TOKEN — same value the server was started with}"
ADMIN="Authorization: Bearer admin:${VICTORIA_ADMIN_TOKEN:?must export VICTORIA_ADMIN_TOKEN — same value the server was started with}"

scene () { printf "\n\n═══ Scene %s — %s ═══\n" "$1" "$2"; }
narrator () { printf "  📢 %s\n" "$1"; }

cat <<'INTRO'
══════════════════════════════════════════════════════════════════════
  SHOWCASE 4 — 00 channel: customer-inbound via email and Telegram
  Real customer messages funnel into Victoria. One canonical entry
  point, idempotent under re-delivery, drives the existing review loop.
══════════════════════════════════════════════════════════════════════
INTRO

# ─────────────────────────────────────────────────────────────────────
scene 0 "Provision a tenant"
TENANT_JSON=$(curl -fsS -X POST "$ADDR/admin/tenants" -H "$ADMIN" -H 'Content-Type: application/json' \
  -d '{"name":"Showcase-4 Roofing","vertical":"roofing","provider_number":"+61400000400","operator_id":"op:demo"}')
T=$(echo "$TENANT_JSON" | python3 -c 'import json,sys;print(json.load(sys.stdin)["tenant"]["id"])')
echo "  tenant=$T"
AUTH="Authorization: Bearer tid:$T"

ingest () {
  local channel="$1" sid="$2" who="$3" subject="$4" body="$5"
  curl -fsS -X POST "$ADDR/ingest/customer-message" -H "$AUTH" -H 'Content-Type: application/json' \
    -d "{\"channel\":\"$channel\",\"source_message_id\":\"$sid\",\"customer_identifier\":\"$who\",\"received_at\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"subject\":\"$subject\",\"body_text\":\"$body\"}"
}

audit_count () {
  curl -fsS "$ADDR/admin/audit-events?tenant_id=$T&event_types=$1" -H "$ADMIN" \
    | python3 -c 'import json,sys;print(len(json.load(sys.stdin)["audit_events"]))'
}

# ─────────────────────────────────────────────────────────────────────
scene 1 "Customer emails Victoria — case auto-created"
narrator "Sarah Lim emails the operator's published support address."
narrator "The IMAP poller (or its HTTP demo equivalent) POSTs the normalised"
narrator "payload to /ingest/customer-message. Victoria starts an enquiry_triage"
narrator "case and returns a review packet ready for operator triage."
SARAH=$(ingest "email" "imap-uid-101" "sarah@example.test" "Need a roof quote" "Hi, can you quote a roof repaint?")
SARAH_CR=$(echo "$SARAH" | python3 -c 'import json,sys;print(json.load(sys.stdin)["case_run_id"])')
SARAH_PKT=$(echo "$SARAH" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d["review_packet"]["packet_id"])')
echo "  case_run_id=$SARAH_CR"
echo "  packet_id  =$SARAH_PKT"
narrator "Audit: customer_message_received fired ($(audit_count customer_message_received) total)."

# ─────────────────────────────────────────────────────────────────────
scene 2 "Idempotency under re-delivery (IDEMP-1)"
narrator "Real IMAP loops fail mid-flight all the time — a poller crash before"
narrator "the message is flagged Seen means the same email is fetched again on"
narrator "the next run. Victoria MUST NOT create a second case."
REPLAY=$(ingest "email" "imap-uid-101" "sarah@example.test" "Need a roof quote" "Hi, can you quote a roof repaint?")
REPLAY_CR=$(echo "$REPLAY" | python3 -c 'import json,sys;print(json.load(sys.stdin)["case_run_id"])')
if [[ "$REPLAY_CR" != "$SARAH_CR" ]]; then
  echo "  ✘ replay returned a different case_run_id ($REPLAY_CR ≠ $SARAH_CR)" >&2
  exit 1
fi
echo "  ✓ replay returned the same case_run_id — idempotent on (tenant, channel, source_message_id)"
N=$(audit_count customer_message_received)
if [[ "$N" -ne "1" ]]; then
  echo "  ✘ customer_message_received fired $N times after replay; want exactly 1" >&2
  exit 1
fi
echo "  ✓ exactly one customer_message_received audit (no duplicate)"

# ─────────────────────────────────────────────────────────────────────
scene 3 "Different channel, same tenant — Telegram bot ingestion"
narrator "Carlos messages the tenant's Telegram bot. Same /ingest/customer-message,"
narrator "different channel string. New case, recorded with channel=telegram."
CARLOS=$(ingest "telegram" "tg-msg-7421" "tg:carlos_chat" "" "Hey, can you quote a chimney re-flash?")
CARLOS_CR=$(echo "$CARLOS" | python3 -c 'import json,sys;print(json.load(sys.stdin)["case_run_id"])')
CARLOS_PKT=$(echo "$CARLOS" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d["review_packet"]["packet_id"])')
echo "  case_run_id=$CARLOS_CR  (≠ Sarah's: $([ "$CARLOS_CR" != "$SARAH_CR" ] && echo yes || echo NO))"
echo "  packet_id  =$CARLOS_PKT"

# ─────────────────────────────────────────────────────────────────────
scene 4 "Operator approves Sarah's case via Telegram"
narrator "The 00-ingested case sits in the same review queue as any other case."
narrator "Operator hits 'Approve' on Sarah's packet. We drive that via the dev"
narrator "telegram webhook (/gateway/inbound) since this showcase doesn't pair WA."
curl -fsS -X POST "$ADDR/gateway/inbound" \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer gw:$GW_TOKEN" \
  -d "{\"channel\":\"telegram\",\"provider_number\":\"+61400000400\",\"packet_id\":\"$SARAH_PKT\",\"source_message_id\":\"sc4-approve-sarah\",\"action_button\":\"approve\"}" >/dev/null
echo "  ✓ approval signal accepted"
echo "  approval_received audit count: $(audit_count approval_received)"

# ─────────────────────────────────────────────────────────────────────
scene 5 "Audit timeline — what just happened"
echo
narrator "The full inbound + review audit trail for this tenant:"
curl -fsS "$ADDR/admin/audit-events?tenant_id=$T&event_types=customer_message_received,packet_sent,approval_received&limit=20" -H "$ADMIN" \
  | python3 -c '
import json, sys
for e in json.load(sys.stdin)["audit_events"]:
    print("  {at}  {kind:30s}  ref={ref}".format(at=e["occurred_at"][:19], kind=e["event_type"], ref=e["ref_id"]))
'

# ─────────────────────────────────────────────────────────────────────
scene 6 "Wrap-up"
echo
narrator "00-channel ingestion validated end-to-end:"
narrator "  • email → enquiry_triage case (Sarah, source_message_id=imap-uid-101)"
narrator "  • idempotent on re-delivery — same case_run_id, single audit"
narrator "  • cross-channel: telegram and email coexist for the same tenant"
narrator "  • the new ingestion path slots into the existing review loop"
narrator "    operators already use — no separate triage UX to learn"
echo
echo "Storyline complete."
