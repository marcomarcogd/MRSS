package dailyreport

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	report "MRSS/internal/dailyreport"
	"MRSS/internal/handlers/core"
	"MRSS/internal/handlers/response"
	"MRSS/internal/models"
)

type configDTO struct {
	Enabled             bool                    `json:"enabled"`
	ScheduleTime        string                  `json:"schedule_time"`
	FeedScope           string                  `json:"feed_scope"`
	FeedIDs             []int64                 `json:"feed_ids"`
	IncludeHidden       bool                    `json:"include_hidden"`
	AIProfileID         *int64                  `json:"ai_profile_id"`
	ArticleSummaryMode  string                  `json:"article_summary_mode"`
	Focus               string                  `json:"focus"`
	Outline             []report.OutlineSection `json:"outline"`
	Language            string                  `json:"language"`
	TitleTemplate       string                  `json:"title_template"`
	InAppNotification   bool                    `json:"in_app_notification"`
	SystemNotification  bool                    `json:"system_notification"`
	NotifyOnEmpty       bool                    `json:"notify_on_empty"`
	LastHandledBoundary *time.Time              `json:"last_handled_boundary,omitempty"`
	CreatedAt           *time.Time              `json:"created_at,omitempty"`
	UpdatedAt           *time.Time              `json:"updated_at,omitempty"`
}

type runDTO struct {
	ID             int64                `json:"id"`
	Kind           string               `json:"kind"`
	Status         string               `json:"status"`
	PeriodStart    time.Time            `json:"period_start"`
	PeriodEnd      time.Time            `json:"period_end"`
	Progress       int                  `json:"progress"`
	CurrentStep    string               `json:"current_step"`
	Title          string               `json:"title"`
	Content        report.ReportContent `json:"content"`
	Markdown       string               `json:"markdown"`
	InputTokens    int64                `json:"input_tokens"`
	OutputTokens   int64                `json:"output_tokens"`
	TotalTokens    int64                `json:"total_tokens"`
	ArticleCount   int                  `json:"article_count"`
	AIUsed         bool                 `json:"ai_used"`
	IsRead         bool                 `json:"is_read"`
	Error          string               `json:"error,omitempty"`
	FailureCode    string               `json:"failure_code,omitempty"`
	GenerationMode string               `json:"generation_mode,omitempty"`
	RetryOfID      *int64               `json:"retry_of_id,omitempty"`
	CreatedAt      time.Time            `json:"created_at"`
	StartedAt      *time.Time           `json:"started_at,omitempty"`
	CompletedAt    *time.Time           `json:"completed_at,omitempty"`
}

