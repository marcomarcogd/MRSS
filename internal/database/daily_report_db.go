package database

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"MRSS/internal/models"
)

// ErrDailyReportRunExists is returned when an automatic or backfill report
// already owns the same period. Manual reports intentionally allow duplicates.
var ErrDailyReportRunExists = errors.New("daily report run already exists for period")

// GetDailyReportConfig returns the singleton report configuration.
func (db *DB) GetDailyReportConfig() (*models.DailyReportConfig, error) {
	db.WaitForReady()

	var config models.DailyReportConfig
	var aiProfileID sql.NullInt64
	var lastHandledBoundary sql.NullTime
	var cloudConsentAt sql.NullTime
	err := db.QueryRow(`
		SELECT id, enabled, schedule_time, feed_scope, include_hidden,
			ai_profile_id, focus, outline_json, language, title_template,
			in_app_notification, system_notification, notify_on_empty,
			last_handled_boundary, cloud_consent_version, cloud_consent_at,
			cloud_consent_destination_fingerprint, created_at, updated_at
		FROM daily_report_config WHERE id = 1
	`).Scan(
		&config.ID, &config.Enabled, &config.ScheduleTime, &config.FeedScope,
		&config.IncludeHidden, &aiProfileID, &config.Focus, &config.OutlineJSON,
		&config.Language, &config.TitleTemplate, &config.InAppNotification,
		&config.SystemNotification, &config.NotifyOnEmpty, &lastHandledBoundary,
		&config.CloudConsentVersion, &cloudConsentAt, &config.CloudConsentFingerprint,
		&config.CreatedAt, &config.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get daily report config: %w", err)
	}
	if aiProfileID.Valid {
		config.AIProfileID = &aiProfileID.Int64
	}
	if lastHandledBoundary.Valid {
		config.LastHandledBoundary = &lastHandledBoundary.Time
	}
	if cloudConsentAt.Valid {
		config.CloudConsentAt = &cloudConsentAt.Time
	}

	feedIDs, err := db.GetDailyReportSelectedFeedIDs()
	if err != nil {
		return nil, err
	}
	config.FeedIDs = feedIDs
	return &config, nil
}

