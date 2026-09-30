#!/usr/bin/env bash
set -uo pipefail

bin="${ONEK_BIN:-onek}"
dir="${ONEK_DIR:-.}"
against="${ONEK_AGAINST:?ONEK_AGAINST is required}"
report="${REPORT_FILE:-onek-compat-report.md}"
marker="<!-- onek-compat -->"

args=(compat --json)
while IFS= read -r allowed; do
  allowed="${allowed#"${allowed%%[![:space:]]*}"}"
  allowed="${allowed%"${allowed##*[![:space:]]}"}"
  if [ -n "$allowed" ]; then
    args+=(--allow "$allowed")
  fi
done <<<"${ONEK_ALLOW:-}"
args+=(--against "$against" "$dir")

stderr_file="$(mktemp)"
findings="$("$bin" "${args[@]}" 2>"$stderr_file")"
code=$?

if [ "$code" -ne 0 ] && [ "$code" -ne 2 ]; then
  cat "$stderr_file" >&2
  rm -f "$stderr_file"
  exit 1
fi
rm -f "$stderr_file"

if [ -z "$findings" ] || [ "$findings" = "null" ]; then
  findings="[]"
fi
count="$(jq 'length' <<<"$findings")"

targets=""
if [ -f "$dir/onekit.toml" ]; then
  targets="$(grep -oE '^\[generate\.[A-Za-z0-9_-]+\]' "$dir/onekit.toml" | sed -E 's/^\[generate\.(.*)\]$/`\1`/' | paste -sd ',' - | sed 's/,/, /g')"
fi

{
  echo "$marker"
  echo "### OneKit schema compatibility"
  echo
  if [ "$count" -eq 0 ]; then
    echo "No breaking changes against \`$against\`."
  else
    echo "**$count breaking change(s)** against \`$against\`:"
    echo
    echo "| Path | Change |"
    echo "| --- | --- |"
    jq -r 'def cell: gsub("\\|"; "\\|"); .[] | "| `\(.path | cell)` | \(.message | cell)\(if .before and .after then " (`\(.before | cell)` -> `\(.after | cell)`)" else "" end) |"' <<<"$findings"
    echo
    if [ -n "$targets" ]; then
      echo "Generated targets that ship this contract: $targets. Existing deployed clients and servers built from the previous schema may break; regenerate and release them together."
      echo
    fi
    echo "To accept an intentional change, pass its path in the action's \`allow\` input."
  fi
} >"$report"

if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  cat "$report" >>"$GITHUB_STEP_SUMMARY"
fi
if [ -n "${GITHUB_OUTPUT:-}" ]; then
  {
    echo "breaking=$count"
    echo "report=$report"
  } >>"$GITHUB_OUTPUT"
fi

cat "$report"
