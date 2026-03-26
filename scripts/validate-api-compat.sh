#!/usr/bin/env bash
# validate-api-compat.sh — Compare Go vs TypeScript brain-api responses side-by-side.
#
# Usage:
#   ./scripts/validate-api-compat.sh [GO_URL] [TS_URL]
#
# Defaults:
#   GO_URL = http://localhost:3000
#   TS_URL = http://localhost:3333
#
# Prerequisites:
#   - Both servers running
#   - curl and jq installed

set -euo pipefail

GO_URL="${1:-http://localhost:3000}"
TS_URL="${2:-http://localhost:3333}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m'

PASS=0
FAIL=0
WARN=0

log_pass() { echo -e "  ${GREEN}✓${NC} $1"; ((PASS++)); }
log_fail() { echo -e "  ${RED}✗${NC} $1"; ((FAIL++)); }
log_warn() { echo -e "  ${YELLOW}⚠${NC} $1"; ((WARN++)); }
log_section() { echo ""; echo -e "${BLUE}━━━ $1 ━━━${NC}"; }

compare_status() {
    local endpoint="$1" method="${2:-GET}" body="${3:-}"
    local go_status ts_status
    if [ "$method" = "GET" ]; then
        go_status=$(curl -s -o /dev/null -w "%{http_code}" "${GO_URL}/api/v1${endpoint}")
        ts_status=$(curl -s -o /dev/null -w "%{http_code}" "${TS_URL}/api/v1${endpoint}")
    elif [ "$method" = "POST" ]; then
        go_status=$(curl -s -o /dev/null -w "%{http_code}" -X POST -H "Content-Type: application/json" -d "$body" "${GO_URL}/api/v1${endpoint}")
        ts_status=$(curl -s -o /dev/null -w "%{http_code}" -X POST -H "Content-Type: application/json" -d "$body" "${TS_URL}/api/v1${endpoint}")
    elif [ "$method" = "DELETE" ]; then
        go_status=$(curl -s -o /dev/null -w "%{http_code}" -X DELETE "${GO_URL}/api/v1${endpoint}")
        ts_status=$(curl -s -o /dev/null -w "%{http_code}" -X DELETE "${TS_URL}/api/v1${endpoint}")
    elif [ "$method" = "PATCH" ]; then
        go_status=$(curl -s -o /dev/null -w "%{http_code}" -X PATCH -H "Content-Type: application/json" -d "$body" "${GO_URL}/api/v1${endpoint}")
        ts_status=$(curl -s -o /dev/null -w "%{http_code}" -X PATCH -H "Content-Type: application/json" -d "$body" "${TS_URL}/api/v1${endpoint}")
    fi
    if [ "$go_status" = "$ts_status" ]; then
        log_pass "$method $endpoint → $go_status (both)"
    else
        log_fail "$method $endpoint → Go=$go_status, TS=$ts_status"
    fi
}

compare_fields() {
    local endpoint="$1" method="${2:-GET}" body="${3:-}" label="${4:-$method $endpoint}"
    local go_resp ts_resp
    if [ "$method" = "GET" ]; then
        go_resp=$(curl -s "${GO_URL}/api/v1${endpoint}")
        ts_resp=$(curl -s "${TS_URL}/api/v1${endpoint}")
    elif [ "$method" = "POST" ]; then
        go_resp=$(curl -s -X POST -H "Content-Type: application/json" -d "$body" "${GO_URL}/api/v1${endpoint}")
        ts_resp=$(curl -s -X POST -H "Content-Type: application/json" -d "$body" "${TS_URL}/api/v1${endpoint}")
    fi
    local go_keys ts_keys
    go_keys=$(echo "$go_resp" | jq -r 'keys[]' 2>/dev/null | sort)
    ts_keys=$(echo "$ts_resp" | jq -r 'keys[]' 2>/dev/null | sort)
    if [ "$go_keys" = "$ts_keys" ]; then
        log_pass "$label — fields match: $(echo "$go_keys" | tr '\n' ', ')"
    else
        local go_only ts_only
        go_only=$(comm -23 <(echo "$go_keys") <(echo "$ts_keys") | tr '\n' ', ')
        ts_only=$(comm -13 <(echo "$go_keys") <(echo "$ts_keys") | tr '\n' ', ')
        [ -n "$go_only" ] && log_warn "$label — Go-only fields: $go_only"
        [ -n "$ts_only" ] && log_warn "$label — TS-only fields: $ts_only"
        [ -z "$go_only" ] && [ -z "$ts_only" ] && log_pass "$label — fields match"
    fi
}

