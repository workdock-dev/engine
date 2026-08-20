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

CREATE INDEX idx_sessions_issue_id ON sessions (issue_id);

---- create above / drop below ----

DROP INDEX IF EXISTS idx_sessions_issue_id;

-- Write your migrate down statements here. If this migration is irreversible
-- Then delete the separator line above.