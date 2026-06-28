#!/usr/bin/env bash
# Showcase 3 — Victoria refuses to learn the wrong thing
#
# Pitch: Victoria isn't credulous. When two operators give contradicting
# corrections on the same case pattern, she catches the conflict and surfaces
# it for senior review — instead of silently letting one bad correction
# poison the rule base.
#
# Prerequisite: showcase 1 has run and "hold_and_request_more_info" rule
# is already promoted for this tenant (the conflict is against that rule).

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
import json, sys
p = json.load(sys.stdin)["review_packet"]
print("  → packet_id={pid} planned={action}".format(pid=p["packet_id"], action=p["planned_action"]["type"]))
'
}

count_audit () { psql -d victoria_demo -tA -c "SELECT COUNT(*) FROM audit_events WHERE tenant_id='$T' AND event_type='$1'"; }

wait_for () {
  local what="$1" event="$2" base="$3"
  printf "\n  ⏳ waiting for %s ..." "$what"
  for i in $(seq 1 300); do
    n=$(count_audit "$event")
    if [[ "$n" -gt "$base" ]]; then printf " ✓\n"; return; fi
    sleep 2
  done
  printf " ✘ timeout\n" >&2; exit 1
}

cat <<'INTRO'
══════════════════════════════════════════════════════════════════════
  SHOWCASE 3 — Victoria refuses to learn the wrong thing
  When operators disagree, Victoria surfaces the conflict instead of
  silently overwriting an established rule.
══════════════════════════════════════════════════════════════════════
INTRO

# Check prereq: there should already be a hold_and_request_more_info rule
# active for this tenant (from Showcase 1).
PROMOTED=$(psql -d victoria_demo -tA -c "SELECT COUNT(*) FROM validated_rules WHERE tenant_id='$T' AND status='active' AND data->>'recommended_action'='hold_and_request_more_info'")
if [[ "$PROMOTED" -lt "1" ]]; then
  echo
  echo "  ✘ prerequisite missing: this showcase assumes Showcase 1 has run"
  echo "    and the hold_and_request_more_info rule is active for this tenant."
  echo "    Run scripts/showcase-1-teach-by-example.sh first."
  exit 1
fi

# ─────────────────────────────────────────────────────────────────────
scene 1 "Baseline — the previously-learned rule fires correctly"
narrator "From Showcase 1, Victoria knows: new customer + no photos → hold."
narrator "Yara Chen comes in, same pattern. Victoria proposes the right thing."
send_case '{
  "workflow_slug":"quote_drafting","mode":"sandbox",
  "payload":{"sandbox":true,"case_name":"showcase3-yara",
    "customer_name":"Yara Chen",
    "project_summary":"Roof tile replacement after hail damage",
    "client_type":"new","photos_complete":false}}'
narrator "planned_action should be hold_and_request_more_info — the rule fires."
prompt_for "Y"
base=$(count_audit approval_received); wait_for "approval" approval_received "$base"

# ─────────────────────────────────────────────────────────────────────
scene 2 "A second operator gives a contradicting instruction"
narrator "Jamal Reyes comes in — same case pattern (new customer, no photos)."
narrator "Victoria proposes hold (per the rule). But this time the operator,"
narrator "perhaps a different reviewer, replies with a CONFLICTING instruction:"
narrator "  'send it anyway with a disclaimer'"
narrator ""
narrator "This text triggers the structured parser to extract a 'send_quote'"
narrator "rule for the SAME conditions. Victoria now has two competing rules."
send_case '{
  "workflow_slug":"quote_drafting","mode":"sandbox",
  "payload":{"sandbox":true,"case_name":"showcase3-jamal",
    "customer_name":"Jamal Reyes",
    "project_summary":"Quick re-flash of a leaky vent",
    "client_type":"new","photos_complete":false}}'
prompt_for "N, send it anyway with a disclaimer"
base=$(count_audit candidate_contradiction_detected)
wait_for "contradiction detection" candidate_contradiction_detected "$base"
narrator "Victoria should reply with a contradiction alert:"
narrator "  '⚠️ Conflict detected.'"
narrator "  'For this same case pattern I'd been agreeing on hold_and_request_more_info'"
narrator "  '(N matching corrections so far). Your latest reply tells me to do send_quote'"
narrator "  'instead. I won't promote either rule until this is resolved.'"

# ─────────────────────────────────────────────────────────────────────
scene 3 "Show the audit trail of the conflict"
narrator "The contradiction is in the audit log — every event is immutable."
echo
echo "Conflict-related audit events (last 5):"
psql -d victoria_demo -P pager=off -c "
  SELECT to_char(occurred_at, 'HH24:MI:SS') AS at, event_type, ref_id
  FROM audit_events
  WHERE tenant_id='$T' AND event_type IN ('candidate_contradiction_detected','candidate_created','correction_received')
  ORDER BY occurred_at DESC LIMIT 5"

echo
echo "Candidates for this tenant — note the contradicting_count on the held rule:"
psql -d victoria_demo -P pager=off -c "
  SELECT id, recommended_action, status,
    data->>'evidence_count' AS evidence,
    data->>'contradicting_count' AS contradictions,
    data->>'confidence' AS confidence
  FROM rule_candidates
  WHERE tenant_id='$T'
    AND data->>'workflow_slug'='quote_drafting'
    AND data->>'decision_type'='send_or_hold'
  ORDER BY recommended_action"

# ─────────────────────────────────────────────────────────────────────
scene 4 "Senior reviewer chooses — Victoria honours the decision"
narrator "Investor takeaway: Victoria flagged the conflict instead of letting"
narrator "one operator silently overwrite a 3-correction rule."
narrator ""
narrator "In production this would route to the Rule Review Console (spec §7),"
narrator "where a senior reviewer chooses which rule wins or rejects both."
narrator ""
narrator "For the demo, the senior reviewer keeps the original rule (hold)"
narrator "by REJECTING the new send_quote candidate via the admin API:"

CONFLICTING=$(psql -d victoria_demo -tA -c "
  SELECT id FROM rule_candidates
  WHERE tenant_id='$T'
    AND recommended_action='send_quote'
    AND status='candidate'
    AND data->>'workflow_slug'='quote_drafting'
  ORDER BY data->>'last_seen_at' DESC LIMIT 1")

if [[ -n "$CONFLICTING" ]]; then
  echo "  conflicting candidate to reject: $CONFLICTING"
  echo "  (in production this is one click in the Review Console)"
  # Mark the conflicting candidate as rejected directly in the DB —
  # there's no /admin/candidates/{id}/reject endpoint yet (Phase 1 stub).
  psql -d victoria_demo -P pager=off -c "
    UPDATE rule_candidates
    SET status='rejected', data = jsonb_set(data, '{status}', '\"rejected\"')
    WHERE id='$CONFLICTING'"
  echo "  ✓ rejected"
fi

# ─────────────────────────────────────────────────────────────────────
scene 5 "Wrap-up"
echo
narrator "Without conflict detection, Victoria would have absorbed the"
narrator "contradicting correction as new evidence and eventually promoted"
narrator "two opposing rules — leading to inconsistent behavior on every case."
narrator ""
narrator "Instead:"
narrator "  • The original 3-correction rule (hold) stayed active"
narrator "  • The conflicting correction was visibly flagged"
narrator "  • A senior decision was made and audit-logged"
narrator "  • Victoria continues to behave consistently"
echo
echo "Active rules and their state:"
psql -d victoria_demo -P pager=off -c "
  SELECT id, status, data->>'recommended_action' AS action, data->>'workflow_slug' AS workflow
  FROM validated_rules
  WHERE tenant_id='$T' AND status='active'"
