#!/usr/bin/env bash
# Showcase 6 — A1 channel: dedicated Victoria number, customer outbound
#
# Pitch: in A1 mode the tenant gets a dedicated WhatsApp number that Victoria
# controls end-to-end. Customers message that number; Victoria reads, drafts;
# the operator approves; Victoria sends the reply directly to the customer.
#
# This showcase walks AC-A1.1 → AC-A1.2 → AC-A1.4:
#   • Customer message arrives BEFORE operator registration → packet queued
#     and `no_command_identity_registered` audit fires.
#   • Operator types "register me as operator <secret>" → identity registered;
#     queued packet drains immediately.
#   • Operator approves → Victoria sends the reply to the customer's JID
#     and writes a customer_outbound_sent audit (gated by MCP approval).
#
# Uses the dev-only inbound shim. Those routes only exist in binaries built
# with `-tags dev`; production binaries do not contain the code at all.
#
# Required env (same value the server was started with):
#   VICTORIA_ADMIN_TOKEN — authenticates the /admin/* control plane
# Server start (the server also requires VICTORIA_GATEWAY_INBOUND_TOKEN):
#   VICTORIA_GATEWAY_INBOUND_TOKEN=demo-secret VICTORIA_ADMIN_TOKEN=demo-admin go run -tags dev ./cmd/victoria

set -euo pipefail
ADDR="${VICTORIA_ADDR:-http://localhost:8090}"
ADMIN="Authorization: Bearer admin:${VICTORIA_ADMIN_TOKEN:?must export VICTORIA_ADMIN_TOKEN — same value the server was started with}"

scene () { printf "\n\n═══ Scene %s — %s ═══\n" "$1" "$2"; }
narrator () { printf "  📢 %s\n" "$1"; }

cat <<'INTRO'
══════════════════════════════════════════════════════════════════════
  SHOWCASE 6 — A1 channel: dedicated Victoria number
  Operator registration via in-band WA secret. Customer reply sent
  directly. (Dev shim — not for production.)
══════════════════════════════════════════════════════════════════════
INTRO

# ─────────────────────────────────────────────────────────────────────
scene 0 "Provision tenant in A1 mode (capture command secret)"
TENANT_JSON=$(curl -fsS -X POST "$ADDR/admin/tenants" -H "$ADMIN" -H 'Content-Type: application/json' \
  -d '{"name":"Showcase-6 Roofing","vertical":"roofing","provider_number":"+61400000600","operator_id":"op:demo"}')
T=$(echo "$TENANT_JSON" | python3 -c 'import json,sys;print(json.load(sys.stdin)["tenant"]["id"])')
SECRET=$(echo "$TENANT_JSON" | python3 -c 'import json,sys;print(json.load(sys.stdin)["manifest"]["whatsapp_command_secret"])')
echo "  tenant=$T"
echo "  whatsapp_command_secret=${SECRET:0:8}…   (in production this is emailed to ops at provisioning)"
AUTH="Authorization: Bearer tid:$T"

curl -fsS -X POST "$ADDR/channel-bindings/whatsapp/consent" -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"inbound_mode":"full_control"}' >/dev/null
echo "  ✓ A1 consent recorded (inbound_mode=full_control)"
curl -fsS -X POST "$ADDR/admin/dev/whatsapp/session-status" -H 'Content-Type: application/json' \
  -d "{\"tenant_id\":\"$T\",\"status\":\"active\"}" >/dev/null
echo "  ✓ dev: WhatsApp session marked active"

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
scene 1 "Customer messages BEFORE operator registers (AC-A1.1)"
narrator "A customer reaches the dedicated Victoria number. There's no command"
narrator "identity registered yet, so the operator-bound review packet has no"
narrator "valid recipient. Per AC-A1.1, Victoria must (a) ingest the case,"
narrator "(b) persist the packet to the durable queue, and (c) emit"
narrator "no_command_identity_registered so ops can intervene."
wa_inbound "85288888888@s.whatsapp.net" "false" "wa-cust-early" "Quote please for tile re-grout?"
echo "  customer_message_received audits      : $(audit_count customer_message_received)  (expect 1)"
echo "  no_command_identity_registered audits : $(audit_count no_command_identity_registered)  (expect 1)"
echo "  packet_sent audits                    : $(audit_count packet_sent)  (expect 0 — queued)"
if [[ "$(audit_count packet_sent)" != "0" ]]; then
  echo "  ✘ packet was delivered before operator registered" >&2
  exit 1
