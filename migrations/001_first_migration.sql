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

CREATE TYPE agent_provider AS ENUM('linear');

CREATE TABLE organizations (
  id serial PRIMARY KEY,
  identifier TEXT NOT NULL,
  provider agent_provider NOT NULL,
  name TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_organizations_identifier ON organizations (identifier);

CREATE TABLE sessions (
  id serial PRIMARY KEY,
  identifier_org TEXT REFERENCES organizations (identifier) NOT NULL,
  identifier TEXT NOT NULL,
  provider agent_provider NOT NULL,
  issue_id TEXT NOT NULL,
  creator TEXT NOT NULL,
  repo_full_name TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_sessions_identifier ON sessions (identifier);

CREATE TABLE sessions_events (
  id serial PRIMARY KEY,
  session_identifier TEXT REFERENCES sessions (identifier) NOT NULL,  
  identifier TEXT NOT NULL,
  payload JSONB NOT NULL,
  git_ref TEXT,
  result JSONB,
  seed TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON COLUMN sessions_events.seed IS 'Set when event originates from a previous event';
CREATE UNIQUE INDEX idx_sessions_events_identifier ON sessions_events (identifier);
CREATE INDEX idx_sessions_events_git_ref_updated_at ON sessions_events (git_ref, updated_at DESC, id DESC) WHERE git_ref IS NOT NULL;
CREATE INDEX idx_sessions_events_session_identifier ON sessions_events (session_identifier);

CREATE TABLE github_connections (
  id serial PRIMARY KEY,
  session_event_identifier TEXT REFERENCES sessions_events (identifier) NOT NULL,
  repo_full_name TEXT NOT NULL,
  connected BOOLEAN NOT NULL,
  installation_id TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_github_connections_installation_id ON github_connections (installation_id) WHERE installation_id IS NOT NULL;
CREATE UNIQUE INDEX idx_github_connections_repo_full_name ON github_connections (repo_full_name);

CREATE TYPE job_status AS ENUM(
  'queued',
  'running',
  'retry',
  'succeeded',
  'failed',
  'cancelled'
);

CREATE TABLE jobs (
  id serial PRIMARY KEY,
  session_event_identifier TEXT REFERENCES sessions_events(identifier) NOT NULL,
  status job_status NOT NULL DEFAULT 'queued',
  attempts INT NOT NULL DEFAULT 0,
  next_attempt_at TIMESTAMPTZ DEFAULT now(),
  lease_owner TEXT,
  lease_expires_at TIMESTAMPTZ,
  last_error TEXT,
  cancellation_reason TEXT,
  queued_by TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  started_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- For fail.sql, retry.sql, complete.sql
CREATE UNIQUE INDEX idx_jobs_session_event_identifier ON jobs (session_event_identifier);

-- For claimn.sql
CREATE INDEX idx_jobs_runnable ON jobs(next_attempt_at) WHERE status IN ('queued', 'retry');
CREATE INDEX idx_jobs_running_session_event ON jobs (session_event_identifier) WHERE status = 'running';

-- For cancel.sql
CREATE INDEX idx_jobs_queued_by_status ON jobs(queued_by) WHERE status IN ('queued', 'running', 'retry'); 

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

CREATE TRIGGER jobs_notify_changed
AFTER INSERT
OR
UPDATE ON public.jobs FOR EACH ROW
EXECUTE FUNCTION public.notify_jobs_changed ();

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

---- create above / drop below ----

SELECT cron.unschedule('jobs-orphaned-recovery');

DROP TRIGGER IF EXISTS jobs_notify_changed ON public.jobs;

DROP FUNCTION IF EXISTS public.notify_jobs_changed ();

DROP TABLE jobs;

DROP TABLE github_connections;

DROP TABLE sessions_events ;

DROP TABLE sessions;

DROP TABLE organizations;

DROP TYPE job_status;

DROP TYPE agent_provider;

-- Write your migrate down statements here. If this migration is irreversible
-- Then delete the separator line above.
