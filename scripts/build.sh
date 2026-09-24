#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
target_os="${1:-$(go env GOOS)}"
target_arch="${2:-$(go env GOARCH)}"
output_dir="${3:-$repo_dir/dist/redis-shake-web-${target_os}-${target_arch}}"
source_sha256="c6c7de3a76b7bf4f18e494ffb40f1d09287622c4574d4da41fc1244a2dcc0b3f"
mkdir -p "$output_dir"
( cd "$repo_dir/third_party/redis-shake" && CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" go build -trimpath -o "$output_dir/redis-shake" ./cmd/redis-shake )
( cd "$repo_dir" && CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" go build -trimpath -o "$output_dir/redis-shake-web" ./cmd/redisshakeweb )
cp "$repo_dir/README.md" "$output_dir/README.md"
cp "$repo_dir/third_party/redis-shake/license.txt" "$output_dir/REDISSHAKE-LICENSE.txt"
mkdir -p "$output_dir/patches"
cp -R "$repo_dir/docs" "$output_dir/docs"
cp "$repo_dir/patches/redis-shake-v4.6.2.patch" "$output_dir/patches/redis-shake-v4.6.2.patch"
printf 'RedisShake source: third_party/redis-shake (v4.6.2 with Web patch applied)\nUpstream commit: f20f28e6f2679e71a213904d2c74ceb521e19551\nUpstream archive SHA-256: %s\nApplied patch record: patches/redis-shake-v4.6.2.patch\n' "$source_sha256" > "$output_dir/VERSIONS.txt"
echo "Built $output_dir"
