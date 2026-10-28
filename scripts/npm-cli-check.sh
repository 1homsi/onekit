#!/usr/bin/env bash
# Runs the @onkt/cli launcher in npm/onkt as the given release version: the
# first run downloads the release binary for this machine and checks it
# against checksums.txt, the second starts the cached copy. Set
# ONKT_CACHE_DIR to an empty directory so the download really happens.
#
#   scripts/npm-cli-check.sh <version>
set -euo pipefail

version=$1
cd "$(dirname "$0")/../npm/onkt"
npm version "$version" --no-git-tag-version --allow-same-version >/dev/null
for run in download cached; do
  got=$(node bin/onkt.js version)
  if [[ "$got" != "$version" ]]; then
    echo "onkt version printed '$got' on the $run run, want '$version'" >&2
    exit 1
  fi
  echo "$run run: onkt $got"
done
