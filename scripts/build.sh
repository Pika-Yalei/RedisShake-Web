#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
target_os="${1:-$(go env GOOS)}"
target_arch="${2:-$(go env GOARCH)}"
output_dir="${3:-$repo_dir/dist/redis-shake-web-${target_os}-${target_arch}}"
source_sha256="c6c7de3a76b7bf4f18e494ffb40f1d09287622c4574d4da41fc1244a2dcc0b3f"
source_url="https://codeload.github.com/tair-opensource/RedisShake/tar.gz/refs/tags/v4.6.2"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

curl --fail --location --silent --show-error "$source_url" -o "$tmp_dir/source.tar.gz"
actual_sha256="$(openssl dgst -sha256 "$tmp_dir/source.tar.gz" | awk '{print $NF}')"
if [[ "$actual_sha256" != "$source_sha256" ]]; then
  echo "RedisShake source digest mismatch" >&2
  exit 1
fi
mkdir -p "$tmp_dir/source" "$output_dir"
tar -xzf "$tmp_dir/source.tar.gz" -C "$tmp_dir/source" --strip-components=1
( cd "$tmp_dir/source" && git apply --check "$repo_dir/patches/redis-shake-v4.6.2.patch" && git apply "$repo_dir/patches/redis-shake-v4.6.2.patch" && CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" go build -trimpath -o "$output_dir/redis-shake" ./cmd/redis-shake )
( cd "$repo_dir" && CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" go build -trimpath -o "$output_dir/redis-shake-web" ./cmd/redisshakeweb )
cp "$repo_dir/README.md" "$output_dir/README.md"
mkdir -p "$output_dir/patches"
cp -R "$repo_dir/docs" "$output_dir/docs"
cp "$repo_dir/patches/redis-shake-v4.6.2.patch" "$output_dir/patches/redis-shake-v4.6.2.patch"
printf 'RedisShake source: v4.6.2\nCommit: f20f28e6f2679e71a213904d2c74ceb521e19551\nSource SHA-256: %s\nPatch: patches/redis-shake-v4.6.2.patch\n' "$source_sha256" > "$output_dir/VERSIONS.txt"
echo "Built $output_dir"
