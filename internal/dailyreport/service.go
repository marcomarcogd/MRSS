package dailyreport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"MRSS/internal/models"
)

var (
	ErrAlreadyRunning        = errors.New("a daily report is already running")
	ErrRunNotFound           = errors.New("daily report run not found")
	ErrServiceStopping       = errors.New("daily report service is stopping")
	ErrCheckpointInvalidated = errors.New("daily report inputs changed after the checkpoint was created")
)

type preparedRunInputs struct {
	candidates        []models.DailyReportCandidate
	carryInputTokens  int64
	carryOutputTokens int64
}

type Service struct {
	store     Store
	refresher Refresher
	generator ReportGenerator
	notifier  Notifier
	clock     Clock
	location  *time.Location

	mu           sync.RWMutex
	running      bool
	currentRunID *int64
	progress     int
	currentStep  string
	wake         func()
	lifecycleCtx context.Context
	runWG        sync.WaitGroup
	stopping     bool
}

func NewService(store Store, refresher Refresher, generator ReportGenerator, notifier Notifier, clock Clock, location *time.Location) *Service {
	if generator == nil {
		generator = LocalGenerator{}
	}
	if notifier == nil {
		notifier = NoopNotifier{}
	}
	if clock == nil {
		clock = RealClock()
	}
	if location == nil {
		location = time.Local
	}
	return &Service{
		store: store, refresher: refresher, generator: generator, notifier: notifier,
		clock: clock, location: location, lifecycleCtx: context.Background(),
	}
}

// SetLifecycleContext provides the application lifetime used by detached HTTP
// operations. Request cancellation must not stop a report after a 202 response.
func (s *Service) SetLifecycleContext(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	s.lifecycleCtx = ctx
	s.stopping = false
	s.mu.Unlock()
}

// BeginShutdown prevents new detached work from racing with the final wait.
// Existing jobs remain tracked until they have persisted their terminal state.
func (s *Service) BeginShutdown() {
	s.mu.Lock()
	s.stopping = true
	s.mu.Unlock()
}

func (s *Service) SetNotifier(notifier Notifier) {
	if notifier == nil {
		notifier = NoopNotifier{}
	}
	s.mu.Lock()
	s.notifier = notifier
	s.mu.Unlock()
}

func (s *Service) SetWake(wake func()) {
	s.mu.Lock()
	s.wake = wake
	s.mu.Unlock()
}

func (s *Service) GetConfig() (*models.DailyReportConfig, error) {
	config, err := s.store.GetDailyReportConfig()
	if err != nil {
		return nil, err
	}
	if config == nil {
		config = &models.DailyReportConfig{}
	}
	feedIDs, err := s.store.GetDailyReportSelectedFeedIDs()
	if err != nil {
		return nil, err
	}
	config.FeedIDs = feedIDs
	s.applyLocalizedDefaults(config)
	if err := normalizeConfig(config, s.clock.Now()); err != nil {
		return nil, err
	}
	consentInvalidated, err := s.invalidateStaleCloudConsent(config)
	if err != nil {
		return nil, err
	}
	requiresPause := config.Enabled && config.FeedScope == "selected" && len(feedIDs) == 0
	if config.Enabled {
		cloudProcessing, cloudErr := s.GetCloudProcessing(config)
		if cloudErr != nil {
			return nil, cloudErr
		}
		requiresPause = requiresPause || (cloudProcessing.Required && !cloudProcessing.Accepted)
	}
	if requiresPause || consentInvalidated {
		// Any destination change invalidates the previous automatic-processing
		// decision. Local targets do not need cloud consent, but the schedule still
		// remains paused until the user explicitly enables the new destination.
		config.Enabled = false
		if err := s.store.SaveDailyReportConfig(config, feedIDs); err != nil {
			return nil, err
		}
	}
	return config, nil
}

