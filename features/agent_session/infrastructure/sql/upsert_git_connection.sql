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

INSERT INTO public.integrations (
    identifier,
    provider,
    name,
    connected,
    metadata
)
VALUES (
    $5,
    'github',
    $2,
    $3,
    jsonb_build_object('session_event_identifier', $1::text, 'installation_id', $4::text)
)
ON CONFLICT (name) WHERE provider = 'github'
DO UPDATE SET
    connected = EXCLUDED.connected,
    metadata = integrations.metadata || jsonb_build_object(
        'session_event_identifier', COALESCE(EXCLUDED.metadata ->> 'session_event_identifier', integrations.metadata ->> 'session_event_identifier'),
        'installation_id', EXCLUDED.metadata ->> 'installation_id'
    ),
    updated_at = NOW()
RETURNING metadata ->> 'session_event_identifier';
