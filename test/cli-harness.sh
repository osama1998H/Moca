#!/usr/bin/env bash

set -euo pipefail

MOCA_BIN="${MOCA_BIN:-moca}"
TEST_PASS=0
TEST_FAIL=0
TEST_LOG="${TEST_LOG:-/tmp/cli-test-$$.log}"

record_pass() {
  echo "  ✓ $1"
  ((TEST_PASS+=1))
}

record_fail() {
  echo "  ✗ $1"
  ((TEST_FAIL+=1))
}

assert_exit_code() {
  local description="$1" expected="$2"
  shift 2

  local actual=0
  "$@" >>"$TEST_LOG" 2>&1 || actual=$?
  if [ "$actual" -eq "$expected" ]; then
    record_pass "$description (exit=$actual)"
  else
    record_fail "$description (expected exit=$expected, got exit=$actual)"
    echo "    Command: $*" 
    tail -20 "$TEST_LOG" | sed 's/^/    | /'
  fi
}

assert_success() {
  assert_exit_code "$1" 0 "${@:2}"
}

assert_failure() {
  assert_exit_code "$1" 1 "${@:2}"
}

assert_stdout_contains() {
  local description="$1" pattern="$2"
  shift 2

  local output
  output=$("$@" 2>/dev/null) || true
  if printf '%s\n' "$output" | grep -qE "$pattern"; then
    record_pass "$description (matched: $pattern)"
  else
    record_fail "$description (pattern '$pattern' not found)"
    printf '%s\n' "$output" | head -10 | sed 's/^/    | /'
  fi
}

assert_file_exists() {
  local description="$1" path="$2"
  if [ -e "$path" ]; then
    record_pass "$description ($path exists)"
  else
    record_fail "$description ($path missing)"
  fi
}

assert_json_field() {
  local description="$1" field="$2" expected="$3"
  shift 3

  local output actual
  output=$("$@" --json 2>/dev/null) || true
  actual=$(printf '%s\n' "$output" | jq -r "$field")
  if [ "$actual" = "$expected" ]; then
    record_pass "$description ($field = $expected)"
  else
    record_fail "$description ($field expected '$expected', got '$actual')"
    printf '%s\n' "$output" | head -20 | sed 's/^/    | /'
  fi
}

print_summary() {
  local total=$((TEST_PASS + TEST_FAIL))
  echo
  echo "═══════════════════════════════════════════════════"
  echo "  Results: $TEST_PASS/$total passed"
  if [ "$TEST_FAIL" -gt 0 ]; then
    echo "  ✗ $TEST_FAIL FAILURES"
    echo "  Full log: $TEST_LOG"
    echo "═══════════════════════════════════════════════════"
    exit 1
  fi

  echo "  ✓ All passed"
  echo "═══════════════════════════════════════════════════"
}