func (s *Service) SaveConfig(config *models.DailyReportConfig) (*models.DailyReportConfig, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}
	now := s.clock.Now()
	previous, err := s.store.GetDailyReportConfig()
	if err != nil {
		return nil, err
	}
	if previous != nil && !previous.CreatedAt.IsZero() {
		config.CreatedAt = previous.CreatedAt
	}
	if previous != nil {
		config.CloudConsentVersion = previous.CloudConsentVersion
		config.CloudConsentAt = previous.CloudConsentAt
		config.CloudConsentFingerprint = previous.CloudConsentFingerprint
	}
	s.applyLocalizedDefaults(config)
	if err := normalizeConfig(config, now); err != nil {
		return nil, err
	}
	if config.FeedScope == "selected" && len(config.FeedIDs) == 0 {
		return nil, fmt.Errorf("feed_ids is required when feed_scope is selected")
	}
	if config.AIProfileID != nil {
		if *config.AIProfileID <= 0 {
			return nil, fmt.Errorf("ai_profile_id must be a positive integer")
		}
		profile, profileErr := s.store.GetAIProfile(*config.AIProfileID)
		if profileErr != nil || profile == nil {
			return nil, fmt.Errorf("ai_profile_id is invalid")
		}
	}
	consentInvalidated, err := s.invalidateStaleCloudConsent(config)
	if err != nil {
		return nil, err
	}
	var previousProfileID *int64
	if previous != nil {
		previousProfileID = previous.AIProfileID
	}
	profileSelectionChanged := !sameOptionalInt64(previousProfileID, config.AIProfileID)
	if config.FeedScope == "selected" {
		feeds, feedErr := s.store.GetFeeds()
		if feedErr != nil {
			return nil, feedErr
		}
		available := make(map[int64]struct{}, len(feeds))
		for _, item := range feeds {
			available[item.ID] = struct{}{}
		}
		seen := make(map[int64]struct{}, len(config.FeedIDs))
		for _, id := range config.FeedIDs {
			if id <= 0 {
				return nil, fmt.Errorf("feed_ids must contain positive integers")
			}
			if _, duplicate := seen[id]; duplicate {
				return nil, fmt.Errorf("feed_ids must not contain duplicates")
			}
			seen[id] = struct{}{}
			if _, exists := available[id]; !exists {
				return nil, fmt.Errorf("feed_ids contains an unknown feed")
			}
		}
	}
	var consentErr error
	if config.Enabled || consentInvalidated || profileSelectionChanged {
		consentErr = s.ensureCloudProcessingConsent(config)
		if consentErr != nil {
			// Persist the user's new destination and the rest of the draft safely
			// in a paused state. The consent endpoint can then disclose and bind
			// the exact pending target before the UI retries this PUT once.
			config.Enabled = false
		}
	}
	if config.CreatedAt.IsZero() {
		config.CreatedAt = now
	}
	config.UpdatedAt = now
	if config.Enabled && (previous == nil || !previous.Enabled || previous.ScheduleTime != config.ScheduleTime) {
		boundary, boundaryErr := ScheduledBoundary(now, config.ScheduleTime, s.location)
		if boundaryErr != nil {
			return nil, boundaryErr
		}
		config.LastHandledBoundary = &boundary
	} else if previous != nil {
		config.LastHandledBoundary = previous.LastHandledBoundary
	}
	feedIDs := config.FeedIDs
	if config.FeedScope == "all" {
		feedIDs = nil
	}
	if err := s.store.SaveDailyReportConfig(config, feedIDs); err != nil {
		return nil, err
	}
	s.mu.RLock()
	wake := s.wake
	s.mu.RUnlock()
	if wake != nil {
		wake()
	}
	if consentErr != nil {
		return nil, consentErr
	}
	return s.GetConfig()
}

