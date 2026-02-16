package types

import (
	"time"

	"github.com/google/uuid"

	"github.com/vultisig/plugin-tests/internal/storage/postgres/queries"
)

type TestRunStatus string

const (
	StatusQueued  TestRunStatus = "QUEUED"
	StatusRunning TestRunStatus = "RUNNING"
	StatusPassed  TestRunStatus = "PASSED"
	StatusFailed  TestRunStatus = "FAILED"
	StatusError   TestRunStatus = "ERROR"
)

type TestRun struct {
	ID             uuid.UUID     `json:"id"`
	PluginID       string        `json:"plugin_id"`
	ProposalID     *string       `json:"proposal_id,omitempty"`
	Version        *string       `json:"version,omitempty"`
	Status         TestRunStatus `json:"status"`
	RequestedBy    string        `json:"requested_by"`
	ArtifactPrefix *string       `json:"artifact_prefix,omitempty"`
	ErrorMessage   *string       `json:"error_message,omitempty"`
	StartedAt      *time.Time    `json:"started_at,omitempty"`
	FinishedAt     *time.Time    `json:"finished_at,omitempty"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

func TestRunFromQuery(q *queries.TestRun) TestRun {
	r := TestRun{
		PluginID:    q.PluginID,
		Status:      TestRunStatus(q.Status),
		RequestedBy: q.RequestedBy,
	}

	copy(r.ID[:], q.ID.Bytes[:])

	if q.ProposalID.Valid {
		r.ProposalID = &q.ProposalID.String
	}
	if q.Version.Valid {
		r.Version = &q.Version.String
	}
	if q.ArtifactPrefix.Valid {
		r.ArtifactPrefix = &q.ArtifactPrefix.String
	}
	if q.ErrorMessage.Valid {
		r.ErrorMessage = &q.ErrorMessage.String
	}
	if q.StartedAt.Valid {
		t := q.StartedAt.Time
		r.StartedAt = &t
	}
	if q.FinishedAt.Valid {
		t := q.FinishedAt.Time
		r.FinishedAt = &t
	}
	if q.CreatedAt.Valid {
		r.CreatedAt = q.CreatedAt.Time
	}
	if q.UpdatedAt.Valid {
		r.UpdatedAt = q.UpdatedAt.Time
	}

	return r
}
