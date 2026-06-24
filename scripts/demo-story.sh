#!/usr/bin/env bash
# demo-story.sh — walk a storyline of 6 scenes that show Victoria learning
# a new rule from operator corrections.
#
# Storyline: ABC Roofing's owner uses Victoria as their first-pass
# enquiry-handler. By default Victoria sends a quote to every new enquiry.
# But the owner has an unwritten rule: don't quote without photos. We watch
# Victoria learn that rule from corrections, get it promoted, and apply it.
#
# Usage:
#   scripts/demo-story.sh           # interactive: prompts you to reply on WhatsApp
#   scripts/demo-story.sh --auto    # skips waits (for screenshot rehearsals)

set -euo pipefail

ADDR="${VICTORIA_ADDR:-http://localhost:8080}"
TENANT_ID_FILE="/tmp/victoria-tenant-id.txt"
[[ -f "$TENANT_ID_FILE" ]] || { echo "no tenant id at $TENANT_ID_FILE — pair first via scripts/whatsapp-pair.sh" >&2; exit 64; }
T=$(cat "$TENANT_ID_FILE")
AUTH="Authorization: Bearer tid:$T"
ADMIN="Authorization: Bearer admin:${VICTORIA_ADMIN_TOKEN:?must export VICTORIA_ADMIN_TOKEN — same value the server was started with}"

scene () {
  local n="$1" title="$2"
  echo
  echo "═════════════════════════════════════════════════════════════"
  echo "▶ Scene $n: $title"
  echo "═════════════════════════════════════════════════════════════"
}

send_case () {
  local payload="$1"
  curl -fsS -X POST "$ADDR/cases" -H "$AUTH" -H 'Content-Type: application/json' -d "$payload" \
    | python3 -c '
import json, sys
p = json.load(sys.stdin)["review_packet"]
print("  packet_id     =", p["packet_id"])
print("  planned_action=", p["planned_action"]["type"])
'
}

wait_for_reply () {
  local label="$1" expect_audit="$2"
  echo
  echo "  ⏳ waiting for your $label reply on WhatsApp ..."
  for i in $(seq 1 300); do
    n=$(psql -d victoria_demo -tA -c "SELECT COUNT(*) FROM audit_events WHERE tenant_id='$T' AND event_type='$expect_audit'" 2>/dev/null)
    if [[ -n "${prev_count:-}" ]] && [[ "$n" -gt "$prev_count" ]]; then
      echo "  ✓ $expect_audit fired"
      prev_count=$n
      return
    fi
    prev_count=${prev_count:-$n}
    sleep 2
  done
  echo "  ✘ timeout — no $expect_audit yet" >&2
  exit 1
}

current_audit_count () {
  psql -d victoria_demo -tA -c "SELECT COUNT(*) FROM audit_events WHERE tenant_id='$T' AND event_type='$1'" 2>/dev/null
}

prompt_continue () {
  echo
  read -r -p "  press ⏎ to continue ..." _
}

echo "Victoria storyline demo for tenant $T"

# -----------------------------------------------------------------------------
scene 1 "Baseline approval — Victoria gets it right"
echo "  Mike's Garage (a regular customer) wants a quote for a roof repaint."
echo "  Photos look complete. Victoria proposes to send the quote."
send_case '{
  "workflow_slug": "quote_drafting",
  "mode": "sandbox",
  "payload": {
    "sandbox": true,
    "case_name": "scene1",
    "customer_name": "Mike'\''s Garage",
    "project_summary": "Repaint of garage roof, ~80 sqm",
    "client_type": "repeat",
    "photos_complete": true
  }
}'
echo
echo "  → Reply on WhatsApp with: 1   (or: ok, sure, looks good)"
prev_count=$(current_audit_count approval_received)
wait_for_reply "approval" approval_received

# -----------------------------------------------------------------------------
scene 2 "First correction — operator teaches Victoria a missing rule"
echo "  Sarah Lim is a new customer asking for a quote — but no photos sent."
echo "  Victoria still proposes to send the quote (workflow default)."
echo "  Operator: \"hold and ask for photos first\""
send_case '{
  "workflow_slug": "quote_drafting",
  "mode": "sandbox",
  "payload": {
    "sandbox": true,
    "case_name": "scene2",
    "customer_name": "Sarah Lim",
    "project_summary": "Townhouse roof patch",
    "client_type": "new",
    "photos_complete": false
  }
}'
echo
echo "  → Reply on WhatsApp with something like: hold, ask for more photos"
prev_count=$(current_audit_count correction_received)
wait_for_reply "correction" correction_received
echo "  ➜ check WhatsApp — Victoria should reply: \"Got it — recorded your correction. (1 of 3 matches before I propose a rule)\""

