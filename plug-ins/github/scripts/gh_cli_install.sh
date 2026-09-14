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

# The Debian mirrors and the GitHub CLI apt repository behind them
# intermittently return 503s or reset connections. When `apt-get update`
# cannot fetch an index it only warns and exits zero, so the subsequent
# `apt-get install gh` fails with "Unable to locate package gh" and fails the
# whole sandbox setup. Retry the install with backoff to ride out the
# short-lived mirror outages.
attempt=1
max_attempts=4

while true; do
  if (type -p wget >/dev/null || (sudo apt-get update && sudo apt-get install wget -y)) \
    && sudo mkdir -p -m 755 /etc/apt/keyrings \
    && out=$(mktemp) && wget -nv -O"$out" https://cli.github.com/packages/githubcli-archive-keyring.gpg \
    && cat "$out" | sudo tee /etc/apt/keyrings/githubcli-archive-keyring.gpg > /dev/null \
    && sudo chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && sudo mkdir -p -m 755 /etc/apt/sources.list.d \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | sudo tee /etc/apt/sources.list.d/github-cli.list > /dev/null \
    && sudo apt-get -o Acquire::Retries=3 update \
    && sudo apt-get -o Acquire::Retries=3 install gh -y; then
    exit 0
  fi

  if [ "$attempt" -ge "$max_attempts" ]; then
    echo "failed to install gh after $attempt attempts" >&2
    exit 1
  fi

  echo "gh install attempt $attempt failed; retrying" >&2
  sleep $((attempt * 5))
  attempt=$((attempt + 1))
done