func sameOptionalInt64(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func (s *Service) OptimizeOutline(ctx context.Context, focus, language string, profileID *int64) ([]OutlineSection, error) {
	config, err := s.GetConfig()
	if err != nil {
		return nil, err
	}
	config.AIProfileID = profileID
	if err := s.ensureCloudProcessingConsent(config); err != nil {
		return nil, err
	}
	return s.generator.OptimizeOutline(ctx, focus, language, profileID)
}

func (s *Service) Preview(periodStart, periodEnd *time.Time) (*Preview, error) {
	config, err := s.GetConfig()
	if err != nil {
		return nil, err
	}
	start, end, err := s.resolvePeriod(config, periodStart, periodEnd)
	if err != nil {
		return nil, err
	}
	candidates, err := s.candidates(config, start, end, RunKindManual)
	if err != nil {
		return nil, err
	}
	batches := (len(candidates) + 9) / 10
	if batches == 0 && len(candidates) > 0 {
		batches = 1
	}
	return &Preview{PeriodStart: start, PeriodEnd: end, ArticleCount: len(candidates), EstimatedBatches: batches}, nil
}

func (s *Service) StartManual(ctx context.Context, periodStart, periodEnd *time.Time) (*models.DailyReportRun, error) {
	config, err := s.GetConfig()
	if err != nil {
		return nil, err
	}
	start, end, err := s.resolvePeriod(config, periodStart, periodEnd)
	if err != nil {
		return nil, err
	}
	if err := s.ensureCloudProcessingConsent(config); err != nil {
		return nil, err
	}
	return s.startOne(ctx, config, RunKindManual, start, end, nil)
}

func (s *Service) Retry(ctx context.Context, id int64, restart ...bool) (*models.DailyReportRun, error) {
	original, err := s.store.GetDailyReportRun(id)
	if err != nil {
		return nil, err
	}
	if original == nil {
		return nil, ErrRunNotFound
	}
	if original.Status != RunStatusFailed && original.Status != RunStatusInterrupted {
		return nil, fmt.Errorf("only failed or interrupted reports can be retried")
	}
	config, err := s.GetConfig()
	if err != nil {
		return nil, err
	}
	if err := s.ensureCloudProcessingConsent(config); err != nil {
		return nil, err
	}
	candidates, err := s.candidates(config, original.PeriodStart, original.PeriodEnd, RunKindManual)
	if err != nil {
		return nil, err
	}

	restartRequested := len(restart) > 0 && restart[0]
	if !restartRequested {
		state, inspectErr := s.inspectRetry(ctx, original, config, candidates)
		if inspectErr != nil {
			return nil, inspectErr
		}
		if state.Action != RetryActionResume {
			return nil, generationFailure("preparing", "checkpoint_invalidated", ErrCheckpointInvalidated)
		}
	}
	if err := s.claimRun(); err != nil {
		return nil, err
	}
	prepared := &preparedRunInputs{
		candidates: append([]models.DailyReportCandidate(nil), candidates...),
	}
	if restartRequested {
		prepared.carryInputTokens = original.InputTokens
		prepared.carryOutputTokens = original.OutputTokens
		original.GenerationHash = ""
		original.CheckpointJSON = ""
	} else {
		prepared.carryInputTokens, prepared.carryOutputTokens = retryTokenCarry(original)
	}
	original.Status = RunStatusQueued
	original.Progress = 0
	original.CurrentStep = "queued"
	original.ContentJSON = ""
	original.Markdown = ""
	original.ConfigSnapshot = safeConfigSnapshot(config)
	original.ArticleCount = len(candidates)
	original.IsRead = false
	original.Error = ""
	original.FailureCode = ""
	original.GenerationMode = "ai"
	original.StartedAt = nil
	original.CompletedAt = nil
	if err := s.store.UpdateDailyReportRun(original); err != nil {
		s.releaseRun()
		return nil, err
	}
	s.setCurrent(original)
	runCtx, stop := s.detachedContext(ctx)
	go func() {
		defer stop()
		defer s.releaseRun()
		s.executePrepared(runCtx, config, original, prepared)
	}()
	return original, nil
}

func retryTokenCarry(run *models.DailyReportRun) (int64, int64) {
	if run == nil || strings.TrimSpace(run.CheckpointJSON) == "" {
		return 0, 0
	}
	var checkpoint struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
	}
	if err := json.Unmarshal([]byte(run.CheckpointJSON), &checkpoint); err != nil {
		return 0, 0
	}
	inputCarry := run.InputTokens - checkpoint.InputTokens
	outputCarry := run.OutputTokens - checkpoint.OutputTokens
	if inputCarry < 0 {
		inputCarry = 0
	}
	if outputCarry < 0 {
		outputCarry = 0
	}
	return inputCarry, outputCarry
}

// UseLocalFallback creates a separate, auditable local report for a failed AI
// run. The failed run and its consumed token count remain unchanged.
func (s *Service) UseLocalFallback(ctx context.Context, id int64) (*models.DailyReportRun, error) {
	original, err := s.store.GetDailyReportRun(id)
	if err != nil {
		return nil, err
	}
	if original == nil {
		return nil, ErrRunNotFound
	}
	if original.Status != RunStatusFailed && original.Status != RunStatusInterrupted {
		return nil, fmt.Errorf("local fallback requires a failed or interrupted report")
	}
	config := &models.DailyReportConfig{}
	if err := json.Unmarshal([]byte(original.ConfigSnapshot), config); err != nil {
		return nil, fmt.Errorf("restore report configuration: %w", err)
	}
	s.applyLocalizedDefaults(config)
	return s.startOneMode(ctx, config, RunKindManual, original.PeriodStart, original.PeriodEnd, &id, "local")
}

func (s *Service) startOne(ctx context.Context, config *models.DailyReportConfig, kind string, start, end time.Time, retryOf *int64) (*models.DailyReportRun, error) {
	return s.startOneMode(ctx, config, kind, start, end, retryOf, "")
}