# -----------------------------------------------------------------------------
scene 3 "Second matching correction"
echo "  Carlos Reyes: same situation — new customer, no photos."
send_case '{
  "workflow_slug": "quote_drafting",
  "mode": "sandbox",
  "payload": {
    "sandbox": true,
    "case_name": "scene3",
    "customer_name": "Carlos Reyes",
    "project_summary": "Re-flash a chimney leak",
    "client_type": "new",
    "photos_complete": false
  }
}'
echo
echo "  → Reply on WhatsApp again: hold and ask for photos"
prev_count=$(current_audit_count correction_received)
wait_for_reply "correction" correction_received
echo "  ➜ Victoria should reply: \"Got it. (2 of 3 matches — 1 more to go.)\""

# -----------------------------------------------------------------------------
scene 4 "Third correction → Victoria proposes a rule"
echo "  Priya Mehta: same situation."
send_case '{
  "workflow_slug": "quote_drafting",
  "mode": "sandbox",
  "payload": {
    "sandbox": true,
    "case_name": "scene4",
    "customer_name": "Priya Mehta",
    "project_summary": "Skylight install + flashing",
    "client_type": "new",
    "photos_complete": false
  }
}'
echo
echo "  → Reply once more: hold and ask for photos"
prev_count=$(current_audit_count correction_received)
wait_for_reply "correction" correction_received
echo "  ➜ Victoria should reply: \"That's 3 corrections matching this same case pattern. I'm flagging a new rule for your review...\""

# -----------------------------------------------------------------------------
scene 5 "Operator promotes the candidate — rule goes live"
echo "  Looking up the under_review candidate ..."
CAND=$(psql -d victoria_demo -tA -c "SELECT id FROM rule_candidates WHERE tenant_id='$T' AND status='under_review' AND recommended_action='hold_and_request_more_info' ORDER BY data->>'last_seen_at' DESC LIMIT 1" 2>/dev/null)
echo "  candidate=$CAND"
echo "  Promoting via /admin/candidates/{tenant}/{candidate}/promote ..."
curl -fsS -X POST "$ADDR/admin/candidates/$T/$CAND/promote" \
  -H "$ADMIN" \
  -H 'Content-Type: application/json' \
  -d '{"reviewer_id":"jesse@victoria","rationale":"three matching corrections from real operator"}' \
  | python3 -c '
import json,sys
d = json.load(sys.stdin)
print("  ValidatedRule  =", d["validated_rule"]["id"], "status=", d["validated_rule"]["status"])
print("  SkillVersion   =", d["skill_version"]["id"], "version=", d["skill_version"]["version"])
print("  rule_manifest  =", len(d["skill_version"]["rule_manifest"]), "rule(s)")
'

# -----------------------------------------------------------------------------
scene 6 "Victoria applies the learned rule on a new enquiry"
echo "  Aisha Patel: new customer, no photos. Watch the planned_action change."
send_case '{
  "workflow_slug": "quote_drafting",
  "mode": "sandbox",
  "payload": {
    "sandbox": true,
    "case_name": "scene6",
    "customer_name": "Aisha Patel",
    "project_summary": "Inspect leaky roof and quote",
    "client_type": "new",
    "photos_complete": false
  }
}'
echo
echo "  ➜ Notice: planned_action is now hold_and_request_more_info (not send_quote)."
echo "  ➜ On WhatsApp, Victoria should describe the *new* plan — \"hold and ask for more info first\"."
echo "  ➜ Reply 1 to approve. Demo done."
echo
echo "═════════════════════════════════════════════════════════════"
echo "Storyline complete. Audit timeline:"
psql -d victoria_demo -P pager=off -c "SELECT to_char(occurred_at, 'HH24:MI:SS') AS at, event_type FROM audit_events WHERE tenant_id='$T' ORDER BY occurred_at DESC LIMIT 15"
