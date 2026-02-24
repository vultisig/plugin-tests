package api

import (
	"embed"
	"errors"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"

	"github.com/vultisig/plugin-tests/internal/storage/postgres/queries"
	"github.com/vultisig/plugin-tests/internal/types"
)

//go:embed templates/*.html
var templateFS embed.FS

const perPage = 20

var allStatuses = []string{"QUEUED", "RUNNING", "PASSED", "FAILED", "ERROR"}

var tmplFuncs = template.FuncMap{
	"lower": func(s any) string { return strings.ToLower(fmt.Sprintf("%v", s)) },
	"add":   func(a, b int) int { return a + b },
	"sub":   func(a, b int) int { return a - b },
	"deref": func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	},
	"formatTime": func(t time.Time) string {
		return t.UTC().Format("2006-01-02 15:04:05 UTC")
	},
	"formatTimePtr": func(t *time.Time) string {
		if t == nil {
			return "-"
		}
		return t.UTC().Format("2006-01-02 15:04:05 UTC")
	},
	"duration": func(start, end *time.Time) string {
		if start == nil || end == nil {
			return "-"
		}
		d := end.Sub(*start)
		if d < time.Second {
			return fmt.Sprintf("%dms", d.Milliseconds())
		}
		return fmt.Sprintf("%.1fs", d.Seconds())
	},
	"paginationURL": func(pluginID, status string, page int) string {
		params := url.Values{}
		if pluginID != "" {
			params.Set("plugin_id", pluginID)
		}
		if status != "" {
			params.Set("status", status)
		}
		if page > 1 {
			params.Set("page", fmt.Sprintf("%d", page))
		}
		q := params.Encode()
		if q != "" {
			return "/results?" + q
		}
		return "/results"
	},
}

var (
	listTmpl = template.Must(
		template.New("").Funcs(tmplFuncs).ParseFS(templateFS, "templates/layout.html", "templates/list.html"),
	)
	detailTmpl = template.Must(
		template.New("").Funcs(tmplFuncs).ParseFS(templateFS, "templates/layout.html", "templates/detail.html"),
	)
)

type listPageData struct {
	Runs             []types.TestRun
	PluginIDs        []string
	Statuses         []string
	SelectedPluginID string
	SelectedStatus   string
	Page             int
	TotalPages       int
}

type detailPageData struct {
	Run             types.TestRun
	SeederLogs      string
	SmokeLogs       string
	IntegrationLogs string
}

func (s *Server) handleResultsList(c echo.Context) error {
	page := 1
	if v := c.QueryParam("page"); v != "" {
		fmt.Sscanf(v, "%d", &page)
		if page < 1 {
			page = 1
		}
	}
	offset := (page - 1) * perPage

	pluginID := strings.TrimSpace(c.QueryParam("plugin_id"))
	status := strings.TrimSpace(c.QueryParam("status"))

	var filterPluginID pgtype.Text
	if pluginID != "" {
		filterPluginID = pgtype.Text{String: pluginID, Valid: true}
	}
	var filterStatus queries.NullTestRunStatus
	if status != "" {
		filterStatus = queries.NullTestRunStatus{
			TestRunStatus: queries.TestRunStatus(status),
			Valid:         true,
		}
	}

	ctx := c.Request().Context()

	runs, err := s.db.Queries().ListTestRuns(ctx, &queries.ListTestRunsParams{
		PluginID:    filterPluginID,
		Status:      filterStatus,
		QueryLimit:  int32(perPage),
		QueryOffset: int32(offset),
	})
	if err != nil {
		s.logger.WithError(err).Error("failed to list test runs")
		return c.String(http.StatusInternalServerError, "failed to load runs")
	}

	total, err := s.db.Queries().CountTestRuns(ctx, &queries.CountTestRunsParams{
		PluginID: filterPluginID,
		Status:   filterStatus,
	})
	if err != nil {
		s.logger.WithError(err).Error("failed to count test runs")
		return c.String(http.StatusInternalServerError, "failed to count runs")
	}

	pluginIDs, err := s.db.Queries().GetDistinctPluginIDs(ctx)
	if err != nil {
		s.logger.WithError(err).Error("failed to get plugin IDs")
		pluginIDs = []string{}
	}

	items := make([]types.TestRun, 0, len(runs))
	for _, r := range runs {
		items = append(items, types.TestRunFromQuery(r))
	}

	totalPages := int(math.Ceil(float64(total) / float64(perPage)))
	if totalPages < 1 {
		totalPages = 1
	}

	data := listPageData{
		Runs:             items,
		PluginIDs:        pluginIDs,
		Statuses:         allStatuses,
		SelectedPluginID: pluginID,
		SelectedStatus:   status,
		Page:             page,
		TotalPages:       totalPages,
	}

	return renderHTML(c, listTmpl, data)
}

func (s *Server) handleResultsDetail(c echo.Context) error {
	idStr := c.Param("id")
	parsed, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid run id")
	}

	pgID := pgtype.UUID{Bytes: parsed, Valid: true}
	ctx := c.Request().Context()

	run, err := s.db.Queries().GetTestRun(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return c.String(http.StatusNotFound, "test run not found")
		}
		s.logger.WithError(err).Error("failed to get test run")
		return c.String(http.StatusInternalServerError, "failed to load run")
	}

	result := types.TestRunFromQuery(run)

	var seederLogs, smokeLogs, integrationLogs string
	if run.ArtifactPrefix.Valid && run.ArtifactPrefix.String != "" && s.artifactS3.Bucket != "" {
		prefix := run.ArtifactPrefix.String
		seederLogs, _ = readArtifact(ctx, s.artifactS3, prefix+"/seeder.txt")
		smokeLogs, _ = readArtifact(ctx, s.artifactS3, prefix+"/smoke.txt")
		integrationLogs, _ = readArtifact(ctx, s.artifactS3, prefix+"/integration.txt")
	}

	data := detailPageData{
		Run:             result,
		SeederLogs:      seederLogs,
		SmokeLogs:       smokeLogs,
		IntegrationLogs: integrationLogs,
	}

	return renderHTML(c, detailTmpl, data)
}

func renderHTML(c echo.Context, t *template.Template, data any) error {
	c.Response().Header().Set("Content-Type", "text/html; charset=utf-8")
	c.Response().WriteHeader(http.StatusOK)
	return t.ExecuteTemplate(c.Response().Writer, "layout", data)
}
