#!/usr/bin/env bash
set -euo pipefail

marker="<!-- onek-compat -->"
report="${REPORT_FILE:?REPORT_FILE is required}"
pr="${PR_NUMBER:?PR_NUMBER is required}"
repo="${REPOSITORY:?REPOSITORY is required}"
breaking="${BREAKING:-0}"

existing="$(gh api --paginate "repos/$repo/issues/$pr/comments" --jq ".[] | select(.body | startswith(\"$marker\")) | .id" | head -n1)"

if [ -n "$existing" ]; then
  gh api --method PATCH "repos/$repo/issues/comments/$existing" -F "body=@$report" >/dev/null
  exit 0
fi
if [ "$breaking" = "0" ]; then
  exit 0
fi
gh api --method POST "repos/$repo/issues/$pr/comments" -F "body=@$report" >/dev/null
