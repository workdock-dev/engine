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

python3 - <<'PYTHON'
import json
import os
from pathlib import Path
import sys
from urllib.error import HTTPError, URLError
from urllib.parse import quote, urlencode
from urllib.request import Request, urlopen

repository = REPO_FULL_NAME_ARG
workspace = Path("/home") / os.environ["USER"] / "workspace"
head = next(workspace.glob("**/.git/HEAD"), None)

if not repository or head is None:
    sys.exit(0)

reference = head.read_text().strip()
prefix = "ref: refs/heads/"

if not reference.startswith(prefix):
    sys.exit(0)

branch = reference[len(prefix):]
owner = repository.split("/", 1)[0]
query = urlencode({"head": f"{owner}:{branch}", "state": "open", "per_page": 1})
request = Request(
    f"https://api.github.com/repos/{quote(repository, safe='/')}/pulls?{query}",
    headers={
        "Accept": "application/vnd.github+json",
        "Authorization": f"Bearer {os.environ['WORKDOCK_GITHUB_API_TOKEN']}",
        "X-GitHub-Api-Version": "2022-11-28",
    },
)

try:
    with urlopen(request, timeout=30) as response:
        pull_requests = json.load(response)
except (HTTPError, URLError):
    print("[github] failed to retrieve pull request metadata", file=sys.stderr)
    sys.exit(1)

if pull_requests:
    pull_request = pull_requests[0]
    print(json.dumps({
        "number": pull_request["number"],
        "url": pull_request["html_url"],
        "headRefName": pull_request["head"]["ref"],
        "headRefOid": pull_request["head"]["sha"],
    }))
PYTHON
