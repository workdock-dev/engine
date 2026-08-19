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

-- The git_ref lookup now also filters by repo_full_name via a join with
-- the sessions table.  This composite index covers that join efficiently
-- and replaces the previous index that only covered git_ref.
CREATE INDEX idx_sessions_events_git_ref_repo ON sessions_events (git_ref, session_identifier) WHERE git_ref IS NOT NULL;

DROP INDEX IF EXISTS idx_sessions_events_git_ref_updated_at;

---- create above / drop below ----

CREATE INDEX idx_sessions_events_git_ref_updated_at ON sessions_events (git_ref, updated_at DESC, id DESC) WHERE git_ref IS NOT NULL;

DROP INDEX IF EXISTS idx_sessions_events_git_ref_repo;