func (s *Service) startOneMode(ctx context.Context, config *models.DailyReportConfig, kind string, start, end time.Time, retryOf *int64, mode string) (*models.DailyReportRun, error) {
	return s.startOneModePrepared(ctx, config, kind, start, end, retryOf, mode, nil)
}

func (s *Service) startOneModePrepared(ctx context.Context, config *models.DailyReportConfig, kind string, start, end time.Time, retryOf *int64, mode string, prepared *preparedRunInputs) (*models.DailyReportRun, error) {
	if err := s.claimRun(); err != nil {
		return nil, err
	}
	run := &models.DailyReportRun{
		Kind: kind, Status: RunStatusQueued, PeriodStart: start, PeriodEnd: end,
		Progress: 0, CurrentStep: "queued", RetryOfID: retryOf, GenerationMode: mode, CreatedAt: s.clock.Now(),
	}
	if retryOf != nil && mode == "" {
		if original, lookupErr := s.store.GetDailyReportRun(*retryOf); lookupErr == nil && original != nil {
			if original.Status == RunStatusFailed || original.Status == RunStatusInterrupted {
				run.GenerationHash = original.GenerationHash
				run.CheckpointJSON = original.CheckpointJSON
			}
		}
	}
	run.ConfigSnapshot = safeConfigSnapshot(config)
	id, err := s.store.CreateDailyReportRun(run)
	if err != nil {
		s.releaseRun()
		return nil, err
	}
	run.ID = id
	s.setCurrent(run)
	runCtx, stop := s.detachedContext(ctx)
	go func() {
		defer stop()
		defer s.releaseRun()
		s.executePrepared(runCtx, config, run, prepared)
	}()
	return run, nil
}

func (s *Service) runScheduled(ctx context.Context, kind string, start, end time.Time) error {
	config, err := s.GetConfig()
	if err != nil {
		return err
	}
	if !config.Enabled {
		return nil
	}
	_, err = s.startOne(ctx, config, kind, start, end, nil)
	return err
}

func (s *Service) execute(ctx context.Context, config *models.DailyReportConfig, run *models.DailyReportRun) {
	s.executePrepared(ctx, config, run, nil)
}

