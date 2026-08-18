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

PGPASSWORD="${POSTGRES_PASSWORD:-workdock}"
export PGPASSWORD

until pg_isready -h postgres -U workdock -d workdock; do
  sleep 1
done

echo "Ensuring infisical database exists..."
psql -h postgres -U workdock -d workdock -tc "SELECT 1 FROM pg_database WHERE datname = 'infisical'" | grep -q 1 || \
  psql -h postgres -U workdock -d workdock -c "CREATE DATABASE infisical OWNER workdock"

echo "Ensuring pg_cron extension in workdock database..."
psql -h postgres -U workdock -d workdock -c "CREATE EXTENSION IF NOT EXISTS pg_cron"

echo "PostgreSQL initialization complete."