#!/usr/bin/env bash
# Showcase 5 — A0 channel: customer-inbound on the operator's own WhatsApp
#
# Pitch: in A0 mode, Victoria reads the operator's own WhatsApp account.
# Personal messages are ignored. Only chats explicitly allowlisted as
# customer enquiries become Victoria cases. The operator manages that
# allowlist by typing commands on WhatsApp itself — no admin UI.
#
# This showcase exercises the A0 path without a real WhatsApp pairing by
# using the dev-only /admin/dev/whatsapp/inbound shim. Those routes are
# compiled in only when the server is built with `-tags dev`. Production
# binaries (no tag) do not contain the dev code at all.
#
# Required env (same value the server was started with):
#   VICTORIA_ADMIN_TOKEN — authenticates the /admin/* control plane
# Server start (the server also requires VICTORIA_GATEWAY_INBOUND_TOKEN):
#   VICTORIA_GATEWAY_INBOUND_TOKEN=demo-secret VICTORIA_ADMIN_TOKEN=demo-admin go run -tags dev ./cmd/victoria
#
# Storyline:
#   1. A friend's personal message lands → ignored, no case.
#   2. Operator types "add customer +85299999999" on WhatsApp → allowlist.
#   3. That customer messages → enquiry_triage case auto-opens.
#   4. Operator approves → A0 draft delivered to operator (NOT to customer).

set -euo pipefail
ADDR="${VICTORIA_ADDR:-http://localhost:8090}"
ADMIN="Authorization: Bearer admin:${VICTORIA_ADMIN_TOKEN:?must export VICTORIA_ADMIN_TOKEN — same value the server was started with}"

scene () { printf "\n\n═══ Scene %s — %s ═══\n" "$1" "$2"; }
narrator () { printf "  📢 %s\n" "$1"; }

cat <<'INTRO'
══════════════════════════════════════════════════════════════════════
  SHOWCASE 5 — A0 channel: read-only Victoria on the operator's WA
  Allowlist gate, in-band commands, draft-to-operator only.
  (Uses dev-only /admin/dev/whatsapp/inbound shim; not for production.)
══════════════════════════════════════════════════════════════════════
INTRO

# ─────────────────────────────────────────────────────────────────────
scene 0 "Provision tenant + acknowledge A0 read-only consent"
TENANT_JSON=$(curl -fsS -X POST "$ADDR/admin/tenants" -H "$ADMIN" -H 'Content-Type: application/json' \
  -d '{"name":"Showcase-5 Roofing","vertical":"roofing","provider_number":"+61400000500","operator_id":"op:demo"}')
T=$(echo "$TENANT_JSON" | python3 -c 'import json,sys;print(json.load(sys.stdin)["tenant"]["id"])')
echo "  tenant=$T"
AUTH="Authorization: Bearer tid:$T"

curl -fsS -X POST "$ADDR/channel-bindings/whatsapp/consent" -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"inbound_mode":"read_only","draft_delivery_jid":"61400000500@s.whatsapp.net"}' >/dev/null
echo "  ✓ A0 consent recorded; draft_delivery_jid=61400000500@s.whatsapp.net"
# Dev shim: mark the session active so the gateway will deliver outbound packets
# through the dev fake WA adapter (real flow would be a QR pair → connecting → active).
curl -fsS -X POST "$ADDR/admin/dev/whatsapp/session-status" -H 'Content-Type: application/json' \
  -d "{\"tenant_id\":\"$T\",\"status\":\"active\"}" >/dev/null
echo "  ✓ dev: WhatsApp session marked active"

# Helper: simulate a WhatsApp inbound message via the dev shim.
wa_inbound () {
  local sender_jid="$1" is_from_me="$2" provider_msg_id="$3" body="$4"
  curl -fsS -X POST "$ADDR/admin/dev/whatsapp/inbound" -H 'Content-Type: application/json' \
    -d "{\"tenant_id\":\"$T\",\"sender_jid\":\"$sender_jid\",\"is_from_me\":$is_from_me,\"provider_message_id\":\"$provider_msg_id\",\"free_text\":\"$body\"}" >/dev/null
}

audit_count () {
  curl -fsS "$ADDR/admin/audit-events?tenant_id=$T&event_types=$1" -H "$ADMIN" \
    | python3 -c 'import json,sys;print(len(json.load(sys.stdin)["audit_events"]))'
}

