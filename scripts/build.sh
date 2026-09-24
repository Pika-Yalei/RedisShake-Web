#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
target_os="${1:-$(go env GOOS)}"
target_arch="${2:-$(go env GOARCH)}"
output_dir="${3:-$repo_dir/dist/redis-shake-web-${target_os}-${target_arch}}"
source_sha256="c6c7de3a76b7bf4f18e494ffb40f1d09287622c4574d4da41fc1244a2dcc0b3f"
mkdir -p "$output_dir"
( cd "$repo_dir" && CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" go build -trimpath -o "$output_dir/redis-shake-web" ./cmd/redisshakeweb )
rm -f "$output_dir/redis-shake"
cp "$repo_dir/README.md" "$output_dir/README.md"
cp "$repo_dir/REDISSHAKE-LICENSE.txt" "$output_dir/REDISSHAKE-LICENSE.txt"
cp -R "$repo_dir/docs" "$output_dir/docs"
rm -f "$output_dir/patches/redis-shake-v4.6.2.patch"
rmdir "$output_dir/patches" 2>/dev/null || true
printf 'RedisShake kernel: integrated as a single-task process in redis-shake-web (upstream v4.6.2 with local changes)\nUpstream commit: f20f28e6f2679e71a213904d2c74ceb521e19551\nUpstream archive SHA-256: %s\nLocal changes: see docs/verification.md\n' "$source_sha256" > "$output_dir/VERSIONS.txt"
echo "Built $output_dir"
