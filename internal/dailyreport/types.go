package dailyreport

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"MRSS/internal/models"
)

const (
	RunKindAuto     = "auto"
	RunKindManual   = "manual"
	RunKindBackfill = "backfill"

	RunStatusQueued      = "queued"
	RunStatusRefreshing  = "refreshing"
	RunStatusGenerating  = "generating"
	RunStatusCompleted   = "completed"
	RunStatusPartial     = "partial"
	RunStatusNoContent   = "no_content"
	RunStatusFailed      = "failed"
	RunStatusInterrupted = "interrupted"
)

// OutlineSection is the editable, non-sensitive report outline.
type OutlineSection struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Instruction string `json:"instruction"`
}

type ReportSection struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Summary   string `json:"summary"`
	SourceIDs []int  `json:"source_ids"`
}

type ReportContent struct {
	Sections []ReportSection `json:"sections"`
}

type Preview struct {
	PeriodStart      time.Time `json:"period_start"`
	PeriodEnd        time.Time `json:"period_end"`
	ArticleCount     int       `json:"article_count"`
	EstimatedBatches int       `json:"estimated_batches"`
}

type Status struct {
	Enabled                   bool       `json:"enabled"`
	IsRunning                 bool       `json:"is_running"`
	CurrentRunID              *int64     `json:"current_run_id,omitempty"`
	Progress                  int        `json:"progress"`
	CurrentStep               string     `json:"current_step,omitempty"`
	UnreadCount               int        `json:"unread_count"`
	NextScheduledAt           *time.Time `json:"next_scheduled_at,omitempty"`
	MissedCount               int        `json:"missed_count"`
	RequiresFeedSelection     bool       `json:"requires_feed_selection"`
	NotificationAuthorization string     `json:"notification_authorization"`
}

type RefreshResult struct {
	FeedID int64  `json:"feed_id"`
	Error  string `json:"error,omitempty"`
}

type Refresher interface {
	Refresh(context.Context, []int64) []RefreshResult
}

type AIResult struct {
	Content      ReportContent
	Markdown     string
	InputTokens  int64
	OutputTokens int64
}

// GenerationProgress is the non-sensitive, persisted state of a multi-stage
// report generation. Checkpoint contains only validated AI insights and source
// identifiers; credentials and request headers are never included.
type GenerationProgress struct {
	Fingerprint  string
	Checkpoint   string
	Stage        string
	InputTokens  int64
	OutputTokens int64
}

type CheckpointSaver func(GenerationProgress) error

// ResumableReportGenerator is implemented by cloud generators that can resume
// already completed extraction and merge batches after a retry or restart.
type ResumableReportGenerator interface {
	GenerateResumable(context.Context, *models.DailyReportConfig, []models.DailyReportCandidate, string, string, CheckpointSaver) (AIResult, error)
}

// GenerationError exposes a stable, non-sensitive failure code and stage to
// handlers and logs while retaining the original cause for errors.Is/As.
type GenerationError struct {
	Code  string
	Stage string
	Cause error
}

func (e *GenerationError) Error() string {
	if e == nil {
		return "daily report generation failed"
	}
	if e.Stage == "" {
		return fmt.Sprintf("daily report generation failed (%s)", e.Code)
	}
	return fmt.Sprintf("daily report generation failed at %s (%s)", e.Stage, e.Code)
}

func (e *GenerationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

type ReportGenerator interface {
	Generate(context.Context, *models.DailyReportConfig, []models.DailyReportCandidate) (AIResult, error)
	OptimizeOutline(context.Context, string, string, *int64) ([]OutlineSection, error)
}

type Store interface {
	GetDailyReportConfig() (*models.DailyReportConfig, error)
	SaveDailyReportConfig(*models.DailyReportConfig, []int64) error
	GetDailyReportSelectedFeedIDs() ([]int64, error)
	ListDailyReportCandidates(models.DailyReportCandidateFilter) ([]models.DailyReportCandidate, error)
	ListDailyReportReferencedArticleIDs(time.Time) ([]int64, error)
	CreateDailyReportRun(*models.DailyReportRun) (int64, error)
	UpdateDailyReportRun(*models.DailyReportRun) error
	GetDailyReportRun(int64) (*models.DailyReportRun, error)
	ListDailyReportRuns(models.DailyReportRunFilter) ([]models.DailyReportRun, int, error)
	DeleteDailyReportRun(int64) error
	SetDailyReportRunRead(int64, bool) error
	ReplaceDailyReportSources(int64, []models.DailyReportSource) error
	GetDailyReportSources(int64) ([]models.DailyReportSource, error)
	CountUnreadDailyReportRuns() (int, error)
	MarkRunningDailyReportsInterrupted(time.Time) error
	HasDailyReportRun(time.Time, time.Time, string) (bool, error)
	SetDailyReportLastHandledBoundary(time.Time) error
	GetFeeds() ([]models.Feed, error)
	GetAIProfile(int64) (*models.AIProfile, error)
	GetDefaultAIProfile() (*models.AIProfile, error)
	GetSetting(string) (string, error)
	GetEncryptedSetting(string) (string, error)
}

func parseOutline(raw string) ([]OutlineSection, error) {
	if raw == "" {
		return defaultOutline(), nil
	}
	var outline []OutlineSection
	if err := json.Unmarshal([]byte(raw), &outline); err != nil {
		return nil, err
	}
	return outline, nil
}

func defaultOutline() []OutlineSection {
	return []OutlineSection{
		{ID: "highlights", Title: "重点速览", Instruction: "提炼最重要的新进展和结论"},
		{ID: "topics", Title: "主题动态", Instruction: "按主题归纳主要动态"},
		{ID: "watch", Title: "值得关注", Instruction: "列出后续值得持续关注的线索"},
	}
}
