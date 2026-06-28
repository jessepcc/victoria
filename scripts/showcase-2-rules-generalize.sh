#!/usr/bin/env bash
# Showcase 2 — One rule, multiple jurisdictions
#
# Pitch: rules don't memorize examples; they generalize. Three Singapore
# corrections produce one rule that also handles US suppliers correctly.
# Local AU invoices still get standard GST.

set -euo pipefail
ADDR="${VICTORIA_ADDR:-http://localhost:8080}"
T=$(cat /tmp/victoria-tenant-id.txt)
AUTH="Authorization: Bearer tid:$T"

scene () { printf "\n\n═══ Scene %s — %s ═══\n" "$1" "$2"; }
narrator () { printf "  📢 %s\n" "$1"; }
prompt_for () { printf "\n  📱 On WhatsApp, reply: %s\n" "$1"; }

send_invoice () {
  local supplier="$1" country="$2" amount="$3" name="$4"
  curl -fsS -X POST "$ADDR/cases" -H "$AUTH" -H 'Content-Type: application/json' -d "{
    \"workflow_slug\":\"invoice_handling\",\"mode\":\"sandbox\",
    \"payload\":{
      \"sandbox\":true,\"case_name\":\"$name\",
      \"supplier_name\":\"$supplier\",\"supplier_country\":\"$country\",
      \"invoice_amount\":\"$amount\"}}" \
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
  SHOWCASE 2 — One rule, multiple jurisdictions
  Three Singapore corrections produce a rule that also handles
  US suppliers correctly. AU invoices still get standard GST.
══════════════════════════════════════════════════════════════════════
INTRO

# ─────────────────────────────────────────────────────────────────────
scene 1 "Baseline — local AU invoice gets standard GST"
narrator "Mike's Manufacturing, AU supplier. Default tax_treatment is apply_gst."
send_invoice "Mike's Manufacturing" "AU" "1200" "showcase2-mike-au"
prompt_for "Y"
base=$(count_audit approval_received); wait_for "approval" approval_received "$base"

# ─────────────────────────────────────────────────────────────────────
scene 2 "First SG correction — overseas supplier shouldn't get GST"
narrator "Tan Plumbing — Singapore supplier. Victoria still proposes apply_gst."
send_invoice "Tan Plumbing Pte Ltd" "SG" "4250" "showcase2-tan-sg"
prompt_for "N, no gst — overseas supplier"
base=$(count_audit correction_received); wait_for "correction" correction_received "$base"
narrator "Victoria: '(1 of 3 matches before I propose a rule.)'"

# ─────────────────────────────────────────────────────────────────────
scene 3 "Second SG correction"
narrator "Lim Electrical — another SG supplier."
send_invoice "Lim Electrical Pte Ltd" "SG" "1880" "showcase2-lim-sg"
prompt_for "N, singapore supplier — no gst"
base=$(count_audit correction_received); wait_for "correction" correction_received "$base"
narrator "Victoria: '(2 of 3.)'"

# ─────────────────────────────────────────────────────────────────────
scene 4 "Third SG correction → Victoria proposes the rule"
narrator "Goh Hardware — third SG supplier in a row."
send_invoice "Goh Hardware Pte Ltd" "SG" "925" "showcase2-goh-sg"
prompt_for "N, no gst, singapore"
base=$(count_audit correction_received); wait_for "correction" correction_received "$base"
narrator "Victoria proposes the rule: when supplier_country != AU → apply_no_gst"

# ─────────────────────────────────────────────────────────────────────
scene 5 "Promote via WhatsApp"
prompt_for "promote"
base=$(count_audit rule_promoted); wait_for "promotion" rule_promoted "$base"
narrator "Rule active. SkillVersion bumped."

# ─────────────────────────────────────────────────────────────────────
scene 6 "New SG invoice — Victoria applies the new rule"
narrator "Watch the planned_action — should now be apply_no_gst."
send_invoice "Wong Concrete Pte Ltd" "SG" "6750" "showcase2-wong-sg"
prompt_for "Y"
base=$(count_audit approval_received); wait_for "approval" approval_received "$base"

# ─────────────────────────────────────────────────────────────────────
scene 7 "Now a US supplier — and Victoria correctly applies the SAME rule"
narrator "Brown Wholesale, US-based supplier."
narrator "We never trained Victoria on US suppliers — but the learned rule"
narrator "is conditional on supplier_country != AU, so it generalizes."
send_invoice "Brown Wholesale Co" "US" "3500" "showcase2-brown-us"
narrator "planned_action should still be apply_no_gst (rule generalized!)."
prompt_for "Y"
base=$(count_audit approval_received); wait_for "approval" approval_received "$base"

# ─────────────────────────────────────────────────────────────────────
scene 8 "Local AU invoice still gets standard GST — rule scope is correct"
narrator "Davis Sand Supplies, AU supplier. The new rule should NOT fire here."
send_invoice "Davis Sand Supplies" "AU" "880" "showcase2-davis-au"
narrator "planned_action should be apply_gst (workflow default for AU)."
prompt_for "Y"
base=$(count_audit approval_received); wait_for "approval" approval_received "$base"

# ─────────────────────────────────────────────────────────────────────
scene 9 "Wrap-up"
echo
narrator "Three Singapore corrections produced one rule that:"
narrator "  • correctly handles SG suppliers (apply_no_gst)"
narrator "  • generalizes to US suppliers — never explicitly trained on (apply_no_gst)"
narrator "  • doesn't over-fire on AU suppliers (still apply_gst)"
echo
echo "Active rules for this tenant:"
psql -d victoria_demo -P pager=off -c "SELECT id, status, scope, data->>'recommended_action' AS action, data->>'workflow_slug' AS workflow FROM validated_rules WHERE tenant_id='$T' AND status='active'"
