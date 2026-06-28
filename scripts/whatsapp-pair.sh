#!/usr/bin/env bash
# whatsapp-pair.sh — provision a tenant + pair a WhatsApp session end-to-end.
#
# Prerequisites:
#   1. Postgres running and reachable via $VICTORIA_DATABASE_URL
#   2. Victoria HTTP server running (go run ./cmd/victoria)
#   3. A phone with WhatsApp installed and signed in to the demo number
#
# Usage:
#   ./scripts/whatsapp-pair.sh <tenant_name> <provider_number> [<operator_id>]
#
# Example:
#   ./scripts/whatsapp-pair.sh "Demo Roofing" "+61400000099" "op:demo"
#
# This script:
#   1. POSTs /admin/tenants → captures tenant_id
#   2. POSTs /channel-bindings/whatsapp/consent → records read-only consent
#   3. POSTs /channel-bindings/whatsapp/init → kicks off pairing
#   4. Saves QR PNG to /tmp/victoria-wa-qr.png and opens it
#   5. Polls /channel-bindings/whatsapp/status until "active"
#   6. Posts a sample case → review packet hits the paired phone

set -euo pipefail

ADDR="${VICTORIA_ADDR:-http://localhost:8080}"
TENANT_NAME="${1:-Demo Roofing}"
PROVIDER_NUMBER="${2:-}"
OPERATOR_ID="${3:-op:demo}"

if [[ -z "$PROVIDER_NUMBER" ]]; then
  echo "usage: $0 <tenant_name> <provider_number> [<operator_id>]" >&2
  exit 64
fi

ADMIN="Authorization: Bearer admin:${VICTORIA_ADMIN_TOKEN:?must export VICTORIA_ADMIN_TOKEN — same value the server was started with}"
echo "→ Provisioning tenant '$TENANT_NAME' with WhatsApp number $PROVIDER_NUMBER"
TENANT_JSON=$(curl -fsS -X POST "$ADDR/admin/tenants" \
  -H "$ADMIN" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"$TENANT_NAME\",\"vertical\":\"roofing\",\"provider_number\":\"$PROVIDER_NUMBER\",\"operator_id\":\"$OPERATOR_ID\"}")
TENANT_ID=$(echo "$TENANT_JSON" | python3 -c 'import json,sys;print(json.load(sys.stdin)["tenant"]["id"])')
echo "  tenant_id=$TENANT_ID"
# Showcases 1, 2, 3 and demo-story.sh read the active tenant id from this file.
echo -n "$TENANT_ID" > /tmp/victoria-tenant-id.txt
echo "  tenant_id saved to /tmp/victoria-tenant-id.txt"

AUTH="Authorization: Bearer tid:$TENANT_ID"

echo "→ Recording WhatsApp read-only consent"
curl -fsS -X POST "$ADDR/channel-bindings/whatsapp/consent" \
  -H "$AUTH" \
  -H 'Content-Type: application/json' \
  -d "{\"inbound_mode\":\"read_only\",\"draft_delivery_jid\":\"$PROVIDER_NUMBER\"}" >/dev/null

echo "→ Beginning WhatsApp pairing"
INIT_JSON=$(curl -fsS -X POST "$ADDR/channel-bindings/whatsapp/init" -H "$AUTH" -d '{}')
QR_CODE=$(echo "$INIT_JSON" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("qr",""))')
if [[ -z "$QR_CODE" ]]; then
  echo "ERROR: no QR returned. Is the WhatsApp manager enabled? Check VICTORIA_DATABASE_URL." >&2
  echo "$INIT_JSON" >&2
  exit 1
fi

QR_PATH="/tmp/victoria-wa-qr-$TENANT_ID.png"
echo "→ Downloading QR PNG to $QR_PATH"
curl -fsS "$ADDR/channel-bindings/whatsapp/qr.png" -H "$AUTH" -o "$QR_PATH"

case "$(uname)" in
  Darwin)  open "$QR_PATH" ;;
  Linux)   command -v xdg-open >/dev/null && xdg-open "$QR_PATH" || true ;;
esac

echo "→ Open WhatsApp on $PROVIDER_NUMBER's phone:"
echo "    Settings → Linked Devices → Link a Device → scan the QR window"
echo
echo "→ Polling pairing status (Ctrl-C to abort)"

while true; do
  STATUS_JSON=$(curl -fsS "$ADDR/channel-bindings/whatsapp/status" -H "$AUTH")
  STATUS=$(echo "$STATUS_JSON" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("status",""))')
  printf "    status=%s\r" "$STATUS"
  case "$STATUS" in
    active)      echo; echo "✔ paired."; break ;;
    suspended)   echo; echo "✘ pairing failed (logged out)."; exit 1 ;;
    "")          echo; echo "✘ no manager status yet — retry in a moment"; ;;
  esac
  sleep 2
done

echo
echo "→ Sending a sandbox quote_drafting case"
CASE_JSON=$(curl -fsS -X POST "$ADDR/cases" -H "$AUTH" -H 'Content-Type: application/json' -d '{
  "workflow_slug":"quote_drafting","mode":"sandbox",
  "payload":{"sandbox":true,"client_type":"new","photos_complete":false,"case_name":"demo-walkthrough"}
}')
PACKET_ID=$(echo "$CASE_JSON" | python3 -c 'import json,sys;print(json.load(sys.stdin)["review_packet"]["packet_id"])')
echo "  packet_id=$PACKET_ID"
echo
echo "→ The review packet should now appear in WhatsApp on $PROVIDER_NUMBER (Message Yourself chat by default)."
echo "  Reply with the option number to drive the correction loop."
