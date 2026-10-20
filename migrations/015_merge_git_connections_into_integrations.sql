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

ALTER TABLE public.integrations
    ADD COLUMN connected BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN metadata JSONB NOT NULL DEFAULT '{}';

CREATE UNIQUE INDEX idx_integrations_github_name
    ON public.integrations (name) WHERE provider = 'github';

INSERT INTO public.integrations (
    identifier,
    provider,
    name,
    connected,
    metadata,
    created_at,
    updated_at
)
SELECT
    uuidv7()::text,
    'github',
    repo_full_name,
    connected,
    to_jsonb(connection) - 'repo_full_name' - 'connected',
    created_at,
    updated_at
FROM public.git_connections AS connection;

DROP TABLE public.git_connections;
