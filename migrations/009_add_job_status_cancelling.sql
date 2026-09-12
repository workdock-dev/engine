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
-- handler down.
--
-- This migration only adds the enum value. PostgreSQL rejects any use of a
-- newly added value inside the transaction that adds it (SQLSTATE 55P04,
-- "unsafe use of new value"), and the runner executes each migration file in
-- a single transaction — so everything that uses 'cancelling' lives in the
-- next migration (010_support_job_status_cancelling.sql).

ALTER TYPE public.job_status ADD VALUE IF NOT EXISTS 'cancelling' AFTER 'running';