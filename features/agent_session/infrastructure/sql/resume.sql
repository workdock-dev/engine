UPDATE public.jobs
SET
    status = 'queued',
    attempts = 0,
    next_attempt_at = now(),
    updated_at = now()
WHERE session_event_identifier = $1
AND status = 'awaiting_action';
