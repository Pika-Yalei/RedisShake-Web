#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
platform="$(go env GOOS)-$(go env GOARCH)"
shake="${REDISSHAKE_WEB_DEV_SHAKE:-$repo_dir/dist/redis-shake-web-$platform/redis-shake}"
if [[ ! -x "$shake" && -z "${REDISSHAKE_WEB_DEV_SHAKE:-}" ]]; then
  shake="$repo_dir/dist/release/redis-shake-web-$platform/redis-shake"
fi
if [[ ! -x "$shake" ]]; then
  echo "RedisShake 内核不存在：$shake" >&2
  echo "请先执行 bash scripts/build.sh，或设置 REDISSHAKE_WEB_DEV_SHAKE。" >&2
  exit 1
fi

data_dir="${REDISSHAKE_WEB_DEV_DATA_DIR:-$repo_dir/.redis-shake-web/dev}"
listen="${REDISSHAKE_WEB_DEV_LISTEN:-127.0.0.1:8080}"
web_bin="$repo_dir/bin/redis-shake-web-dev"
mkdir -p "$repo_dir/bin" "$data_dir"
( cd "$repo_dir" && go build -o "$web_bin" ./cmd/redisshakeweb )

exec "$web_bin" serve \
  --data-dir "$data_dir" \
  --listen "$listen" \
  --redis-shake "$shake" \
  --assets-dir "$repo_dir/cmd/redisshakeweb/static"
