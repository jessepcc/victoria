#!/usr/bin/env bash
# Showcase 1 — Teach by example
#
# Pitch: Victoria learns a new business rule from operator corrections via
# WhatsApp. No engineering, no admin UI, no forms. Watch the operator teach
# Victoria a "don't quote without photos" policy in 4 messages, then watch
# Victoria apply it on the next case.
#
# Setup: paired tenant in /tmp/victoria-tenant-id.txt, server on :8080.

set -euo pipefail
ADDR="${VICTORIA_ADDR:-http://localhost:8080}"
T=$(cat /tmp/victoria-tenant-id.txt)
AUTH="Authorization: Bearer tid:$T"

scene () { printf "\n\n═══ Scene %s — %s ═══\n" "$1" "$2"; }
narrator () { printf "  📢 %s\n" "$1"; }
prompt_for () { printf "\n  📱 On WhatsApp, reply: %s\n" "$1"; }

send_case () {
  curl -fsS -X POST "$ADDR/cases" -H "$AUTH" -H 'Content-Type: application/json' -d "$1" \
  | python3 -c '
import json,sys
d = json.load(sys.stdin)
p = d["review_packet"]
print(f"  → packet_id={p[\"packet_id\"]} planned={p[\"planned_action\"][\"type\"]}")
'
}

wait_for () {
  local what="$1" event="$2" base="$3"
  printf "\n  ⏳ waiting for %s ..." "$what"
  for i in $(seq 1 60); do
    n=$(psql -d victoria_demo -tA -c "SELECT COUNT(*) FROM audit_events WHERE tenant_id='$T' AND event_type='$event'")
    if [[ "$n" -gt "$base" ]]; then printf " ✓\n"; return; fi
    sleep 2
  done
  printf " ✘ timeout\n" >&2; exit 1
}

count_audit () { psql -d victoria_demo -tA -c "SELECT COUNT(*) FROM audit_events WHERE tenant_id='$T' AND event_type='$1'"; }

cat <<'INTRO'
══════════════════════════════════════════════════════════════════════
  SHOWCASE 1 — Teach by example
  Victoria learns a new business rule in real time, via WhatsApp.
══════════════════════════════════════════════════════════════════════
INTRO

# ─────────────────────────────────────────────────────────────────────
scene 1 "Baseline — Victoria already gets it right"
narrator "Mike's Garage is a regular customer; their photos look complete."
narrator "Victoria's default behaviour for quote_drafting is to send a quote."
narrator "Operator just confirms with a single tap."
send_case '{
  "workflow_slug":"quote_drafting","mode":"sandbox",
  "payload":{"sandbox":true,"case_name":"showcase1-mike",
    "customer_name":"Mike'\''s Garage",
    "project_summary":"Repaint of garage roof, ~80 sqm",
    "client_type":"repeat","photos_complete":true}}'
prompt_for "Y"
base=$(count_audit approval_received); wait_for "your approval" approval_received "$base"

# ─────────────────────────────────────────────────────────────────────
scene 2 "First correction — operator teaches Victoria a missing rule"
narrator "Sarah Lim is a NEW customer who didn't send photos."
narrator "Victoria still proposes the standard quote — that's her current logic."
narrator "Operator wants to push back: don't quote without photos."
send_case '{
  "workflow_slug":"quote_drafting","mode":"sandbox",
  "payload":{"sandbox":true,"case_name":"showcase1-sarah",
    "customer_name":"Sarah Lim",
    "project_summary":"Townhouse roof patch",
    "client_type":"new","photos_complete":false}}'
prompt_for "N, hold and ask for photos"
base=$(count_audit correction_received); wait_for "your correction" correction_received "$base"
narrator "Victoria should reply: 'Got it — recorded your correction. (1 of 3 matches before I propose a rule.)'"

# ─────────────────────────────────────────────────────────────────────
scene 3 "Second matching correction"
narrator "Carlos Reyes — same situation, new customer, no photos."
send_case '{
  "workflow_slug":"quote_drafting","mode":"sandbox",
  "payload":{"sandbox":true,"case_name":"showcase1-carlos",
    "customer_name":"Carlos Reyes",
    "project_summary":"Re-flash a chimney leak",
    "client_type":"new","photos_complete":false}}'
prompt_for "N, ask for photos first"
base=$(count_audit correction_received); wait_for "your correction" correction_received "$base"
narrator "Victoria should reply: '(2 of 3 — 1 more to go.)'"

# ─────────────────────────────────────────────────────────────────────
scene 4 "Third correction → Victoria proposes a new rule"
narrator "Priya Mehta — same case pattern. This is the threshold-crossing one."
send_case '{
  "workflow_slug":"quote_drafting","mode":"sandbox",
  "payload":{"sandbox":true,"case_name":"showcase1-priya",
    "customer_name":"Priya Mehta",
    "project_summary":"Skylight install + flashing",
    "client_type":"new","photos_complete":false}}'
prompt_for "N, ask for photos"
base=$(count_audit correction_received); wait_for "your correction" correction_received "$base"
narrator "Victoria should reply with the rule proposal:"
narrator "  '🔔 That's 3 corrections matching this same case pattern.'"
narrator "  'I'm flagging a new rule for your review: hold_and_request_more_info.'"
narrator "  'Reply *promote* when ready, and I'll apply it from here on.'"

# ─────────────────────────────────────────────────────────────────────
scene 5 "Operator promotes — entirely from WhatsApp, no curl"
prompt_for "promote"
base=$(count_audit rule_promoted); wait_for "rule promotion" rule_promoted "$base"
narrator "Victoria should confirm: '✅ Rule promoted. SkillVersion bumped to v2.'"

# ─────────────────────────────────────────────────────────────────────
scene 6 "Victoria applies the new rule on the very next case"
narrator "Aisha Patel — new customer, no photos. SAME pattern as Sarah/Carlos/Priya."
narrator "Watch the planned_action: it should now be hold_and_request_more_info."
send_case '{
  "workflow_slug":"quote_drafting","mode":"sandbox",
  "payload":{"sandbox":true,"case_name":"showcase1-aisha",
    "customer_name":"Aisha Patel",
    "project_summary":"Inspect leaky roof and quote",
    "client_type":"new","photos_complete":false}}'
narrator "Victoria's WhatsApp message should now say: 'I'm planning to: hold and ask for more info first'."
prompt_for "Y"
base=$(count_audit approval_received); wait_for "approval of the learned behavior" approval_received "$base"

# ─────────────────────────────────────────────────────────────────────
scene 7 "Wrap-up — what just happened"
echo
narrator "In 5 WhatsApp messages, the operator taught Victoria a new business rule:"
narrator "  → 4 corrections in normal conversational tone"
narrator "  → 1 'promote' command"
narrator "Now Victoria handles every future enquiry of this kind correctly."
echo
echo "Audit timeline (last 12 events):"
psql -d victoria_demo -P pager=off -c "SELECT to_char(occurred_at, 'HH24:MI:SS') AS at, event_type FROM audit_events WHERE tenant_id='$T' AND occurred_at > NOW() - INTERVAL '15 minutes' ORDER BY occurred_at DESC LIMIT 12"