func (s *Service) executePrepared(ctx context.Context, config *models.DailyReportConfig, run *models.DailyReportRun, prepared *preparedRunInputs) {
	now := s.clock.Now()
	run.StartedAt = &now
	run.Status = RunStatusRefreshing
	run.Progress = 10
	run.CurrentStep = "refreshing"
	s.updateRun(run)

	feedIDs, err := s.feedIDs(config)
	if err != nil {
		s.failRun(run, err)
		return
	}
	if config.FeedScope == "selected" && len(feedIDs) == 0 {
		// GetConfig performs the pause against the current persisted config. Do
		// not write the run's stale snapshot over concurrent user changes.
		_, _ = s.GetConfig()
		s.failRun(run, fmt.Errorf("all selected feeds were removed; daily reports were paused"))
		return
	}
	partial := false
	var refreshErrors []RefreshResult
	if prepared == nil && s.refresher != nil && len(feedIDs) > 0 {
		refreshCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		refreshErrors = s.refresher.Refresh(refreshCtx, feedIDs)
		cancel()
		for _, result := range refreshErrors {
			if result.Error != "" {
				partial = true
			}
		}
	}
	if ctx.Err() != nil {
		s.interruptRun(run)
		return
	}

	run.Status = RunStatusGenerating
	run.Progress = 40
	run.CurrentStep = "collecting"
	s.updateRun(run)
	var candidates []models.DailyReportCandidate
	if prepared != nil {
		candidates = append([]models.DailyReportCandidate(nil), prepared.candidates...)
	} else {
		candidates, err = s.candidates(config, run.PeriodStart, run.PeriodEnd, run.Kind)
		if err != nil {
			s.failRun(run, err)
			return
		}
	}
	run.ArticleCount = len(candidates)
	run.Title = formatTitle(config.TitleTemplate, run.PeriodStart, run.PeriodEnd, len(candidates))
	if len(candidates) == 0 {
		run.Status = RunStatusNoContent
		run.Progress = 100
		run.CurrentStep = "completed"
		s.completeRun(ctx, config, run)
		return
	}

	run.Progress = 55
	run.CurrentStep = "generating"
	s.updateRun(run)
	carryInputTokens := int64(0)
	carryOutputTokens := int64(0)
	if prepared != nil {
		carryInputTokens = prepared.carryInputTokens
		carryOutputTokens = prepared.carryOutputTokens
	}
	var result AIResult
	var generateErr error
	if run.GenerationMode == "local" {
		result = localFallback(config, candidates)
		run.GenerationMode = "local"
	} else {
		consentErr := s.ensureCloudProcessingConsent(config)
		if consentErr != nil {
			generateErr = consentErr
		} else if resumable, ok := s.generator.(ResumableReportGenerator); ok {
			result, generateErr = resumable.GenerateResumable(ctx, config, candidates, run.GenerationHash, run.CheckpointJSON, func(progress GenerationProgress) error {
				run.GenerationHash = progress.Fingerprint
				run.CheckpointJSON = progress.Checkpoint
				run.CurrentStep = progress.Stage
				run.InputTokens = progress.InputTokens + carryInputTokens
				run.OutputTokens = progress.OutputTokens + carryOutputTokens
				run.TotalTokens = run.InputTokens + run.OutputTokens
				run.AIUsed = run.TotalTokens > 0
				return s.store.UpdateDailyReportRun(run)
			})
		} else {
			result, generateErr = s.generator.Generate(ctx, config, candidates)
		}
	}
	result.InputTokens += carryInputTokens
	result.OutputTokens += carryOutputTokens
	if ctx.Err() != nil {
		s.interruptRun(run)
		return
	}
	if generateErr != nil {
		run.InputTokens = result.InputTokens
		run.OutputTokens = result.OutputTokens
		run.TotalTokens = result.InputTokens + result.OutputTokens
		run.AIUsed = run.TotalTokens > 0
		if errors.Is(generateErr, ErrNoAIProvider) || (errors.Is(generateErr, ErrAIUsageLimit) && !run.AIUsed) {
			result = localFallback(config, candidates)
			run.GenerationMode = "local"
			partial = true
			run.Error = "Cloud AI was unavailable before generation; created a local report"
			if errors.Is(generateErr, ErrAIUsageLimit) {
				run.FailureCode = "usage_limit_reached"
			} else {
				run.FailureCode = "no_ai_provider"
			}
		} else {
			run.GenerationMode = "ai"
			var generationErr *GenerationError
			if errors.As(generateErr, &generationErr) {
				run.FailureCode = generationErr.Code
			}
			log.Printf("daily report: AI generation failed run=%d stage=%s code=%s", run.ID, run.CurrentStep, run.FailureCode)
			_ = s.store.ReplaceDailyReportSources(run.ID, sourceSnapshots(run.ID, candidates))
			s.failRun(run, generateErr)
			return
		}
	}
	if result.InputTokens+result.OutputTokens > 0 {
		run.AIUsed = true
	}
	run.InputTokens = result.InputTokens
	run.OutputTokens = result.OutputTokens
	run.TotalTokens = result.InputTokens + result.OutputTokens
	if run.GenerationMode == "" {
		if run.AIUsed {
			run.GenerationMode = "ai"
		} else {
			run.GenerationMode = "local"
		}
	}
	run.ContentJSON = mustJSON(result.Content)
	run.Markdown = result.Markdown
	run.Progress = 90
	run.CurrentStep = "saving"
	if partial {
		run.Status = RunStatusPartial
	} else {
		run.Status = RunStatusCompleted
	}
	sources := sourceSnapshots(run.ID, candidates)
	if err := s.store.ReplaceDailyReportSources(run.ID, sources); err != nil {
		s.failRun(run, err)
		return
	}
	// Successful reports keep their audited token totals but no longer need the
	// potentially large resumable insight payload.
	run.CheckpointJSON = ""
	s.completeRun(ctx, config, run)
}

func (s *Service) completeRun(ctx context.Context, config *models.DailyReportConfig, run *models.DailyReportRun) {
	now := s.clock.Now()
	run.CompletedAt = &now
	run.Progress = 100
	if run.CurrentStep != "completed" {
		run.CurrentStep = "completed"
	}
	if err := s.store.UpdateDailyReportRun(run); err != nil {
		log.Printf("daily report: failed to finalize run %d: %v", run.ID, err)
		return
	}
	s.setCurrent(run)
	if run.Kind != RunKindManual {
		if err := s.store.SetDailyReportLastHandledBoundary(run.PeriodEnd); err != nil {
			log.Printf("daily report: failed to save handled boundary: %v", err)
		}
	}
	if (config.InAppNotification || config.SystemNotification) && (run.Status != RunStatusNoContent || config.NotifyOnEmpty) {
		s.mu.RLock()
		notifier := s.notifier
		s.mu.RUnlock()
		if err := notifier.NotifyCompleted(ctx, run); err != nil {
			log.Printf("daily report: notification failed: %v", err)
		}
	}
}

