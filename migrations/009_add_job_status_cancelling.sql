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

-- Write your migrate up statements here

-- A cancellation request no longer flips a running job straight to
-- 'cancelled': it first moves it to 'cancelling' while the worker tears its
-- handler down. Because the sandbox is shared across the session's jobs, a
-- job queued while a sibling is 'cancelling' must not be claimed until the
-- teardown finishes, so the claim guard treats 'cancelling' like 'running'.

ALTER TYPE public.job_status ADD VALUE IF NOT EXISTS 'cancelling' AFTER 'running';

-- For insert_job.sql
CREATE INDEX idx_jobs_queued_by_cancelling ON public.jobs (queued_by) WHERE status = 'cancelling';

-- For claim.sql
CREATE INDEX idx_jobs_cancelling_session_event ON public.jobs (session_event_identifier) WHERE status = 'cancelling';

-- For the orphan-recovery cron
CREATE INDEX idx_jobs_cancelling_lease ON public.jobs (lease_expires_at) WHERE status = 'cancelling';

-- When a job transitions from 'cancelling' to 'cancelled' the cancellation
-- has fully completed: any job the session queued while the cancellation was
-- in progress (scheduled 2 minutes out by insert_job.sql) is released so its
-- next attempt runs immediately.
CREATE OR REPLACE FUNCTION public.notify_jobs_changed()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.status IN ('queued', 'retry') THEN
            PERFORM pg_notify('jobs_claimable', '');
        END IF;

    ELSIF TG_OP = 'UPDATE' THEN
        IF OLD.status = 'cancelling' AND NEW.status = 'cancelled' THEN
            UPDATE public.jobs pending
            SET
                next_attempt_at = NOW(),
                updated_at = NOW()
            WHERE
                pending.queued_by = NEW.queued_by
            AND
                pending.status IN ('queued', 'retry')
            AND
                pending.next_attempt_at > NOW();

            PERFORM pg_notify('jobs_claimable', '');
        END IF;

        IF OLD.status IS DISTINCT FROM NEW.status THEN
            IF NEW.status = 'retry' THEN
                PERFORM pg_notify('jobs_claimable', '');

            ELSIF NEW.status IN ('cancelling', 'cancelled') THEN
                PERFORM pg_notify('jobs_cancelled', NEW.session_event_identifier);
            END IF;
        END IF;
    END IF;

    RETURN NEW;
END;
$$;

-- A worker that dies mid-teardown never transitions its 'cancelling' job to
-- 'cancelled'; its heartbeat stopped with the cancellation, so once the lease
-- expires the orphan-recovery job finalizes the cancellation on its behalf
-- (which also releases the session's pending jobs through the trigger).
SELECT cron.unschedule('jobs-orphaned-recovery');

SELECT cron.schedule(
    'jobs-orphaned-recovery',
    '5 seconds',
    $$
    WITH orphaned_cancelling AS (
        UPDATE public.jobs
        SET
            status = 'cancelled',
            lease_owner = null,
            lease_expires_at = null,
            updated_at = NOW()
        WHERE
            status = 'cancelling'
        AND
            lease_expires_at <= NOW()
        RETURNING 1
    )
    SELECT pg_notify('jobs_claimable', '')
    WHERE EXISTS (
        SELECT 1
        FROM public.jobs
        WHERE
            (
                status IN ('queued', 'retry')
                AND next_attempt_at <= NOW()
            )
            OR (
                status = 'running'
                AND lease_expires_at <= NOW()
            )
    );
    $$
);

---- create above / drop below ----

SELECT cron.unschedule('jobs-orphaned-recovery');

SELECT cron.schedule(
    'jobs-orphaned-recovery',
    '5 seconds',
    $$
    SELECT pg_notify('jobs_claimable', '')
    WHERE EXISTS (
        SELECT 1
        FROM public.jobs
        WHERE
            (
                status IN ('queued', 'retry')
                AND next_attempt_at <= NOW()
            )
            OR (
                status = 'running'
                AND lease_expires_at <= NOW()
            )
    );
    $$
);

CREATE OR REPLACE FUNCTION public.notify_jobs_changed()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.status IN ('queued', 'retry') THEN
            PERFORM pg_notify('jobs_claimable', '');
        END IF;

    ELSIF TG_OP = 'UPDATE' THEN
        IF OLD.status IS DISTINCT FROM NEW.status THEN
            IF NEW.status = 'retry' THEN
                PERFORM pg_notify('jobs_claimable', '');

            ELSIF NEW.status = 'cancelled' THEN
                PERFORM pg_notify('jobs_cancelled', NEW.session_event_identifier);
            END IF;
        END IF;
    END IF;

    RETURN NEW;
END;
$$;

DROP INDEX IF EXISTS public.idx_jobs_cancelling_lease;

DROP INDEX IF EXISTS public.idx_jobs_cancelling_session_event;

DROP INDEX IF EXISTS public.idx_jobs_queued_by_cancelling;

-- The 'cancelling' enum value cannot be removed from job_status in
-- PostgreSQL; it stays unused after this down migration.