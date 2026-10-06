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

select event.identifier, event.git_ref, event.result
from public.sessions_events as event
join public.jobs as job on job.session_event_identifier = event.identifier
where event.session_identifier = $1
    and job.status in ('running', 'cancelling');
