#!/usr/bin/env bash
# format-bench-comment.sh — Parse benchstat output into a structured PR comment.
#
# Usage: ./format-bench-comment.sh <benchstat-output> <pr-bench-raw>
#   $1 = path to benchstat comparison output
#   $2 = path to PR branch raw benchmark output (for budget proximity)
#
# Outputs: Markdown to stdout

set -euo pipefail

BENCHSTAT_FILE="${1:?Usage: format-bench-comment.sh <benchstat-output> <pr-bench-raw>}"
PR_RAW_FILE="${2:?Usage: format-bench-comment.sh <benchstat-output> <pr-bench-raw>}"

# ── Tier mapping ──────────────────────────────────────────────────────────────
tier_for() {
  local name="$1"
  case "$name" in
    RegistryGet*|DocManagerGet*|DocManagerGetList*|DocManagerInsert_Simple*|DocManagerInsert_Complex*|DocManagerInsert_Parallel*|QueryBuilderBuild*|GatewayHandler*)
      echo "1";;
    ValidateDoc*|NamingEngine*|DispatchEvent*|HookRegistry*|RateLimiter*|TransformerChain*|WithTransaction*)
      echo "2";;
    PGRoundTrip*|RedisGetSet*|PoolSaturation*|GenerateTableDDL*|Compile*|DocManagerInsert_Concurrency*)
      echo "3";;
    *)
      echo "0";;
  esac
}

tier_label() {
  case "$1" in
    1) echo "Tier 1 — Critical Hot Path";;
    2) echo "Tier 2 — Per-Request Components";;
    3) echo "Tier 3 — Infrastructure";;
    *) echo "Other";;
  esac
}

status_icon() {
  local delta="$1"
  local num sign
  num=$(echo "$delta" | sed 's/[+%]//g; s/^-//')
  sign=$(echo "$delta" | head -c1)

  if [ "$sign" = "-" ]; then
    echo ":green_circle:"
  elif awk "BEGIN{exit(!($num >= 10))}"; then
    echo ":red_circle:"
  elif awk "BEGIN{exit(!($num >= 5))}"; then
    echo ":yellow_circle:"
  else
    echo ":white_circle:"
  fi
}

# ── Budget definitions (ns/op) ────────────────────────────────────────────────
declare -A BUDGETS=(
  ["RegistryGet_L1Hit"]=200
  ["GenerateTableDDL/Fields_10"]=50000
  ["TransformerChain_Response"]=20000
  ["HookRegistryResolve_10Hooks"]=5000
)

# ── Parse benchstat for time/op changes ───────────────────────────────────────
declare -A TIER1_ROWS=()
declare -A TIER2_ROWS=()
declare -A TIER3_ROWS=()
declare -A OTHER_ROWS=()
has_changes=false

while IFS= read -r line; do
  # Skip headers, blanks, unchanged (~), alloc lines
  [[ "$line" =~ ^name ]] && continue
  [[ -z "$line" ]] && continue
  [[ "$line" =~ "~" ]] && continue
  [[ "$line" =~ "B/op" ]] && continue
  [[ "$line" =~ "allocs/op" ]] && continue
  # Only process lines with time/op data (contain ns, µs, or ms)
  [[ "$line" =~ (ns|µs|ms) ]] || continue

  bench_name=$(echo "$line" | awk '{print $1}' | sed 's/-[0-9]*$//')
  old_val=$(echo "$line" | awk '{print $2}')
  new_val=$(echo "$line" | awk '{print $4}')
  delta=$(echo "$line" | grep -oE '[+-][0-9]+\.[0-9]+%' | head -1)

  [ -z "$delta" ] && continue
  has_changes=true

  icon=$(status_icon "$delta")
  tier=$(tier_for "$bench_name")
  row="| ${icon} | \`${bench_name}\` | ${old_val} | ${new_val} | ${delta} |"

  case "$tier" in
    1) TIER1_ROWS["$bench_name"]="$row";;
    2) TIER2_ROWS["$bench_name"]="$row";;
    3) TIER3_ROWS["$bench_name"]="$row";;
    *) OTHER_ROWS["$bench_name"]="$row";;
  esac
done < "$BENCHSTAT_FILE"

# ── Parse PR raw output for budget proximity ──────────────────────────────────
declare -A BUDGET_CURRENT
while IFS= read -r line; do
  [[ "$line" =~ ^Benchmark ]] || continue
  for budget_name in "${!BUDGETS[@]}"; do
    if [[ "$line" == *"$budget_name"* ]]; then
      ns=$(echo "$line" | grep -oE '[0-9]+(\.[0-9]+)? ns/op' | head -1 | awk '{print $1}')
      [ -n "$ns" ] && BUDGET_CURRENT["$budget_name"]="$ns"
    fi
  done
done < "$PR_RAW_FILE"

# ── Output markdown ───────────────────────────────────────────────────────────
echo "## Benchmark Results"
echo ""

if [ "$has_changes" != true ]; then
  echo "> All benchmarks within noise margin. No statistically significant changes detected."
  echo ""
else
  print_tier_table() {
    local -n rows=$1
    local tier_num=$2
    [ ${#rows[@]} -eq 0 ] && return
    echo "### $(tier_label "$tier_num")"
    echo ""
    echo "| Status | Benchmark | Base | PR | Delta |"
    echo "|--------|-----------|------|----|-------|"
    for key in "${!rows[@]}"; do
      echo "${rows[$key]}"
    done
    echo ""
  }

  print_tier_table TIER1_ROWS 1
  print_tier_table TIER2_ROWS 2
  print_tier_table TIER3_ROWS 3
  print_tier_table OTHER_ROWS 0
fi

# Budget proximity
budget_found=false
for budget_name in "${!BUDGETS[@]}"; do
  if [ -n "${BUDGET_CURRENT[$budget_name]:-}" ]; then
    budget_found=true
    break
  fi
done

if [ "$budget_found" = true ]; then
  echo "### Performance Budgets"
  echo ""
  echo "| Benchmark | Current | Budget | Used |"
  echo "|-----------|---------|--------|------|"
  for budget_name in "${!BUDGETS[@]}"; do
    current="${BUDGET_CURRENT[$budget_name]:-}"
    [ -z "$current" ] && continue
    budget_ns="${BUDGETS[$budget_name]}"
    # Format budget display
    if [ "$budget_ns" -ge 1000 ]; then
      budget_display="$(awk "BEGIN{printf \"%.1f\", $budget_ns / 1000}") us/op"
    else
      budget_display="${budget_ns} ns/op"
    fi
    # Format current display
    current_int=$(echo "$current" | awk '{printf "%d", $1}')
    if [ "$current_int" -ge 1000 ]; then
      current_display="$(awk "BEGIN{printf \"%.1f\", $current / 1000}") us/op"
    else
      current_display="${current} ns/op"
    fi
    pct=$(awk "BEGIN{printf \"%.0f\", ($current / $budget_ns) * 100}")
    echo "| \`${budget_name}\` | ${current_display} | ${budget_display} | ${pct}% |"
  done
  echo ""
fi

# Raw output
echo "<details>"
echo "<summary>Full benchstat output</summary>"
echo ""
echo '```'
cat "$BENCHSTAT_FILE"
echo '```'
echo ""
echo "</details>"
