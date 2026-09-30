#!/usr/bin/env bash
set -euo pipefail

repo="${ONEK_REPO:-1homsi/onekit}"
version="${ONEK_VERSION:-latest}"
dest="${ONEK_INSTALL_DIR:-${RUNNER_TEMP:-/tmp}/onek-bin}"

case "$(uname -s)" in
  Linux) os="Linux" ;;
  Darwin) os="Darwin" ;;
  *) echo "unsupported runner OS: $(uname -s)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch="x86_64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

if [ "$version" = "latest" ]; then
  tag="$(gh release view --repo "$repo" --json tagName -q .tagName)"
else
  tag="v${version#v}"
fi

asset="onekit_${tag#v}_${os}_${arch}.tar.gz"
work="$(mktemp -d)"
gh release download "$tag" --repo "$repo" --dir "$work" --pattern "$asset" --pattern checksums.txt

expected="$(awk -v f="$asset" '$2 == f { print $1 }' "$work/checksums.txt")"
if [ -z "$expected" ]; then
  echo "no checksum published for $asset" >&2
  exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$work/$asset" | awk '{print $1}')"
else
  actual="$(shasum -a 256 "$work/$asset" | awk '{print $1}')"
fi
if [ "$expected" != "$actual" ]; then
  echo "checksum mismatch for $asset" >&2
  exit 1
fi

mkdir -p "$dest"
tar -xzf "$work/$asset" -C "$dest"
chmod +x "$dest/onek"
rm -rf "$work"

if [ -n "${GITHUB_PATH:-}" ]; then
  echo "$dest" >>"$GITHUB_PATH"
fi
echo "installed onek $tag to $dest"