fi

# ─────────────────────────────────────────────────────────────────────
scene 2 "Operator registers via WA secret (AC-A1.2) → drain"
narrator "The operator messages the Victoria number from their own WA account"
narrator "with: register me as operator <secret>"
narrator "App.handleWhatsAppCommand validates against binding.CommandRegistrationSecret,"
narrator "marks it consumed, and Gateway.DrainQueue fires immediately."
wa_inbound "61411111111@s.whatsapp.net" "false" "wa-cmd-register" "register me as operator $SECRET"
echo "  packet_sent audits after registration: $(audit_count packet_sent)  (expect ≥1 — queue drained)"
if [[ "$(audit_count packet_sent)" -lt "1" ]]; then
  echo "  ✘ queued packet did not drain after registration" >&2
  exit 1
fi

# Recover the packet_id of the customer's case for the approval step.
PACKET=$(curl -fsS "$ADDR/admin/audit-events?tenant_id=$T&event_types=packet_sent&limit=1" -H "$ADMIN" \
  | python3 -c 'import json,sys;d=json.load(sys.stdin)["audit_events"];print(d[-1]["ref_id"])')
echo "  drained packet=$PACKET"

# ─────────────────────────────────────────────────────────────────────
scene 3 "Operator approves → reply sent directly to the customer (AC-A1.4)"
narrator "Operator replies Y on the registered command-identity JID."
narrator "Approval audit fires; SendApprovedCustomerReply pulls the approval"
narrator "audit id, writes a queued outbound_to_customer row, asks the WA"
narrator "adapter to send to the customer JID, then flips the row to 'sent'"
narrator "and emits customer_outbound_sent."
wa_inbound "61411111111@s.whatsapp.net" "false" "wa-approve-a1" "Y"
echo "  approval_received        : $(audit_count approval_received)   (expect 1)"
echo "  customer_outbound_sent   : $(audit_count customer_outbound_sent)   (expect 1)"
echo "  outbound_draft_delivered : $(audit_count outbound_draft_delivered_to_operator)   (expect 0 — A1 not A0)"
if [[ "$(audit_count customer_outbound_sent)" != "1" ]]; then
  echo "  ✘ customer_outbound_sent did not fire" >&2
  exit 1
fi

# ─────────────────────────────────────────────────────────────────────
scene 4 "Replay safety — second approval is a no-op"
narrator "Repeat the approve message (e.g. operator double-tap). The signal"
narrator "envelope is idempotent on (tenant, packet, action_button), and"
narrator "outbound_to_customer is idempotent on (tenant, case_run, body_hash)."
wa_inbound "61411111111@s.whatsapp.net" "false" "wa-approve-a1-replay" "Y"
echo "  customer_outbound_sent after replay   : $(audit_count customer_outbound_sent)   (must still be 1)"
if [[ "$(audit_count customer_outbound_sent)" != "1" ]]; then
  echo "  ✘ replay produced a duplicate customer_outbound_sent" >&2
  exit 1
fi

# ─────────────────────────────────────────────────────────────────────
scene 5 "Audit timeline"
echo
curl -fsS "$ADDR/admin/audit-events?tenant_id=$T&limit=20" -H "$ADMIN" \
  | python3 -c '
import json, sys
for e in json.load(sys.stdin)["audit_events"]:
    print("  {at}  {kind:36s}  ref={ref}".format(at=e["occurred_at"][:19], kind=e["event_type"], ref=e["ref_id"]))
'

# ─────────────────────────────────────────────────────────────────────
scene 6 "Wrap-up"
echo
narrator "A1 invariants validated:"
narrator "  • AC-A1.1 — customer message before registration queues + alerts"
narrator "  • AC-A1.2 — register me as operator drains the queue immediately"
narrator "  • AC-A1.4 — approved reply sent directly to the customer JID"
narrator "  • IDEMP-2 — replayed approval doesn't double-send"
echo
echo "Storyline complete."