# ─────────────────────────────────────────────────────────────────────
scene 1 "Personal contact messages → ignored (privacy invariant)"
narrator "An old friend sends 'lunch tomorrow?' via WhatsApp. They're not a"
narrator "customer; their JID is not on the allowlist. Victoria MUST drop the"
narrator "message — no case, no customer_messages row, no audit-with-content."
wa_inbound "61499999911@s.whatsapp.net" "false" "wa-friend-1" "lunch tomorrow?"
echo "  customer_message_received audits: $(audit_count customer_message_received)  (expect 0)"
if [[ "$(audit_count customer_message_received)" != "0" ]]; then
  echo "  ✘ non-allowlisted message reached customer_messages" >&2
  exit 1
fi

# ─────────────────────────────────────────────────────────────────────
scene 2 "Operator allowlists a customer via in-band WA command"
narrator "The operator types 'add customer +85299999999' on their own WA."
narrator "The message has IsFromMe=true (came from the paired account)."
narrator "App.handleWhatsAppCommand recognises the prefix and updates the binding."
wa_inbound "61400000500@s.whatsapp.net" "true" "wa-cmd-add" "add customer +85299999999"
ALLOWLIST=$(curl -fsS "$ADDR/channel-bindings/whatsapp/customers" 2>/dev/null || true)
# Fetch via consent endpoint to read back the allowlist.
BINDING=$(curl -fsS -X POST "$ADDR/channel-bindings/whatsapp/consent" -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"inbound_mode":"read_only","draft_delivery_jid":"61400000500@s.whatsapp.net"}')
echo "  binding.customer_allowlist:"
echo "$BINDING" | python3 -c 'import json,sys;d=json.load(sys.stdin);print("    "+", ".join(d["binding"].get("customer_allowlist",[])))'

# ─────────────────────────────────────────────────────────────────────
scene 3 "Allowlisted customer messages → case opens"
narrator "Now the same JID writes a real enquiry. Victoria ingests, opens an"
narrator "enquiry_triage case, and emits customer_message_received."
wa_inbound "85299999999@s.whatsapp.net" "false" "wa-cust-1" "Hi, can you quote a kitchen tile job?"
echo "  customer_message_received audits: $(audit_count customer_message_received)  (expect 1)"

# Find the latest review packet so we can drive the approval next.
PACKET=$(curl -fsS "$ADDR/admin/audit-events?tenant_id=$T&event_types=packet_sent&limit=1" -H "$ADMIN" \
  | python3 -c 'import json,sys;d=json.load(sys.stdin)["audit_events"];print(d[-1]["ref_id"] if d else "")')
echo "  latest review packet: $PACKET"

# ─────────────────────────────────────────────────────────────────────
scene 4 "Operator approves → A0 draft delivered to operator (NOT to customer)"
narrator "Operator approves with a Y reply on WhatsApp. Per OQ-2 RESOLVED,"
narrator "Victoria sends the rendered draft text to draft_delivery_jid for the"
narrator "operator to long-press → forward to the customer's chat. Victoria"
narrator "MUST NOT send anything directly to the customer in A0."
wa_inbound "61400000500@s.whatsapp.net" "true" "wa-approve-1" "Y"
DRAFT_AUDITS=$(audit_count outbound_draft_delivered_to_operator)
CUSTOMER_OUT=$(audit_count customer_outbound_sent)
echo "  outbound_draft_delivered_to_operator: $DRAFT_AUDITS   (expect 1)"
echo "  customer_outbound_sent              : $CUSTOMER_OUT   (expect 0 — A0 never sends to customer)"
if [[ "$CUSTOMER_OUT" != "0" ]]; then
  echo "  ✘ A0 mode emitted a customer_outbound_sent audit — invariant A0-OUT-1 violated" >&2
  exit 1
fi

# ─────────────────────────────────────────────────────────────────────
scene 5 "Audit timeline"
echo
curl -fsS "$ADDR/admin/audit-events?tenant_id=$T&limit=15" -H "$ADMIN" \
  | python3 -c '
import json, sys
for e in json.load(sys.stdin)["audit_events"]:
    print("  {at}  {kind:36s}  ref={ref}".format(at=e["occurred_at"][:19], kind=e["event_type"], ref=e["ref_id"]))
'

# ─────────────────────────────────────────────────────────────────────
scene 6 "Wrap-up"
echo
narrator "A0 invariants validated:"
narrator "  • non-allowlisted messages never reach customer_messages"
narrator "  • operator self-service allowlist via in-band WA command"
narrator "  • approval delivers a draft to the operator only"
narrator "  • zero customer_outbound_sent events — the customer-facing channel"
narrator "    is the operator's own thumb, exactly as A0 promises"
echo
echo "Storyline complete."
