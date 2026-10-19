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

CREATE TABLE integrations (
  id serial PRIMARY KEY,
  identifier TEXT NOT NULL,
  provider agent_provider NOT NULL,
  name TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_integrations_identifier ON integrations (identifier);

INSERT INTO integrations (id, identifier, provider, name, created_at, updated_at)
SELECT id, identifier, provider, name, created_at, updated_at
FROM organizations;

SELECT setval(
  pg_get_serial_sequence('integrations', 'id'),
  COALESCE((SELECT MAX(id) FROM integrations), 1),
  EXISTS (SELECT 1 FROM integrations)
);

ALTER TABLE sessions DROP CONSTRAINT sessions_identifier_org_fkey;

ALTER TABLE sessions
  ADD CONSTRAINT sessions_identifier_org_fkey
  FOREIGN KEY (identifier_org) REFERENCES integrations (identifier);

---- create above / drop below ----

INSERT INTO organizations (identifier, provider, name, created_at, updated_at)
SELECT identifier, provider, name, created_at, updated_at
FROM integrations
ON CONFLICT (identifier) DO NOTHING;

ALTER TABLE sessions DROP CONSTRAINT sessions_identifier_org_fkey;

ALTER TABLE sessions
  ADD CONSTRAINT sessions_identifier_org_fkey
  FOREIGN KEY (identifier_org) REFERENCES organizations (identifier);

DROP TABLE integrations;
