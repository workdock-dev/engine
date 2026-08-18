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

if [[ ! -f .env ]]; then
  printf 'No .env file found. Creating a temporary one for docker compose down.\n'
  cp .env.example .env
fi

docker compose down -v --rmi local

rm -f docker/workdock/config.yaml docker/workdock/tern.conf .env

printf '\nAll containers, volumes, and generated files have been removed.\n'