# --- Connectivity ---
log_section "Connectivity"
if curl -s -o /dev/null -w "%{http_code}" "${GO_URL}/api/v1/health" | grep -q "200"; then
    log_pass "Go server reachable at ${GO_URL}"
else
    log_fail "Go server NOT reachable at ${GO_URL}"
    echo "Start the Go server first: go run ./cmd/brain-api"
    exit 1
fi
if curl -s -o /dev/null -w "%{http_code}" "${TS_URL}/api/v1/health" | grep -q "200"; then
    log_pass "TS server reachable at ${TS_URL}"
else
    log_fail "TS server NOT reachable at ${TS_URL}"
    echo "Start the TS server first: bun run dev"
    exit 1
fi

# --- Health ---
log_section "Health"
compare_status "/health"
compare_fields "/health" "GET" "" "GET /health"

# --- Create Entry ---
log_section "Create Entry"
CREATE_BODY='{"type":"plan","title":"Compat Test Plan","content":"Testing API compatibility","tags":["test","compat"]}'
go_create=$(curl -s -X POST -H "Content-Type: application/json" -d "$CREATE_BODY" "${GO_URL}/api/v1/entries")
ts_create=$(curl -s -X POST -H "Content-Type: application/json" -d "$CREATE_BODY" "${TS_URL}/api/v1/entries")
go_id=$(echo "$go_create" | jq -r '.id')
ts_id=$(echo "$ts_create" | jq -r '.id')
[ -n "$go_id" ] && [ "$go_id" != "null" ] && log_pass "Go created entry: $go_id" || log_fail "Go failed to create entry"
[ -n "$ts_id" ] && [ "$ts_id" != "null" ] && log_pass "TS created entry: $ts_id" || log_fail "TS failed to create entry"
go_create_keys=$(echo "$go_create" | jq -r 'keys[]' | sort)
ts_create_keys=$(echo "$ts_create" | jq -r 'keys[]' | sort)
[ "$go_create_keys" = "$ts_create_keys" ] && log_pass "Create response fields match" || log_warn "Create response fields differ"

# --- Get Entry ---
log_section "Get Entry"
[ -n "$go_id" ] && [ "$go_id" != "null" ] && compare_status "/entries/$go_id"
[ -n "$ts_id" ] && [ "$ts_id" != "null" ] && {
    ts_get_keys=$(curl -s "${TS_URL}/api/v1/entries/$ts_id" | jq -r 'keys[]' | sort | tr '\n' ',')
    log_pass "TS GET /entries/$ts_id — fields: $ts_get_keys"
}

# --- List Entries ---
log_section "List Entries"
compare_status "/entries"
compare_fields "/entries" "GET" "" "GET /entries"

# --- Search ---
log_section "Search"
SEARCH_BODY='{"query":"Compat"}'
compare_status "/search" "POST" "$SEARCH_BODY"
compare_fields "/search" "POST" "$SEARCH_BODY" "POST /search"

# --- Inject ---
log_section "Inject"
INJECT_BODY='{"query":"test"}'
compare_status "/inject" "POST" "$INJECT_BODY"
compare_fields "/inject" "POST" "$INJECT_BODY" "POST /inject"

