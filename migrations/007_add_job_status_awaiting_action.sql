-- Write your migrate up statements here

ALTER TYPE public.job_status ADD VALUE IF NOT EXISTS 'awaiting_action' AFTER 'retry';