func (s *Service) candidates(config *models.DailyReportConfig, start, end time.Time, kind string) ([]models.DailyReportCandidate, error) {
	feedIDs, err := s.feedIDs(config)
	if err != nil {
		return nil, err
	}
	filter := models.DailyReportCandidateFilter{
		PeriodStart: start, PeriodEnd: end, FeedIDs: feedIDs,
		Manual: kind == RunKindManual, IncludeHidden: config.IncludeHidden,
	}
	return s.store.ListDailyReportCandidates(filter)
}

func (s *Service) feedIDs(config *models.DailyReportConfig) ([]int64, error) {
	if config.FeedScope == "selected" {
		if config.FeedIDs != nil {
			return append([]int64(nil), config.FeedIDs...), nil
		}
		return s.store.GetDailyReportSelectedFeedIDs()
	}
	feeds, err := s.store.GetFeeds()
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(feeds))
	for _, feed := range feeds {
		ids = append(ids, feed.ID)
	}
	return ids, nil
}

func (s *Service) resolvePeriod(config *models.DailyReportConfig, requestedStart, requestedEnd *time.Time) (time.Time, time.Time, error) {
	if requestedStart != nil || requestedEnd != nil {
		if requestedStart == nil || requestedEnd == nil || !requestedStart.Before(*requestedEnd) {
			return time.Time{}, time.Time{}, fmt.Errorf("period_start must be before period_end")
		}
		if requestedEnd.Sub(*requestedStart) > 31*24*time.Hour {
			return time.Time{}, time.Time{}, fmt.Errorf("manual period must not exceed 31 days")
		}
		return requestedStart.In(s.location), requestedEnd.In(s.location), nil
	}
	end, err := ScheduledBoundary(s.clock.Now(), config.ScheduleTime, s.location)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	start, err := PreviousBoundary(end, config.ScheduleTime, s.location)
	return start, end, err
}

func (s *Service) GetStatus(ctx context.Context) (*Status, error) {
	config, err := s.GetConfig()
	if err != nil {
		return nil, err
	}
	unread, err := s.store.CountUnreadDailyReportRuns()
	if err != nil {
		return nil, err
	}
	missed, next, err := s.missedPeriods(config)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	notifier := s.notifier
	status := &Status{
		Enabled: config.Enabled, IsRunning: s.running, CurrentRunID: s.currentRunID,
		Progress: s.progress, CurrentStep: s.currentStep, UnreadCount: unread,
		MissedCount: len(missed), RequiresFeedSelection: config.FeedScope == "selected" && len(config.FeedIDs) == 0,
		NotificationAuthorization: NotificationUnsupported,
	}
	s.mu.RUnlock()
	status.NotificationAuthorization = validNotificationStatus(notifier.AuthorizationStatus(ctx))
	if config.Enabled {
		status.NextScheduledAt = &next
	}
	return status, nil
}

// HandleMissed applies the explicit user choice for periods missed while the
// application was not running. It never silently marks a dismissed prompt.
func (s *Service) HandleMissed(ctx context.Context, action string) (accepted, skipped int, err error) {
	if action != "latest" && action != "all" && action != "skip_all" {
		return 0, 0, fmt.Errorf("action must be latest, all, or skip_all")
	}
	config, err := s.GetConfig()
	if err != nil {
		return 0, 0, err
	}
	periods, _, err := s.missedPeriods(config)
	if err != nil {
		return 0, 0, err
	}
	if len(periods) == 0 {
		return 0, 0, nil
	}
	if action != "skip_all" {
		if err := s.ensureCloudProcessingConsent(config); err != nil {
			return 0, 0, err
		}
	}
	if action == "skip_all" {
		latest := periods[len(periods)-1].End
		if err := s.store.SetDailyReportLastHandledBoundary(latest); err != nil {
			return 0, 0, err
		}
		return 0, len(periods), nil
	}
	if action == "latest" {
		periods = periods[len(periods)-1:]
	}
	if err := s.claimRun(); err != nil {
		return 0, 0, err
	}
	runCtx, stop := s.detachedContext(ctx)
	go func() {
		defer stop()
		defer s.releaseRun()
		for _, period := range periods {
			run := &models.DailyReportRun{
				Kind: RunKindBackfill, Status: RunStatusQueued, PeriodStart: period.Start, PeriodEnd: period.End,
				Progress: 0, CurrentStep: "queued", CreatedAt: s.clock.Now(), ConfigSnapshot: safeConfigSnapshot(config),
			}
			id, createErr := s.store.CreateDailyReportRun(run)
			if createErr != nil {
				log.Printf("daily report: failed to create missed run: %v", createErr)
				continue
			}
			run.ID = id
			s.setCurrent(run)
			s.execute(runCtx, config, run)
			if runCtx.Err() != nil {
				return
			}
		}
	}()
	return len(periods), 0, nil
}

