#!/usr/bin/env bash
# Run an OpenCuttles device test headlessly (CI-friendly).
#
# Usage:
#   OPENCUTTLES_URL=http://host OPENCUTTLES_SESSION=<cookie-value> \
#     bash scripts/run-test.sh <test-id> <instance-id>
#
# Exits 0 if the run passed, 1 if it failed, and prints the report URL.
set -euo pipefail

BASE="${OPENCUTTLES_URL:?set OPENCUTTLES_URL}"
COOKIE="opencuttles_session=${OPENCUTTLES_SESSION:?set OPENCUTTLES_SESSION}"
TEST_ID="${1:?usage: run-test.sh <test-id> <instance-id>}"
INSTANCE_ID="${2:?usage: run-test.sh <test-id> <instance-id>}"

run_id=$(curl -fsS -X POST "$BASE/api/v1/tests/$TEST_ID/run" \
  -H "Cookie: $COOKIE" -H 'Content-Type: application/json' \
  -d "{\"instanceId\":\"$INSTANCE_ID\"}" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
echo "run: $run_id"

for _ in $(seq 1 180); do
  body=$(curl -fsS "$BASE/api/v1/tests/runs/$run_id" -H "Cookie: $COOKIE")
  status=$(echo "$body" | sed -n 's/.*"status":"\([a-z]*\)".*/\1/p')
  if [[ "$status" != "running" ]]; then
    echo "status: $status"
    echo "report: $BASE/#run-$run_id"
    [[ "$status" == "passed" ]] && exit 0 || exit 1
  fi
  sleep 5
done
echo "timed out waiting for run $run_id" >&2
exit 1
