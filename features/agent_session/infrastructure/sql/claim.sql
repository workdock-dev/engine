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

WITH next_job AS (
    SELECT
        j.id,
        j.status AS previous_status
    FROM public.jobs j
    JOIN public.sessions_events se
        ON se.identifier = j.session_event_identifier
    JOIN public.sessions s
        ON s.identifier = se.session_identifier
    WHERE
        (
            (
                j.status IN ('queued', 'retry')
                AND j.next_attempt_at <= NOW()
            )
            OR
            (
                j.status = 'running'
                AND j.lease_expires_at <= NOW()
            )
        )
        AND NOT EXISTS (
            SELECT 1
            FROM public.jobs running_job
            JOIN public.sessions_events running_se
                ON running_se.identifier = running_job.session_event_identifier
            WHERE
                running_job.status = 'running'
                AND running_job.id <> j.id
                AND running_se.session_identifier = se.session_identifier
        )
    ORDER BY j.next_attempt_at
    FOR UPDATE OF j, s SKIP LOCKED
    LIMIT 1
)
UPDATE public.jobs j
SET
    status = 'running',
    attempts = j.attempts + 1,
    next_attempt_at = $1,
    lease_owner = $2,
    lease_expires_at = $3,
    started_at = COALESCE(j.started_at, NOW()),
    updated_at = NOW()
FROM next_job
WHERE j.id = next_job.id
RETURNING
    j.session_event_identifier,
    next_job.previous_status,
    j.status AS current_status,
    j.attempts,
    j.next_attempt_at,
    j.lease_owner,
    j.lease_expires_at,
    j.last_error,
    j.cancellation_reason,
    j.queued_by;
