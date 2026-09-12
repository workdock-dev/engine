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

UPDATE public.jobs
SET
    -- A running job is only marked 'cancelling': its worker is still tearing
    -- the handler down (the sandbox is shared across the session, so a new
    -- prompt must not run on it mid-teardown). The scheduler finalizes the
    -- cancellation once the handler returns.
    status = CASE
        WHEN status = 'running' THEN 'cancelling'
        ELSE 'cancelled'
    END::job_status,
    cancellation_reason = $2,
    next_attempt_at = null,
    -- The lease of a cancelling job is kept: the heartbeats that renew it
    -- stop once the job context is cancelled, so an expired lease means the
    -- worker died mid-teardown and the orphan-recovery job finalizes the
    -- cancellation.
    lease_owner = CASE WHEN status = 'running' THEN lease_owner ELSE null END,
    lease_expires_at = CASE WHEN status = 'running' THEN lease_expires_at ELSE null END,
    updated_at = now()
WHERE
    queued_by = $1
AND
    status IN ('queued', 'running', 'retry');