type sourceDTO struct {
	ID          int64      `json:"id"`
	SourceIndex int        `json:"source_index"`
	ArticleID   *int64     `json:"article_id"`
	FeedID      *int64     `json:"feed_id"`
	Title       string     `json:"title"`
	FeedTitle   string     `json:"feed_title"`
	Author      string     `json:"author"`
	URL         string     `json:"url"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	FirstSeenAt *time.Time `json:"first_seen_at,omitempty"`
	LateArrival bool       `json:"late_arrival"`
	ContentKind string     `json:"content_kind"`
}

// HandleConfig reads or updates the daily report configuration.
// @Summary Get or update daily report configuration
// @Tags daily-report
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} response.APIResponse
// @Failure 500 {object} response.APIResponse
// @Router /daily-report/config [get]
// @Router /daily-report/config [put]
func HandleConfig(h *core.Handler, w http.ResponseWriter, r *http.Request) {
	service, ok := requireService(h, w)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		config, err := service.GetConfig()
		if err != nil {
			writeError(w, err)
			return
		}
		cloudProcessing, err := service.GetCloudProcessing(config)
		if err != nil {
			writeError(w, err)
			return
		}
		response.JSON(w, map[string]interface{}{"config": toConfigDTO(config), "cloud_processing": cloudProcessing})
	case http.MethodPut:
		var input configDTO
		if err := decodeJSON(r, &input); err != nil {
			response.Error(w, err, http.StatusBadRequest)
			return
		}
		saved, err := service.SaveConfig(fromConfigDTO(input))
		if err != nil {
			writeError(w, err)
			return
		}
		cloudProcessing, err := service.GetCloudProcessing(saved)
		if err != nil {
			writeError(w, err)
			return
		}
		response.JSON(w, map[string]interface{}{"config": toConfigDTO(saved), "cloud_processing": cloudProcessing})
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPut)
	}
}

// HandleConsent returns, grants, or revokes cloud-processing consent.
// @Summary Manage daily report cloud-processing consent
// @Tags daily-report
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} response.APIResponse
// @Failure 500 {object} response.APIResponse
// @Router /daily-report/consent [get]
// @Router /daily-report/consent [post]
func HandleConsent(h *core.Handler, w http.ResponseWriter, r *http.Request) {
	service, ok := requireService(h, w)
	if !ok {
		return
	}
	if r.Method == http.MethodGet {
		config, err := service.GetConfig()
		if err != nil {
			writeError(w, err)
			return
		}
		cloudProcessing, err := service.GetCloudProcessing(config)
		if err != nil {
			writeError(w, err)
			return
		}
		response.JSON(w, map[string]interface{}{"cloud_processing": cloudProcessing})
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
		return
	}
	var input struct {
		Action  string `json:"action"`
		Version *int   `json:"version"`
	}
	if err := decodeJSON(r, &input); err != nil {
		response.Error(w, err, http.StatusBadRequest)
		return
	}
	var cloudProcessing report.CloudProcessingStatus
	var err error
	switch input.Action {
	case "grant":
		if input.Version == nil {
			response.Error(w, fmt.Errorf("version is required"), http.StatusBadRequest)
			return
		}
		cloudProcessing, err = service.GrantCloudProcessingConsent(*input.Version)
	case "revoke":
		if input.Version != nil {
			response.Error(w, fmt.Errorf("version is not allowed when revoking consent"), http.StatusBadRequest)
			return
		}
		cloudProcessing, err = service.RevokeCloudProcessingConsent()
	default:
		response.Error(w, fmt.Errorf("action must be grant or revoke"), http.StatusBadRequest)
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	response.JSON(w, map[string]interface{}{"cloud_processing": cloudProcessing})
}

// HandleStatus returns scheduler, progress, unread, and missed-run state.
// @Summary Get daily report status
// @Tags daily-report
// @Produce json
// @Success 200 {object} report.Status
// @Failure 500 {object} response.APIResponse
// @Router /daily-report/status [get]
func HandleStatus(h *core.Handler, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	service, ok := requireService(h, w)
	if !ok {
		return
	}
	status, err := service.GetStatus(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	response.JSON(w, status)
}

// HandleGenerate previews or explicitly starts a report.
// @Summary Preview or start a daily report
// @Tags daily-report
// @Accept json
// @Produce json
// @Success 200 {object} report.Preview
// @Success 202 {object} map[string]interface{}
// @Failure 400 {object} response.APIResponse
// @Failure 409 {object} response.APIResponse
// @Router /daily-report/generate [post]
func HandleGenerate(h *core.Handler, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	service, ok := requireService(h, w)
	if !ok {
		return
	}
	var input struct {
		Action      string     `json:"action"`
		PeriodStart *time.Time `json:"period_start"`
		PeriodEnd   *time.Time `json:"period_end"`
	}
	if err := decodeJSON(r, &input); err != nil {
		response.Error(w, err, http.StatusBadRequest)
		return
	}
	switch input.Action {
	case "preview":
		preview, err := service.Preview(input.PeriodStart, input.PeriodEnd)
		if err != nil {
			writeError(w, err)
			return
		}
		response.JSON(w, preview)
	case "start":
		run, err := service.StartManual(r.Context(), input.PeriodStart, input.PeriodEnd)
		if err != nil {
			writeError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"run": toRunDTO(run)})
	default:
		response.Error(w, fmt.Errorf("action must be preview or start"), http.StatusBadRequest)
	}
}

// HandleOptimizeOutline creates an unsaved outline draft.
// @Summary Optimize daily report outline
// @Tags daily-report
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} response.APIResponse
// @Router /daily-report/outline/optimize [post]
func HandleOptimizeOutline(h *core.Handler, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	service, ok := requireService(h, w)
	if !ok {
		return
	}
	var input struct {
		Focus       string `json:"focus"`
		Language    string `json:"language"`
		AIProfileID *int64 `json:"ai_profile_id"`
	}
	if err := decodeJSON(r, &input); err != nil {
		response.Error(w, err, http.StatusBadRequest)
		return
	}
	config, err := service.GetConfig()
	if err != nil {
		writeError(w, err)
		return
	}
	if !sameOptionalInt64(config.AIProfileID, input.AIProfileID) {
		response.Error(w, fmt.Errorf("ai_profile_id must be saved before optimizing outline"), http.StatusBadRequest)
		return
	}
	outline, err := service.OptimizeOutline(r.Context(), input.Focus, input.Language, input.AIProfileID)
	if err != nil {
		writeError(w, err)
		return
	}
	response.JSON(w, map[string]interface{}{"outline": outline})
}

// HandleMissedRuns backfills or explicitly skips missed periods.
// @Summary Handle missed daily reports
// @Tags daily-report
// @Accept json
// @Produce json
// @Success 200 {object} map[string]int
// @Failure 400 {object} response.APIResponse
// @Failure 409 {object} response.APIResponse
// @Router /daily-report/missed-runs [post]
func HandleMissedRuns(h *core.Handler, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	service, ok := requireService(h, w)
	if !ok {
		return
	}
	var input struct {
		Action string `json:"action"`
	}
	if err := decodeJSON(r, &input); err != nil {
		response.Error(w, err, http.StatusBadRequest)
		return
	}
	accepted, skipped, err := service.HandleMissed(r.Context(), input.Action)
	if err != nil {
		writeError(w, err)
		return
	}
	response.JSON(w, map[string]int{"accepted": accepted, "skipped": skipped})
}

// HandleHistory lists, reads, marks, retries, or deletes report history.
// @Summary Manage daily report history
// @Description List, inspect, mutate, retry, or delete daily report history. The retry route accepts restart=true to discard an invalid checkpoint.
// @Tags daily-report
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Success 202 {object} map[string]interface{}
// @Success 204
// @Failure 400 {object} response.APIResponse
// @Failure 404 {object} response.APIResponse
// @Router /daily-report/history [get]
// @Router /daily-report/history/{id} [get]
// @Router /daily-report/history/{id} [delete]
// @Router /daily-report/history/{id}/read [put]
// @Router /daily-report/history/{id}/retry [post]
// @Router /daily-report/history/{id}/local-fallback [post]
func HandleHistory(h *core.Handler, w http.ResponseWriter, r *http.Request) {
	service, ok := requireService(h, w)
	if !ok {
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/daily-report/history"), "/")
	if path == "" {
		handleHistoryCollection(service, w, r)
		return
	}
	parts := strings.Split(path, "/")
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		response.Error(w, fmt.Errorf("invalid report id"), http.StatusBadRequest)
		return
	}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			run, sources, err := service.GetHistory(id)
			if err != nil {
				writeError(w, err)
				return
			}
			retryState, err := service.InspectRetry(r.Context(), run)
			if err != nil {
				writeError(w, err)
				return
			}
			response.JSON(w, map[string]interface{}{
				"run": toRunDTO(run), "sources": toSourceDTOs(sources), "retry_state": retryState,
			})
		case http.MethodDelete:
			if err := service.Delete(id); err != nil {
				writeError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			methodNotAllowed(w, http.MethodGet, http.MethodDelete)
		}
		return
	}
	if len(parts) != 2 {
		response.Error(w, fmt.Errorf("not found"), http.StatusNotFound)
		return
	}
	switch parts[1] {
	case "read":
		if r.Method != http.MethodPut {
			methodNotAllowed(w, http.MethodPut)
			return
		}
		var input struct {
			Read *bool `json:"read"`
		}
		if err := decodeJSON(r, &input); err != nil || input.Read == nil {
			if err == nil {
				err = fmt.Errorf("read is required")
			}
			response.Error(w, err, http.StatusBadRequest)
			return
		}
		run, err := service.MarkRead(id, *input.Read)
		if err != nil {
			writeError(w, err)
			return
		}
		response.JSON(w, map[string]interface{}{"run": toRunDTO(run)})
	case "retry":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		restart := r.URL.Query().Get("restart") == "true"
		run, err := service.Retry(r.Context(), id, restart)
		if err != nil {
			writeError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"run": toRunDTO(run)})
	case "local-fallback":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		run, err := service.UseLocalFallback(r.Context(), id)
		if err != nil {
			writeError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"run": toRunDTO(run)})
	default:
		response.Error(w, fmt.Errorf("not found"), http.StatusNotFound)
	}
}

func handleHistoryCollection(service *report.Service, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	page, err := positiveQueryInt(r, "page", 1)
	if err != nil {
		response.Error(w, err, http.StatusBadRequest)
		return
	}
	pageSize, err := positiveQueryInt(r, "page_size", 20)
	if err != nil || pageSize > 100 {
		if err == nil {
			err = fmt.Errorf("page_size must not exceed 100")
		}
		response.Error(w, err, http.StatusBadRequest)
		return
	}
	items, total, err := service.ListHistory(r.URL.Query().Get("status"), page, pageSize)
	if err != nil {
		writeError(w, err)
		return
	}
	result := make([]runDTO, 0, len(items))
	for i := range items {
		result = append(result, toRunDTO(&items[i]))
	}
	response.JSON(w, map[string]interface{}{"items": result, "total": total, "page": page, "page_size": pageSize})
}

// HandleAuthorizeNotifications requests platform notification permission.
// @Summary Authorize daily report notifications
// @Tags daily-report
// @Produce json
// @Success 200 {object} map[string]string
// @Failure 500 {object} response.APIResponse
// @Router /daily-report/notifications/authorize [post]
func HandleAuthorizeNotifications(h *core.Handler, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	service, ok := requireService(h, w)
	if !ok {
		return
	}
	status, err := service.AuthorizeNotifications(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	response.JSON(w, map[string]string{"status": status})
}

func requireService(h *core.Handler, w http.ResponseWriter) (*report.Service, bool) {
	if h == nil || h.DailyReportService == nil {
		response.Error(w, fmt.Errorf("daily report service is unavailable"), http.StatusServiceUnavailable)
		return nil, false
	}
	return h.DailyReportService, true
}

func decodeJSON(r *http.Request, target interface{}) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("request must contain one JSON object")
		}
		return err
	}
	return nil
}

func toConfigDTO(config *models.DailyReportConfig) configDTO {
	outline := []report.OutlineSection{}
	_ = json.Unmarshal([]byte(config.OutlineJSON), &outline)
	result := configDTO{
		Enabled: config.Enabled, ScheduleTime: config.ScheduleTime, FeedScope: config.FeedScope,
		FeedIDs: config.FeedIDs, IncludeHidden: config.IncludeHidden, AIProfileID: config.AIProfileID,
		ArticleSummaryMode: config.ArticleSummaryMode,
		Focus:              config.Focus, Outline: outline, Language: config.Language, TitleTemplate: config.TitleTemplate,
		InAppNotification: config.InAppNotification, SystemNotification: config.SystemNotification,
		NotifyOnEmpty: config.NotifyOnEmpty, LastHandledBoundary: config.LastHandledBoundary,
	}
	if !config.CreatedAt.IsZero() {
		result.CreatedAt = &config.CreatedAt
	}
	if !config.UpdatedAt.IsZero() {
		result.UpdatedAt = &config.UpdatedAt
	}
	return result
}

func fromConfigDTO(input configDTO) *models.DailyReportConfig {
	return &models.DailyReportConfig{
		Enabled: input.Enabled, ScheduleTime: input.ScheduleTime, FeedScope: input.FeedScope,
		FeedIDs: input.FeedIDs, IncludeHidden: input.IncludeHidden, AIProfileID: input.AIProfileID,
		ArticleSummaryMode: input.ArticleSummaryMode,
		Focus:              input.Focus, OutlineJSON: mustMarshal(input.Outline), Language: input.Language,
		TitleTemplate: input.TitleTemplate, InAppNotification: input.InAppNotification,
		SystemNotification: input.SystemNotification, NotifyOnEmpty: input.NotifyOnEmpty,
	}
}

func toRunDTO(run *models.DailyReportRun) runDTO {
	var content report.ReportContent
	if run != nil && run.ContentJSON != "" {
		_ = json.Unmarshal([]byte(run.ContentJSON), &content)
	}
	if run == nil {
		return runDTO{}
	}
	return runDTO{
		ID: run.ID, Kind: run.Kind, Status: run.Status, PeriodStart: run.PeriodStart, PeriodEnd: run.PeriodEnd,
		Progress: run.Progress, CurrentStep: run.CurrentStep, Title: run.Title, Content: content, Markdown: run.Markdown,
		InputTokens: run.InputTokens, OutputTokens: run.OutputTokens, TotalTokens: run.TotalTokens,
		ArticleCount: run.ArticleCount, AIUsed: run.AIUsed, IsRead: run.IsRead, Error: run.Error,
		FailureCode: run.FailureCode, GenerationMode: run.GenerationMode,
		RetryOfID: run.RetryOfID, CreatedAt: run.CreatedAt, StartedAt: run.StartedAt, CompletedAt: run.CompletedAt,
	}
}

func toSourceDTOs(sources []models.DailyReportSource) []sourceDTO {
	result := make([]sourceDTO, 0, len(sources))
	for _, source := range sources {
		result = append(result, sourceDTO{
			ID: source.ID, SourceIndex: source.SourceIndex, ArticleID: source.ArticleID, FeedID: source.FeedID,
			Title: source.ArticleTitle, FeedTitle: source.FeedTitle, Author: source.Author, URL: source.URL,
			PublishedAt: source.PublishedAt, FirstSeenAt: source.FirstSeenAt, LateArrival: source.LateArrival,
			ContentKind: source.ContentKind,
		})
	}
	return result
}

func positiveQueryInt(r *http.Request, key string, fallback int) (int, error) {
	value := r.URL.Query().Get(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return parsed, nil
}

func mustMarshal(value interface{}) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func methodNotAllowed(w http.ResponseWriter, methods ...string) {
	w.Header().Set("Allow", strings.Join(methods, ", "))
	response.Error(w, fmt.Errorf("method not allowed"), http.StatusMethodNotAllowed)
}

func writeError(w http.ResponseWriter, err error) {
	var consentErr *report.CloudConsentRequiredError
	if errors.As(err, &consentErr) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error": map[string]interface{}{
				"code":    "cloud_processing_consent_required",
				"message": consentErr.Error(),
				"details": map[string]interface{}{"cloud_processing": consentErr.CloudProcessing},
			},
		})
		return
	}
	var generationErr *report.GenerationError
	if errors.As(err, &generationErr) {
		status := http.StatusBadGateway
		if generationErr.Code == "usage_limit_reached" || generationErr.Code == "consent_required" || generationErr.Code == "checkpoint_invalidated" {
			status = http.StatusConflict
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error": map[string]interface{}{
				"code": generationErr.Code, "message": generationErr.Error(),
				"details": map[string]string{"stage": generationErr.Stage},
			},
		})
		return
	}
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, report.ErrAlreadyRunning):
		status = http.StatusConflict
	case errors.Is(err, report.ErrServiceStopping):
		status = http.StatusServiceUnavailable
	case errors.Is(err, report.ErrRunNotFound), errors.Is(err, sql.ErrNoRows):
		status = http.StatusNotFound
	default:
		message := strings.ToLower(err.Error())
		if strings.Contains(message, "already exists") {
			status = http.StatusConflict
		} else if strings.Contains(message, "must") || strings.Contains(message, "required") || strings.Contains(message, "invalid") || strings.Contains(message, "not exceed") {
			status = http.StatusBadRequest
		}
	}
	response.Error(w, err, status)
}

func sameOptionalInt64(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
