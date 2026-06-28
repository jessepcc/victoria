#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
TENANT_ID="${TENANT_ID:-}"
INTERVAL_SECONDS="${INTERVAL_SECONDS:-30}"
CHANNEL="${CHANNEL:-simulator}"

if [[ -z "$TENANT_ID" ]]; then
  echo "TENANT_ID is required" >&2
  echo "usage: TENANT_ID=t_... BASE_URL=http://localhost:8080 INTERVAL_SECONDS=30 $0" >&2
  exit 2
fi

names=("Grace Wong" "Daniel Lee" "Mei Chan" "Alex Patel" "Sarah Ng" "Ben Roberts")
projects=("roof leak after the rain" "bathroom renovation" "shopfront repaint" "office partition repair" "invoice question" "urgent quote request")
channels=("email" "telegram")

count=0
while true; do
  count=$((count + 1))
  name="${names[$((RANDOM % ${#names[@]}))]}"
  project="${projects[$((RANDOM % ${#projects[@]}))]}"
  source_channel="${channels[$((RANDOM % ${#channels[@]}))]}"
  source_id="${CHANNEL}-${source_channel}-$(date +%s)-${count}"
  customer="$(echo "$name" | tr '[:upper:] ' '[:lower:].')@example.test"
  received_at="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"

  body="Hi, this is ${name}. I need help with a ${project}. Can you let me know the next step?"

  curl -fsS \
    -H "Authorization: Bearer tid:${TENANT_ID}" \
    -H "Content-Type: application/json" \
    -X POST "${BASE_URL}/ingest/customer-message" \
    -d "{
      \"channel\": \"${source_channel}\",
      \"source_message_id\": \"${source_id}\",
      \"customer_identifier\": \"${customer}\",
      \"received_at\": \"${received_at}\",
      \"subject\": \"${project}\",
      \"body_text\": \"${body}\",
      \"metadata\": {\"simulator\": true, \"sequence\": ${count}}
    }"
  echo
  sleep "${INTERVAL_SECONDS}"
done
