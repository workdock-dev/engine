#!/usr/bin/env bash
# Copyright 2026 Jaziel Guerrero
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Starts the single-node services and renders the mounted WorkDock config.
set -Eeuo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root_dir"

require_command() {
  command -v "$1" >/dev/null 2>&1 || {
    printf 'Missing required command: %s\n' "$1" >&2
    exit 1
  }
}

require_command docker
require_command openssl
require_command curl

if [[ ! -f .env ]]; then
  cp .env.example .env
fi

set_env() {
  local key="$1" value="$2"
  if grep -q "^${key}=" .env; then
    sed -i "s|^${key}=.*|${key}=${value}|" .env
  else
    printf '%s=%s\n' "$key" "$value" >> .env
  fi
}

is_placeholder() {
  [[ -z "$1" || "$1" == replace-with-* ]]
}

# shellcheck disable=SC1091
set -a
source .env
set +a

for secret_spec in "INFISICAL_ENCRYPTION_KEY:16" "INFISICAL_AUTH_SECRET:32" "SIGNOZ_JWT_SECRET:32"; do
  key="${secret_spec%%:*}"
  bytes="${secret_spec##*:}"
  value="${!key:-}"
  if is_placeholder "$value"; then
    value="$(openssl rand -hex "$bytes")"
    set_env "$key" "$value"
    export "$key=$value"
  fi
done

docker compose up --build -d \
  postgres postgres-init redis infisical \
  signoz-keeper clickhouse signoz-telemetrystore-migrator signoz signoz-otel-collector

wait_for() {
  local name="$1" url="$2"
  for _ in $(seq 1 60); do
    if curl --fail --silent --show-error "$url" >/dev/null 2>&1; then
      printf '%s is ready.\n' "$name"
      return 0
    fi
    sleep 2
  done
  printf 'Timed out waiting for %s (%s). Inspect: docker compose logs %s\n' "$name" "$url" "$name" >&2
  return 1
}

wait_for infisical http://localhost:8081/api/status
wait_for signoz http://localhost:8082/api/v1/health

# Refresh values after the generated secrets were persisted to .env.
set -a
source .env
set +a

required=(
  WORKDOCK_LINEAR_SERVER_URL
  WORKDOCK_LINEAR_WEBHOOK_SECRET
  WORKDOCK_LINEAR_API_KEY
  WORKDOCK_LINEAR_CLIENT_ID
  WORKDOCK_LINEAR_CLIENT_SECRET
  WORKDOCK_GITHUB_BOT_LOGIN_ID
  WORKDOCK_GITHUB_CLIENT_ID
  WORKDOCK_GITHUB_WEBHOOK_SECRET
  WORKDOCK_DAYTONA_API_KEY
  WORKDOCK_OPENCODE_MODEL
  WORKDOCK_INFISICAL_CLIENT_ID
  WORKDOCK_INFISICAL_CLIENT_SECRET
  WORKDOCK_INFISICAL_PROJECT_ID
)

missing=()
for key in "${required[@]}"; do
  if is_placeholder "${!key:-}"; then
    missing+=("$key")
  fi
done

if [[ ! -f docker/workdock/github-app.pem ]]; then
  missing+=("docker/workdock/github-app.pem")
fi

if (( ${#missing[@]} )); then
  printf '\nInfrastructure is running, but WorkDock has not been started.\n'
  printf 'Open Infisical at http://localhost:8081, create the administrator, project, and Universal Auth client.\n'
  printf 'Then add the following to .env, place the GitHub App key in docker/workdock/github-app.pem, and rerun this script:\n'
  printf '  %s\n' "${missing[@]}"
  exit 0
fi

yaml_quote() {
  printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

quote() {
  printf '"%s"' "$(yaml_quote "$1")"
}

cat > docker/workdock/config.yaml <<EOF
service_name: workdock
server_address: ":8080"
workers: 3

linear:
  server_url: $(quote "$WORKDOCK_LINEAR_SERVER_URL")
  webhook_secret: $(quote "$WORKDOCK_LINEAR_WEBHOOK_SECRET")
  api_key: $(quote "$WORKDOCK_LINEAR_API_KEY")
  client_id: $(quote "$WORKDOCK_LINEAR_CLIENT_ID")
  client_secret: $(quote "$WORKDOCK_LINEAR_CLIENT_SECRET")
  ips: ["35.231.147.226", "35.243.134.228", "34.140.253.14", "34.38.87.206", "34.134.222.122", "35.222.25.142"]

github:
  bot_login_id: $(quote "$WORKDOCK_GITHUB_BOT_LOGIN_ID")
  client_id: $(quote "$WORKDOCK_GITHUB_CLIENT_ID")
  private_key_path: "/app/config/github-app.pem"
  webhook_secret: $(quote "$WORKDOCK_GITHUB_WEBHOOK_SECRET")

daytona:
  api_url: $(quote "${WORKDOCK_DAYTONA_API_URL:-https://app.daytona.io/api}")
  api_key: $(quote "$WORKDOCK_DAYTONA_API_KEY")
  target: $(quote "${WORKDOCK_DAYTONA_TARGET:-us}")

opencode:
  destroy_on_dispose: false
  liveness_timeout_seconds: 30
  max_health_misses: 3
  model: $(quote "$WORKDOCK_OPENCODE_MODEL")
  permission:
    "external_directory": "deny"
    "*": "allow"

infisical:
  site_url: "http://infisical:8080"
  client_id: $(quote "$WORKDOCK_INFISICAL_CLIENT_ID")
  client_secret: $(quote "$WORKDOCK_INFISICAL_CLIENT_SECRET")
  project_id: $(quote "$WORKDOCK_INFISICAL_PROJECT_ID")
  environment: $(quote "${WORKDOCK_INFISICAL_ENVIRONMENT:-dev}")

postgres:
  database_url: "postgresql://workdock:$(yaml_quote "$POSTGRES_PASSWORD")@postgres:5432/workdock"

otlp:
  endpoint: "signoz-otel-collector:4318"
  insecure: true
  slog:
    level: debug
    source: true
EOF

cat > docker/workdock/tern.conf <<EOF
[database]
host = postgres
port = 5432
database = workdock
user = workdock
password = $POSTGRES_PASSWORD
sslmode = disable
version_table = public.schema_version
EOF

chmod 644 docker/workdock/config.yaml docker/workdock/tern.conf

docker compose up -d migrate workdock
printf '\nWorkDock is running at http://localhost:8080\n'