func (s *Service) detachedContext(requestCtx context.Context) (context.Context, context.CancelFunc) {
	base := context.WithoutCancel(requestCtx)
	runCtx, cancel := context.WithCancel(base)
	s.mu.RLock()
	lifecycle := s.lifecycleCtx
	s.mu.RUnlock()
	done := make(chan struct{})
	go func() {
		select {
		case <-lifecycle.Done():
			cancel()
		case <-done:
		}
	}()
	return runCtx, func() {
		close(done)
		cancel()
	}
}

// WaitForRuns waits for detached report jobs to finish before the database is
// closed. Stop cancels the lifecycle context first, allowing active network
// requests to persist an interrupted state before shutdown continues.
func (s *Service) WaitForRuns(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		s.runWG.Wait()
		close(done)
	}()
	if timeout <= 0 {
		<-done
		return true
	}
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

type period struct {
	Start time.Time
	End   time.Time
}

func (s *Service) missedPeriods(config *models.DailyReportConfig) ([]period, time.Time, error) {
	latest, err := ScheduledBoundary(s.clock.Now(), config.ScheduleTime, s.location)
	if err != nil {
		return nil, time.Time{}, err
	}
	next, err := NextBoundary(latest, config.ScheduleTime, s.location)
	if err != nil {
		return nil, time.Time{}, err
	}
	if !config.Enabled || config.LastHandledBoundary == nil {
		return nil, next, nil
	}
	cursor := config.LastHandledBoundary.In(s.location)
	periods := make([]period, 0)
	for {
		end, boundaryErr := NextBoundary(cursor, config.ScheduleTime, s.location)
		if boundaryErr != nil {
			return nil, time.Time{}, boundaryErr
		}
		if end.After(latest) {
			break
		}
		hasRun, runErr := s.store.HasDailyReportRun(cursor, end, RunKindAuto)
		if runErr != nil {
			return nil, time.Time{}, runErr
		}
		if !hasRun {
			hasRun, runErr = s.store.HasDailyReportRun(cursor, end, RunKindBackfill)
			if runErr != nil {
				return nil, time.Time{}, runErr
			}
		}
		if !hasRun {
			periods = append(periods, period{Start: cursor, End: end})
		}
		cursor = end
		if len(periods) > 3660 {
			return nil, time.Time{}, fmt.Errorf("too many missed report periods")
		}
	}
	return periods, next, nil
}

func (s *Service) ListHistory(status string, page, pageSize int) ([]models.DailyReportRun, int, error) {
	if status != "" && !validRunStatus(status) {
		return nil, 0, fmt.Errorf("invalid status")
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return s.store.ListDailyReportRuns(models.DailyReportRunFilter{Status: status, Limit: pageSize, Offset: (page - 1) * pageSize})
}

func (s *Service) GetHistory(id int64) (*models.DailyReportRun, []models.DailyReportSource, error) {
	run, err := s.store.GetDailyReportRun(id)
	if err != nil || run == nil {
		if err == nil {
			err = ErrRunNotFound
		}
		return nil, nil, err
	}
	sources, err := s.store.GetDailyReportSources(id)
	return run, sources, err
}

// InspectRetry determines the safe recovery action for a failed AI run using
// current local inputs only. It never creates a run or calls an AI endpoint.
func (s *Service) InspectRetry(ctx context.Context, run *models.DailyReportRun) (RetryState, error) {
	if run == nil || run.GenerationMode != "ai" ||
		(run.Status != RunStatusFailed && run.Status != RunStatusInterrupted) {
		return RetryState{Action: RetryActionNone, Reason: RetryReasonNotRecoverable}, nil
	}
	config, err := s.GetConfig()
	if err != nil {
		return RetryState{}, err
	}
	candidates, err := s.candidates(config, run.PeriodStart, run.PeriodEnd, RunKindManual)
	if err != nil {
		return RetryState{}, err
	}
	return s.inspectRetry(ctx, run, config, candidates)
}

func (s *Service) inspectRetry(ctx context.Context, run *models.DailyReportRun, config *models.DailyReportConfig, candidates []models.DailyReportCandidate) (RetryState, error) {
	inspector, ok := s.generator.(CheckpointInspector)
	if !ok {
		return RetryState{Action: RetryActionRestart, Reason: RetryReasonCheckpointMissing}, nil
	}
	return inspector.InspectCheckpoint(ctx, config, candidates, run.GenerationHash, run.CheckpointJSON)
}

func (s *Service) MarkRead(id int64, read bool) (*models.DailyReportRun, error) {
	if err := s.store.SetDailyReportRunRead(id, read); err != nil {
		return nil, err
	}
	return s.store.GetDailyReportRun(id)
}

func (s *Service) Delete(id int64) error { return s.store.DeleteDailyReportRun(id) }

func (s *Service) AuthorizeNotifications(ctx context.Context) (string, error) {
	s.mu.RLock()
	notifier := s.notifier
	s.mu.RUnlock()
	status, err := notifier.Authorize(ctx)
	return validNotificationStatus(status), err
}

func (s *Service) claimRun() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return ErrServiceStopping
	}
	if s.running {
		return ErrAlreadyRunning
	}
	s.runWG.Add(1)
	s.running = true
	s.progress = 0
	s.currentStep = "queued"
	return nil
}

