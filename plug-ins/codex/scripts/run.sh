#!/usr/bin/env sh

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

CODEX_STDERR_FILE="$(mktemp)" || exit 1

if ! mkdir -p WORKSPACE_PATH_ARG; then
	rm -f "${CODEX_STDERR_FILE}"
	exit 1
fi

cd WORKSPACE_PATH_ARG || {
	rm -f "${CODEX_STDERR_FILE}"
	exit 1
}

PATH=/home/${USER}/.local/bin:$PATH CODEX_HOME=CODEX_HOME_ARG codex exec --json --skip-git-repo-check resume --last < PROMPT_FILE_PATH_ARG 2>"${CODEX_STDERR_FILE}"
CODEX_EXIT_CODE=$?

sed '/^Reading prompt from stdin\.\.\.$/d' "${CODEX_STDERR_FILE}" >&2
printf '%s\n' '{"type":"workdock.stream.flush"}'
rm -f "${CODEX_STDERR_FILE}"

exit "${CODEX_EXIT_CODE}"
