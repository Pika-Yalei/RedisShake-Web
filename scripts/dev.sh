#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
data_dir="${REDISSHAKE_WEB_DEV_DATA_DIR:-$repo_dir/.redis-shake-web/dev}"
listen="${REDISSHAKE_WEB_DEV_LISTEN:-127.0.0.1:8080}"
web_bin="$repo_dir/bin/redis-shake-web-dev"
mkdir -p "$repo_dir/bin" "$data_dir"
shake="${REDISSHAKE_WEB_DEV_SHAKE:-$repo_dir/bin/redis-shake-dev}"
if [[ -n "${REDISSHAKE_WEB_DEV_SHAKE:-}" ]]; then
  if [[ ! -x "$shake" ]]; then
    echo "RedisShake 内核不存在或不可执行：$shake" >&2
    exit 1
  fi
else
  ( cd "$repo_dir" && go build -o "$shake" ./cmd/redis-shake )
fi
( cd "$repo_dir" && go build -o "$web_bin" ./cmd/redisshakeweb )

exec "$web_bin" serve \
  --data-dir "$data_dir" \
  --listen "$listen" \
  --redis-shake "$shake" \
  --assets-dir "$repo_dir/cmd/redisshakeweb/static"
