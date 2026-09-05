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

set -Eeuo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root_dir"

if [[ -f docker/.watch.pid ]]; then
  kill "$(cat docker/.watch.pid)" 2>/dev/null || true
  rm -f docker/.watch.pid docker/.watch.log
fi

if [[ ! -f docker/generated.env ]]; then
  printf 'No docker/generated.env file found. Creating a temporary one for docker compose down.\n'
  printf 'POSTGRES_PASSWORD=workdock\n' > docker/generated.env
  printf 'SIGNOZ_JWT_SECRET=workdock\n' >> docker/generated.env
fi

docker compose --env-file docker/generated.env down -v --rmi local

rm -f docker/generated.env docker/workdock/tern.conf

printf '\nAll containers, volumes, and generated files have been removed.\n'
printf 'config.yaml and github-app.pem were kept since they are user-provided.\n'