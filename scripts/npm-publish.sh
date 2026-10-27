#!/usr/bin/env bash
# Publishes the npm package in <dir> under the release version, with
# provenance. A version that is already on npm is left alone, so a rerun of
# the release workflow picks up where it stopped. Prereleases go to the
# "next" dist-tag.
#
#   scripts/npm-publish.sh <dir> <version>
set -euo pipefail

dir=$1
version=$2
name=$(node -p "require('./$dir/package.json').name")
if npm view "$name@$version" version >/dev/null 2>&1; then
  echo "$name $version is already published"
  exit 0
fi
tag=latest
if [[ "$version" == *-* ]]; then
  tag=next
fi
cd "$dir"
npm version "$version" --no-git-tag-version --allow-same-version
npm publish --provenance --access public --tag "$tag"