// SaveDailyReportConfig atomically updates configuration and selected feeds.
func (db *DB) SaveDailyReportConfig(config *models.DailyReportConfig, feedIDs []int64) error {
	db.WaitForReady()
	if config == nil {
		return errors.New("daily report config is nil")
	}
	if config.FeedScope != "all" && config.FeedScope != "selected" {
		return fmt.Errorf("invalid daily report feed scope %q", config.FeedScope)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin daily report config transaction: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	if config.CreatedAt.IsZero() {
		config.CreatedAt = now
	}
	config.ID = 1
	config.UpdatedAt = now
	_, err = tx.Exec(`
		INSERT INTO daily_report_config (
			id, enabled, schedule_time, feed_scope, include_hidden, ai_profile_id,
			focus, outline_json, language, title_template, in_app_notification,
			system_notification, notify_on_empty, last_handled_boundary,
			cloud_consent_version, cloud_consent_at,
			cloud_consent_destination_fingerprint, created_at, updated_at
		) VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			enabled = excluded.enabled,
			schedule_time = excluded.schedule_time,
			feed_scope = excluded.feed_scope,
			include_hidden = excluded.include_hidden,
			ai_profile_id = excluded.ai_profile_id,
			focus = excluded.focus,
			outline_json = excluded.outline_json,
			language = excluded.language,
			title_template = excluded.title_template,
			in_app_notification = excluded.in_app_notification,
			system_notification = excluded.system_notification,
			notify_on_empty = excluded.notify_on_empty,
			last_handled_boundary = excluded.last_handled_boundary,
			cloud_consent_version = excluded.cloud_consent_version,
			cloud_consent_at = excluded.cloud_consent_at,
			cloud_consent_destination_fingerprint = excluded.cloud_consent_destination_fingerprint,
			updated_at = excluded.updated_at
	`, config.Enabled, config.ScheduleTime, config.FeedScope, config.IncludeHidden,
		config.AIProfileID, config.Focus, config.OutlineJSON, config.Language,
		config.TitleTemplate, config.InAppNotification, config.SystemNotification,
		config.NotifyOnEmpty, config.LastHandledBoundary, config.CloudConsentVersion,
		config.CloudConsentAt, config.CloudConsentFingerprint, config.CreatedAt,
		config.UpdatedAt)
	if err != nil {
		return fmt.Errorf("save daily report config: %w", err)
	}

	if _, err = tx.Exec(`DELETE FROM daily_report_config_feeds WHERE config_id = 1`); err != nil {
		return fmt.Errorf("clear daily report selected feeds: %w", err)
	}
	if config.FeedScope == "selected" {
		seen := make(map[int64]struct{}, len(feedIDs))
		for _, feedID := range feedIDs {
			if feedID <= 0 {
				continue
			}
			if _, exists := seen[feedID]; exists {
				continue
			}
			seen[feedID] = struct{}{}
			if _, err = tx.Exec(`
				INSERT INTO daily_report_config_feeds (config_id, feed_id)
				SELECT 1, id FROM feeds WHERE id = ?
			`, feedID); err != nil {
				return fmt.Errorf("save daily report selected feed %d: %w", feedID, err)
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit daily report config: %w", err)
	}
	config.FeedIDs = append([]int64(nil), feedIDs...)
	return nil
}

// GetDailyReportSelectedFeedIDs lists configured feed IDs in user feed order.
func (db *DB) GetDailyReportSelectedFeedIDs() ([]int64, error) {
	db.WaitForReady()
	rows, err := db.Query(`
		SELECT drcf.feed_id
		FROM daily_report_config_feeds drcf
		JOIN feeds f ON f.id = drcf.feed_id
		WHERE drcf.config_id = 1
		ORDER BY COALESCE(f.position, 0), f.id
	`)
	if err != nil {
		return nil, fmt.Errorf("list daily report selected feeds: %w", err)
	}
	defer rows.Close()

	feedIDs := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan daily report selected feed: %w", err)
		}
		feedIDs = append(feedIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate daily report selected feeds: %w", err)
	}
	return feedIDs, nil
}

// ListDailyReportCandidates returns articles in [PeriodStart, PeriodEnd).
// Old entries first observed in this period are included as late arrivals.
func (db *DB) ListDailyReportCandidates(filter models.DailyReportCandidateFilter) ([]models.DailyReportCandidate, error) {
	db.WaitForReady()
	if filter.PeriodStart.IsZero() || filter.PeriodEnd.IsZero() || !filter.PeriodEnd.After(filter.PeriodStart) {
		return nil, errors.New("invalid daily report period")
	}

	publishedJulian := sqliteTimeJulian("a.published_at")
	firstSeenJulian := sqliteTimeJulian("a.first_seen_at")
	publicationValid := `COALESCE(a.has_valid_published_time, 1) = 1 AND a.published_at IS NOT NULL AND TRIM(CAST(a.published_at AS TEXT)) != '' AND ` + publishedJulian + ` IS NOT NULL AND ` + publishedJulian + ` <= julianday(?)`
	query := `
		SELECT a.id, a.feed_id, COALESCE(a.title, ''), COALESCE(a.author, ''),
			COALESCE(a.url, ''), COALESCE(f.title, ''),
			COALESCE(NULLIF(a.original_summary, ''), NULLIF(a.summary, ''), ''),
			COALESCE(ac.content, ''), COALESCE(a.unique_id, ''), a.published_at, a.first_seen_at,
			CASE WHEN ` + publicationValid + ` THEN 1 ELSE 0 END AS has_valid_published_time,
			CASE WHEN ` + publicationValid + `
				AND ` + publishedJulian + ` < julianday(?)
				AND ` + firstSeenJulian + ` >= julianday(?)
				AND ` + firstSeenJulian + ` < julianday(?)
			THEN 1 ELSE 0 END AS late_arrival
		FROM articles a
		JOIN feeds f ON f.id = a.feed_id
		LEFT JOIN article_contents ac ON ac.article_id = a.id
		WHERE (
			(` + publicationValid + `
				AND ` + publishedJulian + ` >= julianday(?)
				AND ` + publishedJulian + ` < julianday(?))
			OR
			((NOT (` + publicationValid + `) OR ` + publishedJulian + ` < julianday(?))
				AND ` + firstSeenJulian + ` >= julianday(?)
				AND ` + firstSeenJulian + ` < julianday(?))
		)
	`
	periodStart := filter.PeriodStart.UTC().Format(time.RFC3339Nano)
	periodEnd := filter.PeriodEnd.UTC().Format(time.RFC3339Nano)
	args := []any{
		periodEnd,
		periodEnd, periodStart, periodStart, periodEnd,
		periodEnd, periodStart, periodEnd,
		periodEnd, periodStart, periodStart, periodEnd,
	}

	if !filter.IncludeHidden {
		query += ` AND COALESCE(a.is_hidden, 0) = 0`
	}
	if len(filter.FeedIDs) > 0 {
		query += ` AND a.feed_id IN (` + placeholders(len(filter.FeedIDs)) + `)`
		for _, id := range filter.FeedIDs {
			args = append(args, id)
		}
	}
	if len(filter.ExcludeArticleIDs) > 0 {
		query += ` AND a.id NOT IN (` + placeholders(len(filter.ExcludeArticleIDs)) + `)`
		for _, id := range filter.ExcludeArticleIDs {
			args = append(args, id)
		}
	}
	if !filter.Manual {
		query += ` AND NOT EXISTS (
			SELECT 1 FROM daily_report_sources drs
			JOIN daily_report_runs drr ON drr.id = drs.run_id
			WHERE (drs.article_id = a.id OR (drs.article_unique_id != '' AND drs.article_unique_id = a.unique_id))
				AND drr.kind != 'manual'
				AND drr.status IN ('completed', 'partial', 'no_content')
		)`
	}
	query += ` ORDER BY COALESCE(` + publishedJulian + `, ` + firstSeenJulian + `) ASC, a.id ASC`

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list daily report candidates: %w", err)
	}
	defer rows.Close()

	candidates := make([]models.DailyReportCandidate, 0)
	for rows.Next() {
		var candidate models.DailyReportCandidate
		var publishedAt, firstSeenAt sql.NullTime
		if err := rows.Scan(
			&candidate.ArticleID, &candidate.FeedID, &candidate.Title,
			&candidate.Author, &candidate.URL, &candidate.FeedTitle,
			&candidate.Summary, &candidate.Content, &candidate.UniqueID, &publishedAt, &firstSeenAt,
			&candidate.HasValidPublishedTime, &candidate.LateArrival,
		); err != nil {
			return nil, fmt.Errorf("scan daily report candidate: %w", err)
		}
		if publishedAt.Valid {
			candidate.PublishedAt = publishedAt.Time
		}
		if firstSeenAt.Valid {
			candidate.FirstSeenAt = firstSeenAt.Time
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate daily report candidates: %w", err)
	}
	return candidates, nil
}

// ListDailyReportReferencedArticleIDs lists articles already consumed by
// successful non-manual reports before a boundary.
func (db *DB) ListDailyReportReferencedArticleIDs(before time.Time) ([]int64, error) {
	db.WaitForReady()
	query := `
		SELECT DISTINCT drs.article_id
		FROM daily_report_sources drs
		JOIN daily_report_runs drr ON drr.id = drs.run_id
		WHERE drs.article_id IS NOT NULL
			AND drr.kind != 'manual'
			AND drr.status IN ('completed', 'partial', 'no_content')
	`
	args := make([]any, 0, 1)
	if !before.IsZero() {
		query += ` AND ` + sqliteTimeJulian("drr.period_end") + ` <= julianday(?)`
		args = append(args, before.UTC().Format(time.RFC3339Nano))
	}
	query += ` ORDER BY drs.article_id`
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list referenced daily report articles: %w", err)
	}
	defer rows.Close()

	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan referenced daily report article: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// CreateDailyReportRun inserts a report run and applies the automatic-period
// uniqueness constraint.
func (db *DB) CreateDailyReportRun(run *models.DailyReportRun) (int64, error) {
	db.WaitForReady()
	if run == nil {
		return 0, errors.New("daily report run is nil")
	}
	if run.CreatedAt.IsZero() {
		run.CreatedAt = time.Now().UTC()
	}
	if run.Status == "" {
		run.Status = "queued"
	}
	if run.ConfigSnapshot == "" {
		run.ConfigSnapshot = "{}"
	}
	if err := validateSafeConfigSnapshot(run.ConfigSnapshot); err != nil {
		return 0, err
	}

	result, err := db.Exec(`
		INSERT INTO daily_report_runs (
			kind, status, period_start, period_end, progress, current_step,
			title, content_json, markdown, config_snapshot, input_tokens,
			output_tokens, total_tokens, article_count, ai_used, is_read,
			error, failure_code, generation_mode, generation_fingerprint,
			generation_checkpoint, retry_of_id, created_at, started_at, completed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, run.Kind, run.Status, run.PeriodStart, run.PeriodEnd, run.Progress,
		run.CurrentStep, run.Title, run.ContentJSON, run.Markdown,
		run.ConfigSnapshot, run.InputTokens, run.OutputTokens, run.TotalTokens,
		run.ArticleCount, run.AIUsed, run.IsRead, run.Error, run.FailureCode,
		run.GenerationMode, run.GenerationHash, run.CheckpointJSON, run.RetryOfID,
		run.CreatedAt, run.StartedAt, run.CompletedAt)
	if err != nil {
		if run.Kind != "manual" {
			exists, existsErr := db.HasDailyReportRun(run.PeriodStart, run.PeriodEnd, run.Kind)
			if existsErr == nil && exists {
				return 0, ErrDailyReportRunExists
			}
		}
		return 0, fmt.Errorf("create daily report run: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get daily report run id: %w", err)
	}
	run.ID = id
	return id, nil
}

// UpdateDailyReportRun updates all mutable report run fields.
func (db *DB) UpdateDailyReportRun(run *models.DailyReportRun) error {
	db.WaitForReady()
	if run == nil || run.ID <= 0 {
		return errors.New("invalid daily report run")
	}
	if run.ConfigSnapshot == "" {
		run.ConfigSnapshot = "{}"
	}
	if err := validateSafeConfigSnapshot(run.ConfigSnapshot); err != nil {
		return err
	}
	result, err := db.Exec(`
		UPDATE daily_report_runs SET
			kind = ?, status = ?, period_start = ?, period_end = ?, progress = ?,
			current_step = ?, title = ?, content_json = ?, markdown = ?,
			config_snapshot = ?, input_tokens = ?, output_tokens = ?,
			total_tokens = ?, article_count = ?, ai_used = ?, is_read = ?,
			error = ?, failure_code = ?, generation_mode = ?,
			generation_fingerprint = ?, generation_checkpoint = ?, retry_of_id = ?,
			started_at = ?, completed_at = ?
		WHERE id = ?
	`, run.Kind, run.Status, run.PeriodStart, run.PeriodEnd, run.Progress,
		run.CurrentStep, run.Title, run.ContentJSON, run.Markdown,
		run.ConfigSnapshot, run.InputTokens, run.OutputTokens, run.TotalTokens,
		run.ArticleCount, run.AIUsed, run.IsRead, run.Error, run.FailureCode,
		run.GenerationMode, run.GenerationHash, run.CheckpointJSON, run.RetryOfID,
		run.StartedAt, run.CompletedAt, run.ID)
	if err != nil {
		return fmt.Errorf("update daily report run: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDailyReportRun(scanner rowScanner) (*models.DailyReportRun, error) {
	var run models.DailyReportRun
	var retryOfID sql.NullInt64
	var startedAt, completedAt sql.NullTime
	err := scanner.Scan(
		&run.ID, &run.Kind, &run.Status, &run.PeriodStart, &run.PeriodEnd,
		&run.Progress, &run.CurrentStep, &run.Title, &run.ContentJSON,
		&run.Markdown, &run.ConfigSnapshot, &run.InputTokens, &run.OutputTokens,
		&run.TotalTokens, &run.ArticleCount, &run.AIUsed, &run.IsRead,
		&run.Error, &run.FailureCode, &run.GenerationMode, &run.GenerationHash,
		&run.CheckpointJSON, &retryOfID, &run.CreatedAt, &startedAt, &completedAt,
	)
	if err != nil {
		return nil, err
	}
	if retryOfID.Valid {
		run.RetryOfID = &retryOfID.Int64
	}
	if startedAt.Valid {
		run.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		run.CompletedAt = &completedAt.Time
	}
	return &run, nil
}

const dailyReportRunColumns = `
	id, kind, status, period_start, period_end, progress, current_step,
	title, content_json, markdown, config_snapshot, input_tokens, output_tokens,
	total_tokens, article_count, ai_used, is_read, error, failure_code,
	generation_mode, generation_fingerprint, generation_checkpoint, retry_of_id,
	created_at, started_at, completed_at`

// GetDailyReportRun returns one report history entry.
func (db *DB) GetDailyReportRun(id int64) (*models.DailyReportRun, error) {
	db.WaitForReady()
	run, err := scanDailyReportRun(db.QueryRow(`SELECT `+dailyReportRunColumns+` FROM daily_report_runs WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get daily report run: %w", err)
	}
	return run, nil
}

// ListDailyReportRuns returns paginated report history and total count.
func (db *DB) ListDailyReportRuns(filter models.DailyReportRunFilter) ([]models.DailyReportRun, int, error) {
	db.WaitForReady()
	if filter.Limit <= 0 {
		filter.Limit = 20
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}

	where := ""
	args := make([]any, 0, 3)
	if filter.Status != "" {
		where = " WHERE status = ?"
		args = append(args, filter.Status)
	}
	var total int
	if err := db.QueryRow(`SELECT COUNT(*) FROM daily_report_runs`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count daily report runs: %w", err)
	}

	queryArgs := append(append([]any(nil), args...), filter.Limit, filter.Offset)
	rows, err := db.Query(`SELECT `+dailyReportRunColumns+` FROM daily_report_runs`+where+` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list daily report runs: %w", err)
	}
	defer rows.Close()

	runs := make([]models.DailyReportRun, 0)
	for rows.Next() {
		run, err := scanDailyReportRun(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan daily report run: %w", err)
		}
		runs = append(runs, *run)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate daily report runs: %w", err)
	}
	return runs, total, nil
}

// DeleteDailyReportRun deletes a report and cascades its source snapshots.
func (db *DB) DeleteDailyReportRun(id int64) error {
	db.WaitForReady()
	_, err := db.Exec(`DELETE FROM daily_report_runs WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete daily report run: %w", err)
	}
	return nil
}

// SetDailyReportRunRead updates the unread badge state.
func (db *DB) SetDailyReportRunRead(id int64, read bool) error {
	db.WaitForReady()
	result, err := db.Exec(`UPDATE daily_report_runs SET is_read = ? WHERE id = ?`, read, id)
	if err != nil {
		return fmt.Errorf("set daily report run read state: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ReplaceDailyReportSources atomically replaces immutable source snapshots.
func (db *DB) ReplaceDailyReportSources(runID int64, sources []models.DailyReportSource) error {
	db.WaitForReady()
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin daily report sources transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`DELETE FROM daily_report_sources WHERE run_id = ?`, runID); err != nil {
		return fmt.Errorf("clear daily report sources: %w", err)
	}
	stmt, err := tx.Prepare(`
		INSERT INTO daily_report_sources (
			run_id, source_index, article_id, feed_id, article_title, feed_title,
			author, url, article_unique_id, published_at, first_seen_at, late_arrival, content_kind,
			created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare daily report source insert: %w", err)
	}
	defer stmt.Close()
	now := time.Now().UTC()
	for i := range sources {
		source := &sources[i]
		if source.CreatedAt.IsZero() {
			source.CreatedAt = now
		}
		source.RunID = runID
		if source.SourceIndex <= 0 {
			source.SourceIndex = i + 1
		}
		if _, err = stmt.Exec(
			runID, source.SourceIndex, source.ArticleID, source.FeedID,
			source.ArticleTitle, source.FeedTitle, source.Author, source.URL,
			source.ArticleUniqueID, source.PublishedAt, source.FirstSeenAt, source.LateArrival,
			source.ContentKind, source.CreatedAt,
		); err != nil {
			return fmt.Errorf("insert daily report source %d: %w", source.SourceIndex, err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit daily report sources: %w", err)
	}
	return nil
}

// GetDailyReportSources returns source snapshots in citation order.
func (db *DB) GetDailyReportSources(runID int64) ([]models.DailyReportSource, error) {
	db.WaitForReady()
	rows, err := db.Query(`
		SELECT id, run_id, source_index, article_id, feed_id, article_title,
			feed_title, author, url, article_unique_id, published_at, first_seen_at, late_arrival,
			content_kind, created_at
		FROM daily_report_sources WHERE run_id = ?
		ORDER BY source_index ASC
	`, runID)
	if err != nil {
		return nil, fmt.Errorf("get daily report sources: %w", err)
	}
	defer rows.Close()
	sources := make([]models.DailyReportSource, 0)
	for rows.Next() {
		var source models.DailyReportSource
		var articleID, feedID sql.NullInt64
		var publishedAt, firstSeenAt sql.NullTime
		if err := rows.Scan(
			&source.ID, &source.RunID, &source.SourceIndex, &articleID, &feedID,
			&source.ArticleTitle, &source.FeedTitle, &source.Author, &source.URL,
			&source.ArticleUniqueID, &publishedAt, &firstSeenAt, &source.LateArrival, &source.ContentKind,
			&source.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan daily report source: %w", err)
		}
		if articleID.Valid {
			source.ArticleID = &articleID.Int64
		}
		if feedID.Valid {
			source.FeedID = &feedID.Int64
		}
		if publishedAt.Valid {
			source.PublishedAt = &publishedAt.Time
		}
		if firstSeenAt.Valid {
			source.FirstSeenAt = &firstSeenAt.Time
		}
		sources = append(sources, source)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate daily report sources: %w", err)
	}
	return sources, nil
}

// CountUnreadDailyReportRuns returns the activity bar badge count.
func (db *DB) CountUnreadDailyReportRuns() (int, error) {
	db.WaitForReady()
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM daily_report_runs
		WHERE is_read = 0 AND status IN ('completed', 'partial', 'no_content')
	`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count unread daily report runs: %w", err)
	}
	return count, nil
}

// MarkRunningDailyReportsInterrupted makes shutdown recovery explicit.
func (db *DB) MarkRunningDailyReportsInterrupted(now time.Time) error {
	db.WaitForReady()
	_, err := db.Exec(`
		UPDATE daily_report_runs
		SET status = 'interrupted', error = CASE WHEN error = '' THEN 'application stopped during generation' ELSE error END,
			completed_at = ?
		WHERE status IN ('queued', 'refreshing', 'generating')
	`, now)
	if err != nil {
		return fmt.Errorf("mark daily report runs interrupted: %w", err)
	}
	return nil
}

// GetDailyReportLastHandledBoundary returns the scheduler baseline.
func (db *DB) GetDailyReportLastHandledBoundary() (*time.Time, error) {
	db.WaitForReady()
	var value sql.NullTime
	if err := db.QueryRow(`SELECT last_handled_boundary FROM daily_report_config WHERE id = 1`).Scan(&value); err != nil {
		return nil, fmt.Errorf("get daily report last handled boundary: %w", err)
	}
	if !value.Valid {
		return nil, nil
	}
	return &value.Time, nil
}

// SetDailyReportLastHandledBoundary advances the scheduler baseline without
// rewriting the rest of the configuration.
func (db *DB) SetDailyReportLastHandledBoundary(boundary time.Time) error {
	db.WaitForReady()
	_, err := db.Exec(`
		UPDATE daily_report_config
		SET last_handled_boundary = ?, updated_at = ?
		WHERE id = 1
	`, boundary, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("set daily report last handled boundary: %w", err)
	}
	return nil
}

// HasDailyReportRun checks period ownership. Automatic and backfill runs share
// one uniqueness domain; manual runs are checked by kind only.
func (db *DB) HasDailyReportRun(periodStart, periodEnd time.Time, kind string) (bool, error) {
	db.WaitForReady()
	query := `SELECT EXISTS(SELECT 1 FROM daily_report_runs WHERE period_start = ? AND period_end = ?`
	args := []any{periodStart, periodEnd}
	if kind == "manual" {
		query += ` AND kind = 'manual'`
	} else {
		query += ` AND kind != 'manual' AND status NOT IN ('failed', 'interrupted')`
	}
	query += `)`
	var exists bool
	if err := db.QueryRow(query, args...).Scan(&exists); err != nil {
		return false, fmt.Errorf("check daily report run: %w", err)
	}
	return exists, nil
}

func placeholders(count int) string {
	if count <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", count), ",")
}

// modernc SQLite stores Go time.Time values as "YYYY-MM-DD HH:MM:SS +HHMM TZ".
// SQLite's date functions do not understand that suffix, so normalize it to an
// RFC3339-compatible offset while still accepting CURRENT_TIMESTAMP values.
func sqliteTimeJulian(column string) string {
	text := `CAST(` + column + ` AS TEXT)`
	return `julianday(CASE
		WHEN LENGTH(` + text + `) >= 25 AND SUBSTR(` + text + `, 20, 1) IN ('+', '-')
		THEN SUBSTR(` + text + `, 1, 19) || SUBSTR(` + text + `, 20, 6)
		WHEN LENGTH(` + text + `) >= 26 AND SUBSTR(` + text + `, 21, 1) IN ('+', '-')
		THEN SUBSTR(` + text + `, 1, 19) || SUBSTR(` + text + `, 21, 3) || ':' || SUBSTR(` + text + `, 24, 2)
		ELSE SUBSTR(` + text + `, 1, 19)
	END)`
}

func validateSafeConfigSnapshot(snapshot string) error {
	var value any
	if err := json.Unmarshal([]byte(snapshot), &value); err != nil {
		return fmt.Errorf("invalid daily report config snapshot: %w", err)
	}
	if snapshotContainsSensitiveKey(value) {
		return errors.New("daily report config snapshot contains sensitive fields")
	}
	return nil
}

func snapshotContainsSensitiveKey(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
			switch normalized {
			case "apikey", "authorization", "customheaders", "accesstoken", "secret":
				return true
			}
			if snapshotContainsSensitiveKey(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if snapshotContainsSensitiveKey(child) {
				return true
			}
		}
	}
	return false
}
