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

repo=$(find /home/${USER}/workspace -type d -name .git -print -quit)

[ -n "$repo" ] || exit 0

cd "$(dirname "$repo")" || exit 1

output=$(gh pr view --json number,url,headRefName,headRefOid 2>&1)
exit_code=$?

if [ "$exit_code" -eq 0 ]; then
    printf '%s\n' "$output"
elif [[ "$output" == *"no pull requests found for branch"* ]]; then
    exit 0
else
    printf '%s\n' "$output" >&2
    exit "$exit_code"
fi
