-- Real success time of a scheduled job. last_run_at is written when a run starts (it drives the
-- rate limit), so it cannot tell a finished run from a failed one. NULL = unknown.
ALTER TABLE job_executions ADD COLUMN last_success_at DATETIME;