func (s *Service) releaseRun() {
	s.mu.Lock()
	s.running = false
	s.currentRunID = nil
	s.currentStep = ""
	s.progress = 0
	wake := s.wake
	s.mu.Unlock()
	s.runWG.Done()
	if wake != nil {
		wake()
	}
}

func (s *Service) setCurrent(run *models.DailyReportRun) {
	s.mu.Lock()
	id := run.ID
	s.currentRunID = &id
	s.progress = run.Progress
	s.currentStep = run.CurrentStep
	s.mu.Unlock()
}

func (s *Service) updateRun(run *models.DailyReportRun) {
	s.setCurrent(run)
	if err := s.store.UpdateDailyReportRun(run); err != nil {
		log.Printf("daily report: failed to update run %d: %v", run.ID, err)
	}
}

func (s *Service) failRun(run *models.DailyReportRun, err error) {
	now := s.clock.Now()
	run.Status = RunStatusFailed
	run.Progress = 100
	run.CurrentStep = "failed"
	run.Error = err.Error()
	run.CompletedAt = &now
	s.updateRun(run)
}

func (s *Service) interruptRun(run *models.DailyReportRun) {
	now := s.clock.Now()
	run.Status = RunStatusInterrupted
	run.CurrentStep = "interrupted"
	run.Error = "report generation was interrupted"
	run.CompletedAt = &now
	s.updateRun(run)
}

func sourceSnapshots(runID int64, candidates []models.DailyReportCandidate) []models.DailyReportSource {
	sources := make([]models.DailyReportSource, 0, len(candidates))
	for index, candidate := range candidates {
		articleID, feedID := candidate.ArticleID, candidate.FeedID
		var publishedAt, firstSeenAt *time.Time
		if candidate.HasValidPublishedTime && !candidate.PublishedAt.IsZero() {
			value := candidate.PublishedAt
			publishedAt = &value
		}
		if !candidate.FirstSeenAt.IsZero() {
			value := candidate.FirstSeenAt
			firstSeenAt = &value
		}
		_, kind := candidateContent(candidate)
		sources = append(sources, models.DailyReportSource{
			RunID: runID, SourceIndex: index + 1, ArticleID: &articleID, FeedID: &feedID,
			ArticleTitle: candidate.Title, FeedTitle: candidate.FeedTitle, Author: candidate.Author, URL: candidate.URL,
			ArticleUniqueID: candidate.UniqueID,
			PublishedAt:     publishedAt, FirstSeenAt: firstSeenAt, LateArrival: candidate.LateArrival, ContentKind: kind,
		})
	}
	return sources
}

func candidateContent(candidate models.DailyReportCandidate) (string, string) {
	if candidate.Content != "" {
		return candidate.Content, "content"
	}
	if candidate.Summary != "" {
		return candidate.Summary, "summary"
	}
	return candidate.Title, "title"
}

func safeConfigSnapshot(config *models.DailyReportConfig) string {
	copy := *config
	copy.FeedIDs = append([]int64(nil), config.FeedIDs...)
	encoded, _ := json.Marshal(copy)
	return string(encoded)
}

func mustJSON(value interface{}) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func validRunStatus(status string) bool {
	switch status {
	case RunStatusQueued, RunStatusRefreshing, RunStatusGenerating, RunStatusCompleted, RunStatusPartial, RunStatusNoContent, RunStatusFailed, RunStatusInterrupted:
		return true
	default:
		return false
	}
}
