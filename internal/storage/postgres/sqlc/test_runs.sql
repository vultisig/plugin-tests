-- name: CreateTestRun :one
INSERT INTO test_runs (plugin_id, proposal_id, version, status, requested_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetTestRun :one
SELECT * FROM test_runs
WHERE id = $1;

-- name: ListTestRuns :many
SELECT * FROM test_runs
WHERE (sqlc.narg('plugin_id')::text IS NULL OR plugin_id = sqlc.narg('plugin_id'))
  AND (sqlc.narg('status')::test_run_status IS NULL OR status = sqlc.narg('status'))
ORDER BY created_at DESC
LIMIT sqlc.arg('query_limit') OFFSET sqlc.arg('query_offset');

-- name: CountTestRuns :one
SELECT COUNT(*)::bigint FROM test_runs
WHERE (sqlc.narg('plugin_id')::text IS NULL OR plugin_id = sqlc.narg('plugin_id'))
  AND (sqlc.narg('status')::test_run_status IS NULL OR status = sqlc.narg('status'));

-- name: GetDistinctPluginIDs :many
SELECT DISTINCT plugin_id FROM test_runs ORDER BY plugin_id;

-- name: UpdateTestRunStarted :exec
UPDATE test_runs
SET status = 'RUNNING', started_at = NOW(), updated_at = NOW()
WHERE id = $1;

-- name: UpdateTestRunFinished :exec
UPDATE test_runs
SET status = $2, artifact_prefix = $3, error_message = $4, finished_at = NOW(), updated_at = NOW()
WHERE id = $1;

-- name: MarkStaleRunAsError :exec
UPDATE test_runs
SET status = 'ERROR', error_message = $2, finished_at = NOW(), updated_at = NOW()
WHERE id = $1 AND status = 'RUNNING';

-- name: GetStaleRunningRuns :many
SELECT * FROM test_runs
WHERE status = 'RUNNING' AND started_at < NOW() - $1::interval;
