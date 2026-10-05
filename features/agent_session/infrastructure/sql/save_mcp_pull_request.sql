-- Copyright 2026 Jaziel Guerrero
--
-- Licensed under the Apache License, Version 2.0 (the "License");
-- you may not use this file except in compliance with the License.
-- You may obtain a copy of the License at
--
--     http://www.apache.org/licenses/LICENSE-2.0
--
-- Unless required by applicable law or agreed to in writing, software
-- distributed under the License is distributed on an "AS IS" BASIS,
-- WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
-- See the License for the specific language governing permissions and
-- limitations under the License.

update public.sessions_events as event
set git_ref = $3,
    result = coalesce(event.result, '{}'::jsonb) || jsonb_build_object('PullRequest', $4::jsonb)
from public.sessions_mcp_tokens as token
where token.agent_session_id = $1
    and token.agent_session_token = $2
    and event.identifier = token.agent_session_event_id
    and event.session_identifier = token.agent_session_id;