# --- Stats ---
log_section "Stats"
compare_status "/stats"
compare_fields "/stats" "GET" "" "GET /stats"

# --- Orphans ---
log_section "Orphans"
compare_status "/orphans"

# --- Stale ---
log_section "Stale"
compare_status "/stale?days=30"

# --- Link Generation ---
log_section "Link Generation"
if [ -n "$go_id" ] && [ "$go_id" != "null" ]; then
    go_path=$(curl -s "${GO_URL}/api/v1/entries/$go_id" | jq -r '.path')
    LINK_BODY="{\"path\":\"$go_path\"}"
    go_link_status=$(curl -s -o /dev/null -w "%{http_code}" -X POST -H "Content-Type: application/json" -d "$LINK_BODY" "${GO_URL}/api/v1/link")
    log_pass "Go POST /link → $go_link_status"
fi
if [ -n "$ts_id" ] && [ "$ts_id" != "null" ]; then
    ts_path=$(curl -s "${TS_URL}/api/v1/entries/$ts_id" | jq -r '.path')
    LINK_BODY="{\"path\":\"$ts_path\"}"
    ts_link_status=$(curl -s -o /dev/null -w "%{http_code}" -X POST -H "Content-Type: application/json" -d "$LINK_BODY" "${TS_URL}/api/v1/link")
    log_pass "TS POST /link → $ts_link_status"
fi

# --- Validation Errors ---
log_section "Validation Errors"
INVALID_BODY='{"title":"No Type"}'
compare_status "/entries" "POST" "$INVALID_BODY"
go_err=$(curl -s -X POST -H "Content-Type: application/json" -d "$INVALID_BODY" "${GO_URL}/api/v1/entries")
ts_err=$(curl -s -X POST -H "Content-Type: application/json" -d "$INVALID_BODY" "${TS_URL}/api/v1/entries")
go_err_keys=$(echo "$go_err" | jq -r 'keys[]' | sort)
ts_err_keys=$(echo "$ts_err" | jq -r 'keys[]' | sort)
[ "$go_err_keys" = "$ts_err_keys" ] && log_pass "Error response fields match" || log_warn "Error response fields differ"

# --- 404 Handling ---
log_section "404 Handling"
compare_status "/entries/nonexistent"
compare_status "/nonexistent"

# --- Delete (known difference: Go=204, TS=200) ---
log_section "Delete Entry"
if [ -n "$go_id" ] && [ "$go_id" != "null" ]; then
    go_del_status=$(curl -s -o /dev/null -w "%{http_code}" -X DELETE "${GO_URL}/api/v1/entries/${go_id}?confirm=true")
    log_pass "Go DELETE → $go_del_status"
fi
if [ -n "$ts_id" ] && [ "$ts_id" != "null" ]; then
    ts_del_status=$(curl -s -o /dev/null -w "%{http_code}" -X DELETE "${TS_URL}/api/v1/entries/${ts_id}?confirm=true")
    log_pass "TS DELETE → $ts_del_status"
fi
if [ -n "${go_del_status:-}" ] && [ -n "${ts_del_status:-}" ] && [ "$go_del_status" != "$ts_del_status" ]; then
    log_warn "DELETE status differs: Go=$go_del_status, TS=$ts_del_status (known: Go=204, TS=200)"
fi

# --- Tasks ---
log_section "Tasks"
compare_status "/tasks"

# --- Summary ---
echo ""
echo -e "${BLUE}━━━ Summary ━━━${NC}"
echo -e "  ${GREEN}Passed:${NC}  $PASS"
echo -e "  ${RED}Failed:${NC}  $FAIL"
echo -e "  ${YELLOW}Warnings:${NC} $WARN"
echo ""
if [ "$FAIL" -gt 0 ]; then
    echo -e "${RED}API compatibility check FAILED${NC}"
    exit 1
else
    echo -e "${GREEN}API compatibility check PASSED${NC}"
    exit 0
fi
