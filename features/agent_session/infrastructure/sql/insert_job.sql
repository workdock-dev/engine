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

insert into public.jobs (session_event_identifier, queued_by, next_attempt_at)
values(
    $1,
    $2,
    -- While a job of the session is 'cancelling' (teardown in progress, the
    -- sandbox is stopped with a 2 minutes timeout), the claim guard blocks
    -- every claim of this session. Schedule the first attempt 2 minutes out
    -- instead of now so the job is not claimed while the teardown is still
    -- running; the 'cancelling' -> 'cancelled' transition releases it sooner.
    case when exists (
        select 1
        from public.jobs j
        where
            j.queued_by = $2
        and
            j.status = 'cancelling'
    ) then now() + interval '2 minutes' else now() end
);

