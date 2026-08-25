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

SELECT
    se.session_identifier,
    se.identifier,
    se.payload,
    se.seed,
    se.git_ref,
    se.result,
    se.reason
FROM
    public.sessions_events se
JOIN
    public.sessions s ON s.identifier = se.session_identifier
WHERE
    se.git_ref = $1
    AND s.repo_full_name = $2
ORDER BY se.updated_at DESC, se.id DESC
LIMIT 1;
