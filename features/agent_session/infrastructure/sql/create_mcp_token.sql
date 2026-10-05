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

with token as (
    insert into public.sessions_mcp_tokens (
        agent_session_id,
        agent_session_token,
        agent_session_event_id
    ) values ($1, $2, $3)
    returning agent_session_event_id
)
update public.sessions_events as event
set result = event.result - 'Report'
from token
where event.identifier = token.agent_session_event_id;
