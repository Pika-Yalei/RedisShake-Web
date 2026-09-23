#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
release_dir="${1:-$repo_dir/dist/release}"
mkdir -p "$release_dir"

for target in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64; do
  target_os="${target%%/*}"
  target_arch="${target##*/}"
  bundle="redis-shake-web-${target_os}-${target_arch}"
  bash "$repo_dir/scripts/build.sh" "$target_os" "$target_arch" "$release_dir/$bundle"
  tar -C "$release_dir" -czf "$release_dir/$bundle.tar.gz" "$bundle"
done

: > "$release_dir/SHA256SUMS"
for archive in "$release_dir"/*.tar.gz; do
  archive_sha256="$(openssl dgst -sha256 "$archive" | awk '{print $NF}')"
  printf '%s  %s\n' "$archive_sha256" "$(basename "$archive")" >> "$release_dir/SHA256SUMS"
done
echo "Release archives: $release_dir"
