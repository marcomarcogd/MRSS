package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"MRSS/internal/ai"
	"MRSS/internal/dailyreport"
	"MRSS/internal/database"
	"MRSS/internal/feed"
	"MRSS/internal/models"
	"MRSS/internal/statistics"

	"github.com/mmcdole/gofeed"
)

func TestNewHandler_ConstructsHandler(t *testing.T) {
	db, err := database.NewDB(":memory:")
	if err != nil {
		t.Fatalf("NewDB failed: %v", err)
	}
	if err := db.Init(); err != nil {
		t.Fatalf("db Init failed: %v", err)
	}

	f := feed.NewFetcher(db)
	h := NewHandler(db, f, nil, nil)

	if h.DB == nil {
		t.Fatal("Handler DB is nil")
	}
	if h.Fetcher == nil {
		t.Fatal("Handler Fetcher is nil")
	}
	if h.DiscoveryService == nil {
		t.Fatal("DiscoveryService should be initialized")
	}
	if h.DailyReportService == nil {
		t.Fatal("DailyReportService should be initialized")
	}
	if h.DailyReportScheduler == nil {
		t.Fatal("DailyReportScheduler should be initialized")
	}
	config, err := h.DailyReportService.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}
	if config.ScheduleTime != "08:00" || config.FeedScope != "all" || config.Enabled || config.ArticleSummaryMode != "ai" {
		t.Fatalf("unexpected daily report defaults: %+v", config)
	}
	if config.OutlineJSON == "" || config.OutlineJSON == "[]" {
		t.Fatalf("daily report default outline was not normalized: %q", config.OutlineJSON)
	}
}

func TestDailyReportBoundariesUseLocalCalendarAcrossDST(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("timezone data unavailable: %v", err)
	}

	springNow := time.Date(2025, time.March, 9, 10, 0, 0, 0, location)
	springEnd, err := dailyreport.ScheduledBoundary(springNow, "08:00", location)
	if err != nil {
		t.Fatalf("ScheduledBoundary failed: %v", err)
	}
	springStart, err := dailyreport.PreviousBoundary(springEnd, "08:00", location)
	if err != nil {
		t.Fatalf("PreviousBoundary failed: %v", err)
	}
	if got := springEnd.Sub(springStart); got != 23*time.Hour {
		t.Fatalf("spring DST period = %s, want 23h", got)
	}

	fallNow := time.Date(2025, time.November, 2, 10, 0, 0, 0, location)
	fallEnd, err := dailyreport.ScheduledBoundary(fallNow, "08:00", location)
	if err != nil {
		t.Fatalf("ScheduledBoundary failed: %v", err)
	}
	fallStart, err := dailyreport.PreviousBoundary(fallEnd, "08:00", location)
	if err != nil {
		t.Fatalf("PreviousBoundary failed: %v", err)
	}
	if got := fallEnd.Sub(fallStart); got != 25*time.Hour {
		t.Fatalf("fall DST period = %s, want 25h", got)
	}
}

func TestDailyReportDefaultsFollowApplicationLanguage(t *testing.T) {
	db := newDailyReportTestDB(t)
	defer db.Close()
	if err := db.SetSetting("language", "en"); err != nil {
		t.Fatalf("SetSetting(language) failed: %v", err)
	}
	config, err := db.GetDailyReportConfig()
	if err != nil {
		t.Fatalf("GetDailyReportConfig failed: %v", err)
	}
	config.Language = "auto"
	config.TitleTemplate = ""
	config.OutlineJSON = "[]"
	if err := db.SaveDailyReportConfig(config, nil); err != nil {
		t.Fatalf("SaveDailyReportConfig failed: %v", err)
	}
	service := dailyreport.NewService(db, nil, dailyreport.LocalGenerator{}, dailyreport.NoopNotifier{}, dailyreport.RealClock(), time.UTC)
	localized, err := service.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}
	if localized.TitleTemplate != "24-Hour AI Digest · {{date}}" || !strings.Contains(localized.OutlineJSON, "Top highlights") {
		t.Fatalf("English defaults were not localized: %+v", localized)
	}
}

func TestDailyReportCloudConsentTracksCurrentDestination(t *testing.T) {
	db, err := database.NewDB(":memory:")
	if err != nil {
		t.Fatalf("NewDB failed: %v", err)
	}
	defer db.Close()
	if err := db.Init(); err != nil {
		t.Fatalf("db Init failed: %v", err)
	}

	profile := &models.AIProfile{
		Name: "Private profile", APIKey: "must-not-leak",
		Endpoint: "https://ai.example.com/v1/chat?token=must-not-leak#fragment",
		Model:    "example-model", IsDefault: true,
	}
	profileID, err := db.CreateAIProfile(profile)
	if err != nil {
		t.Fatalf("CreateAIProfile failed: %v", err)
	}
	profile.ID = profileID

	h := NewHandler(db, feed.NewFetcher(db), nil, nil)
	config, err := h.DailyReportService.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}
	config.AIProfileID = &profileID
	config.Enabled = false
	if _, err := h.DailyReportService.SaveConfig(config); err == nil {
		t.Fatal("changing the AI profile should require consent even while scheduling is disabled")
	} else {
		var consentErr *dailyreport.CloudConsentRequiredError
		if !errors.As(err, &consentErr) {
			t.Fatalf("SaveConfig error = %v, want consent required", err)
		}
	}

	cloud, err := h.DailyReportService.GetCloudProcessing(config)
	if err != nil {
		t.Fatalf("GetCloudProcessing failed: %v", err)
	}
	if !cloud.Required || cloud.Accepted || cloud.Destination == nil {
		t.Fatalf("Unexpected consent requirement: %+v", cloud)
	}
	if cloud.Destination.Endpoint != "https://ai.example.com/v1/chat" {
		t.Fatalf("Endpoint was not redacted: %q", cloud.Destination.Endpoint)
	}
	encoded, _ := json.Marshal(cloud)
	if strings.Contains(string(encoded), "must-not-leak") || strings.Contains(string(encoded), "token=") {
		t.Fatalf("Consent response leaked sensitive provider data: %s", encoded)
	}

	cloud, err = h.DailyReportService.GrantCloudProcessingConsent(1)
	if err != nil {
		t.Fatalf("GrantCloudProcessingConsent failed: %v", err)
	}
	if !cloud.Accepted || cloud.AcceptedVersion == nil || *cloud.AcceptedVersion != 1 || cloud.AcceptedAt == nil {
		t.Fatalf("Consent was not accepted: %+v", cloud)
	}

	config, _ = h.DailyReportService.GetConfig()
	config.Enabled = true
	if _, err := h.DailyReportService.SaveConfig(config); err != nil {
		t.Fatalf("Enable after consent failed: %v", err)
	}
	profile.Model = "example-model-v2"
	if err := db.UpdateAIProfile(profile); err != nil {
		t.Fatalf("UpdateAIProfile(model) failed: %v", err)
	}
	config, err = h.DailyReportService.GetConfig()
	if err != nil || !config.Enabled {
		t.Fatalf("Model-only change should keep the schedule enabled: config=%+v err=%v", config, err)
	}
	cloud, err = h.DailyReportService.GetCloudProcessing(config)
	if err != nil || !cloud.Accepted {
		t.Fatalf("Model-only change should retain destination consent: cloud=%+v err=%v", cloud, err)
	}
	profile.Endpoint = "https://second.example.com/v1/chat"
	if err := db.UpdateAIProfile(profile); err != nil {
		t.Fatalf("UpdateAIProfile failed: %v", err)
	}
	config, err = h.DailyReportService.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig after destination change failed: %v", err)
	}
	if config.Enabled {
		t.Fatal("Destination change should pause daily reports")
	}
	cloud, err = h.DailyReportService.GetCloudProcessing(config)
	if err != nil {
		t.Fatalf("GetCloudProcessing after change failed: %v", err)
	}
	if cloud.Accepted || !cloud.Required || cloud.Destination.Endpoint != "https://second.example.com/v1/chat" {
		t.Fatalf("Destination change did not invalidate consent: %+v", cloud)
	}
}

func TestDailyReportCloudConsentPersistsPendingDestinationBeforeGrant(t *testing.T) {
	db, err := database.NewDB(":memory:")
	if err != nil {
		t.Fatalf("NewDB failed: %v", err)
	}
	defer db.Close()
	if err := db.Init(); err != nil {
		t.Fatalf("db Init failed: %v", err)
	}

	firstProfileID, err := db.CreateAIProfile(&models.AIProfile{
		Name: "First destination", APIKey: "first-secret",
		Endpoint: "https://first.example.com/v1/chat", Model: "model-a", IsDefault: true,
	})
	if err != nil {
		t.Fatalf("CreateAIProfile(first) failed: %v", err)
	}
	secondProfileID, err := db.CreateAIProfile(&models.AIProfile{
		Name: "Second destination", APIKey: "second-secret",
		Endpoint: "https://second.example.com/v1/chat", Model: "model-b",
	})
	if err != nil {
		t.Fatalf("CreateAIProfile(second) failed: %v", err)
	}

	h := NewHandler(db, feed.NewFetcher(db), nil, nil)
	config, err := h.DailyReportService.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}
	config.AIProfileID = &firstProfileID
	if _, err := h.DailyReportService.SaveConfig(config); err == nil {
		t.Fatal("selecting the first cloud profile should require consent")
	} else {
		var firstConsentErr *dailyreport.CloudConsentRequiredError
		if !errors.As(err, &firstConsentErr) {
			t.Fatalf("SaveConfig(first) error = %v, want consent required", err)
		}
	}
	if _, err := h.DailyReportService.GrantCloudProcessingConsent(1); err != nil {
		t.Fatalf("GrantCloudProcessingConsent(first) failed: %v", err)
	}

	config, err = h.DailyReportService.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig before destination change failed: %v", err)
	}
	config.AIProfileID = &secondProfileID
	config.Enabled = true
	_, err = h.DailyReportService.SaveConfig(config)
	var consentErr *dailyreport.CloudConsentRequiredError
	if !errors.As(err, &consentErr) {
		t.Fatalf("SaveConfig(new destination) error = %v, want consent required", err)
	}
	if consentErr.CloudProcessing.Destination == nil ||
		consentErr.CloudProcessing.Destination.Endpoint != "https://second.example.com/v1/chat" {
		t.Fatalf("Consent error did not disclose pending destination: %+v", consentErr.CloudProcessing)
	}

	pendingConfig, err := h.DailyReportService.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig(pending) failed: %v", err)
	}
	if pendingConfig.Enabled || pendingConfig.AIProfileID == nil || *pendingConfig.AIProfileID != secondProfileID {
		t.Fatalf("Pending destination was not safely persisted: %+v", pendingConfig)
	}
	cloud, err := h.DailyReportService.GrantCloudProcessingConsent(1)
	if err != nil {
		t.Fatalf("GrantCloudProcessingConsent(second) failed: %v", err)
	}
	if !cloud.Accepted || cloud.Destination == nil || cloud.Destination.ProfileID == nil ||
		*cloud.Destination.ProfileID != secondProfileID {
		t.Fatalf("Consent was not bound to pending destination: %+v", cloud)
	}

	pendingConfig.Enabled = true
	if _, err := h.DailyReportService.SaveConfig(pendingConfig); err != nil {
		t.Fatalf("Enable after pending destination consent failed: %v", err)
	}
	staleConfig, err := h.DailyReportService.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig before revoke failed: %v", err)
	}
	staleProvider, err := dailyreport.NewAIProviderResolver(db).Resolve(staleConfig)
	if err != nil || staleProvider == nil {
		t.Fatalf("Resolve provider before revoke: provider=%+v err=%v", staleProvider, err)
	}
	cloud, err = h.DailyReportService.RevokeCloudProcessingConsent()
	if err != nil {
		t.Fatalf("RevokeCloudProcessingConsent failed: %v", err)
	}
	if cloud.Accepted || !cloud.Required || cloud.AcceptedAt != nil || cloud.AcceptedVersion != nil {
		t.Fatalf("Consent was not fully revoked: %+v", cloud)
	}
	revokedConfig, err := h.DailyReportService.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig after revoke failed: %v", err)
	}
	if revokedConfig.Enabled {
		t.Fatal("Revoking cloud consent must pause daily reports")
	}
	var requiredAfterRevoke *dailyreport.CloudConsentRequiredError
	if err := h.DailyReportService.EnsureCloudProcessingConsent(staleConfig, staleProvider); !errors.As(err, &requiredAfterRevoke) {
		t.Fatalf("stale running config remained authorized after revoke: %v", err)
	}
}

func TestDailyReportAIGeneratorRetryPolicyAndSourceValidation(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		failures   int32
		wantCalls  int32
		wantError  bool
		wantCode   string
	}{
		{name: "429 is retried twice", statusCode: http.StatusTooManyRequests, failures: 2, wantCalls: 4},
		{name: "5xx is retried twice", statusCode: http.StatusServiceUnavailable, failures: 2, wantCalls: 4},
		{name: "payment failure is not retried", statusCode: http.StatusPaymentRequired, failures: 1, wantCalls: 1, wantError: true, wantCode: "payment_required"},
		{name: "missing model is not retried", statusCode: http.StatusNotFound, failures: 1, wantCalls: 1, wantError: true, wantCode: "model_or_endpoint_not_found"},
		{name: "oversized request is not retried", statusCode: http.StatusRequestEntityTooLarge, failures: 1, wantCalls: 1, wantError: true, wantCode: "request_too_large"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				call := calls.Add(1)
				if call <= tt.failures {
					w.WriteHeader(tt.statusCode)
					_, _ = w.Write([]byte(`{"error":{"message":"temporary or client error"}}`))
					return
				}
				var requestBody struct {
					Prompt   string `json:"prompt"`
					Messages []struct {
						Role    string `json:"role"`
						Content string `json:"content"`
					} `json:"messages"`
				}
				_ = json.NewDecoder(r.Body).Decode(&requestBody)
				userPrompt := requestBody.Prompt
				for _, message := range requestBody.Messages {
					if message.Role == "user" {
						userPrompt = message.Content
					}
				}
				response := "[ARTICLE 1]\nverified insight"
				if strings.Contains(userPrompt, "Sources:") {
					response = "verified report [1] [999]"
				}
				if requestBody.Prompt != "" {
					writeOllamaResponse(t, w, response)
				} else {
					writeOpenAIResponse(t, w, response)
				}
			}))
			defer server.Close()

			db := newDailyReportTestDB(t)
			defer db.Close()
			profileID, err := db.CreateAIProfile(&models.AIProfile{
				Name: "Local integration profile", Endpoint: server.URL, Model: "test-model", IsDefault: true,
			})
			if err != nil {
				t.Fatalf("CreateAIProfile failed: %v", err)
			}
			generator := dailyreport.NewAIGenerator(
				db,
				ai.NewUsageTracker(db),
				statistics.NewService(db),
				dailyreport.WithAIGeneratorRetryDelays(0, 0, 0),
			)
			config := dailyReportGeneratorConfig(profileID)
			result, generateErr := generator.Generate(context.Background(), config, []models.DailyReportCandidate{{
				ArticleID: 1, FeedID: 1, Title: "A trusted title", FeedTitle: "Test feed", OriginalSummary: "Cached summary",
			}})
			if tt.wantError {
				if generateErr == nil {
					t.Fatal("Generate succeeded, want a non-format 4xx error")
				}
				var generationErr *dailyreport.GenerationError
				if !errors.As(generateErr, &generationErr) || generationErr.Code != tt.wantCode {
					t.Fatalf("Generate error = %v, want code %q", generateErr, tt.wantCode)
				}
			} else {
				if generateErr != nil {
					t.Fatalf("Generate failed: %v", generateErr)
				}
				if len(result.Content.Sections) != 1 || len(result.Content.Sections[0].SourceIDs) != 1 || result.Content.Sections[0].SourceIDs[0] != 1 {
					t.Fatalf("Untrusted source IDs were not filtered: %+v", result.Content.Sections)
				}
				if result.InputTokens == 0 || result.OutputTokens == 0 {
					t.Fatalf("AI token usage was not recorded: %+v", result)
				}
				stats, statsErr := db.GetTotalStats()
				if statsErr != nil || stats["ai_daily_report"] != 1 {
					t.Fatalf("ai_daily_report statistic = %v, err = %v", stats, statsErr)
				}
			}
			if got := calls.Load(); got != tt.wantCalls {
				t.Fatalf("AI request count = %d, want %d", got, tt.wantCalls)
			}
		})
	}
}

func TestDailyReportAIGeneratorUsesPlainTextActualUsageCachesSummariesAndResumesCheckpoint(t *testing.T) {
	var calls atomic.Int32
	var extractionCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if _, exists := body["response_format"]; exists {
			t.Errorf("article summaries must not require structured output: %#v", body["response_format"])
		}
		joined := ""
		if messages, ok := body["messages"].([]interface{}); ok {
			for _, raw := range messages {
				message, _ := raw.(map[string]interface{})
				joined += fmt.Sprint(message["content"])
			}
		}
		if strings.Contains(joined, "[ARTICLE 1]") {
			extractionCalls.Add(1)
			writeOpenAIResponseWithUsage(t, w, "[ARTICLE 1]\ncheckpointed summary", 101, 17, 3)
			return
		}
		if call == 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"message":"temporary"}}`))
			return
		}
		writeOpenAIResponseWithUsage(t, w, "resumed report [1]", 53, 11, 2)
	}))
	defer server.Close()

	db := newDailyReportTestDB(t)
	defer db.Close()
	openAIEndpoint := strings.Replace(server.URL, "127.0.0.1", "0.0.0.0", 1)
	profileID, err := db.CreateAIProfile(&models.AIProfile{Name: "Checkpoint", Endpoint: openAIEndpoint, Model: "test-model", IsDefault: true})
	if err != nil {
		t.Fatalf("CreateAIProfile failed: %v", err)
	}
	generator := dailyreport.NewAIGenerator(db, ai.NewUsageTracker(db), nil, dailyreport.WithAIGeneratorRetryDelays(0))
	generator.SetConsentVerifier(func(*models.DailyReportConfig, *dailyreport.ResolvedAIProvider) error { return nil })
	config := dailyReportGeneratorConfig(profileID)
	candidates := []models.DailyReportCandidate{{ArticleID: 1, FeedID: 1, Title: "Title", OriginalSummary: "Summary"}}
	if _, err := db.Exec(`INSERT INTO feeds (id, title, url) VALUES (1, 'Feed', 'https://example.com/feed')`); err != nil {
		t.Fatalf("insert feed: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO articles (id, feed_id, title, url, published_at, first_seen_at, unique_id) VALUES (1, 1, 'Title', 'https://example.com/article', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'summary-cache-test')`); err != nil {
		t.Fatalf("insert article: %v", err)
	}
	var fingerprint, checkpoint string
	result, err := generator.GenerateResumable(context.Background(), config, candidates, "", "", func(progress dailyreport.GenerationProgress) error {
		fingerprint, checkpoint = progress.Fingerprint, progress.Checkpoint
		return nil
	})
	if err == nil {
		t.Fatal("first generation unexpectedly succeeded")
	}
	if result.InputTokens != 101 || result.OutputTokens != 17 {
		t.Fatalf("provider usage not preserved on failure: %+v", result)
	}
	if fingerprint == "" || checkpoint == "" {
		t.Fatal("generation checkpoint was not persisted")
	}
	cachedArticle, cacheErr := db.GetArticleByID(1)
	if cacheErr != nil || cachedArticle.Summary != "checkpointed summary" || cachedArticle.SummarySource != "ai_daily_report" || cachedArticle.SummaryContentHash == "" {
		t.Fatalf("AI article summary was not cached for article reuse: article=%+v err=%v", cachedArticle, cacheErr)
	}
	changedCandidates := append([]models.DailyReportCandidate(nil), candidates...)
	changedCandidates[0].Title = "Changed after checkpoint"
	_, mismatchErr := generator.GenerateResumable(
		context.Background(), config, changedCandidates, fingerprint, checkpoint, nil,
	)
	var generationErr *dailyreport.GenerationError
	if !errors.As(mismatchErr, &generationErr) || generationErr.Code != "checkpoint_invalidated" {
		t.Fatalf("changed inputs did not invalidate checkpoint: %v", mismatchErr)
	}
	result, err = generator.GenerateResumable(context.Background(), config, candidates, fingerprint, checkpoint, nil)
	if err != nil {
		t.Fatalf("resumed generation failed: %v", err)
	}
	if extractionCalls.Load() != 1 {
		t.Fatalf("resumed generation repeated extraction %d times", extractionCalls.Load())
	}
	if result.InputTokens != 154 || result.OutputTokens != 28 {
		t.Fatalf("resumed attempt did not use actual provider usage: %+v", result)
	}

	reusableCandidate := candidates[0]
	reusableCandidate.GeneratedSummary = cachedArticle.Summary
	reusableCandidate.SummarySource = cachedArticle.SummarySource
	reusableCandidate.SummaryFingerprint = cachedArticle.SummaryFingerprint
	reusableCandidate.SummaryContentHash = cachedArticle.SummaryContentHash
	if _, err := generator.Generate(context.Background(), config, []models.DailyReportCandidate{reusableCandidate}); err != nil {
		t.Fatalf("generation with reusable AI summary failed: %v", err)
	}
	if extractionCalls.Load() != 1 {
		t.Fatalf("valid cached AI summary triggered another summary request: %d", extractionCalls.Load())
	}

	profile, profileErr := db.GetAIProfile(profileID)
	if profileErr != nil || profile == nil {
		t.Fatalf("reload AI profile: profile=%+v err=%v", profile, profileErr)
	}
	profile.Model = "changed-model"
	if err := db.UpdateAIProfile(profile); err != nil {
		t.Fatalf("change AI profile model: %v", err)
	}
	if _, err := generator.Generate(context.Background(), config, []models.DailyReportCandidate{reusableCandidate}); err != nil {
		t.Fatalf("generation after AI configuration changed failed: %v", err)
	}
	if extractionCalls.Load() != 2 {
		t.Fatalf("changed AI configuration did not invalidate the cached summary: %d", extractionCalls.Load())
	}

	reusableCandidate.OriginalSummary = "Article content changed"
	if _, err := generator.Generate(context.Background(), config, []models.DailyReportCandidate{reusableCandidate}); err != nil {
		t.Fatalf("generation after article content changed failed: %v", err)
	}
	if extractionCalls.Load() != 3 {
		t.Fatalf("changed article content did not invalidate the cached summary: %d", extractionCalls.Load())
	}
}

func TestDailyReportLocalFallbackMatchesOutlineAndCleansHTML(t *testing.T) {
	config := &models.DailyReportConfig{
		Language: "en",
		OutlineJSON: `[
			{"id":"ai","title":"AI","instruction":"artificial intelligence models"},
			{"id":"sport","title":"Sport","instruction":"football league"}
		]`,
	}
	candidates := []models.DailyReportCandidate{
		{ArticleID: 1, Title: "New artificial intelligence model", OriginalSummary: `<p>Model update</p><script>steal()</script>`},
		{ArticleID: 2, Title: "Football league final", OriginalSummary: `<strong>Team wins</strong>`},
		{ArticleID: 3, Title: "Unrelated cooking notes", OriginalSummary: "A recipe"},
	}
	result, err := (dailyreport.LocalGenerator{}).Generate(context.Background(), config, candidates)
	if err != nil {
		t.Fatalf("local generation failed: %v", err)
	}
	if len(result.Content.Sections) != 2 {
		t.Fatalf("local sections = %+v", result.Content.Sections)
	}
	if got := result.Content.Sections[0].SourceIDs; len(got) != 1 || got[0] != 1 {
		t.Fatalf("AI section sources = %v", got)
	}
	if got := result.Content.Sections[1].SourceIDs; len(got) != 1 || got[0] != 2 {
		t.Fatalf("sport section sources = %v", got)
	}
	if strings.Contains(result.Markdown, "<p>") || strings.Contains(result.Markdown, "steal()") || strings.Contains(result.Markdown, "cooking") {
		t.Fatalf("local report was not cleaned or relevance-filtered: %s", result.Markdown)
	}
}

func TestDailyReportPlainTextGenerationDoesNotRequireProviderSchemaSupport(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, ok := body["response_format"]; ok {
			t.Errorf("plain text digest request unexpectedly required response_format: %#v", body["response_format"])
		}
		joined := fmt.Sprint(body["messages"])
		if strings.Contains(joined, "Sources:") {
			writeOpenAIResponseWithUsage(t, w, "done [1]", 20, 3, 1)
			return
		}
		writeOpenAIResponseWithUsage(t, w, "[ARTICLE 1]\ndone", 10, 2, 1)
	}))
	defer server.Close()
	db := newDailyReportTestDB(t)
	defer db.Close()
	endpoint := strings.Replace(server.URL, "127.0.0.1", "0.0.0.0", 1)
	profileID, err := db.CreateAIProfile(&models.AIProfile{Name: "Schema fallback", Endpoint: endpoint, Model: "test-model", IsDefault: true})
	if err != nil {
		t.Fatalf("CreateAIProfile failed: %v", err)
	}
	generator := dailyreport.NewAIGenerator(db, ai.NewUsageTracker(db), nil, dailyreport.WithAIGeneratorRetryDelays(0))
	generator.SetConsentVerifier(func(*models.DailyReportConfig, *dailyreport.ResolvedAIProvider) error { return nil })
	result, err := generator.Generate(context.Background(), dailyReportGeneratorConfig(profileID), []models.DailyReportCandidate{{ArticleID: 1, Title: "Title"}})
	if err != nil {
		t.Fatalf("schema compatibility fallback failed: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("plain text generation calls = %d, want 2", calls.Load())
	}
	if result.InputTokens != 30 || result.OutputTokens != 5 {
		t.Fatalf("unsupported schema response was incorrectly counted: %+v", result)
	}
}

func TestDailyReportBatchParserAcceptsMarkdownArticleMarkers(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		joined := fmt.Sprint(body["messages"])
		if strings.Contains(joined, "Sources:") {
			writeOpenAIResponse(t, w, "### Result\nDone [1]")
			return
		}
		writeOpenAIResponse(t, w, "- [文章 1]\n**摘要：** done")
	}))
	defer server.Close()

	db := newDailyReportTestDB(t)
	defer db.Close()
	endpoint := strings.Replace(server.URL, "127.0.0.1", "0.0.0.0", 1)
	profileID, err := db.CreateAIProfile(&models.AIProfile{
		Name: "Prompt JSON fallback", Endpoint: endpoint, Model: "test-model", IsDefault: true,
	})
	if err != nil {
		t.Fatalf("CreateAIProfile failed: %v", err)
	}
	generator := dailyreport.NewAIGenerator(db, ai.NewUsageTracker(db), nil, dailyreport.WithAIGeneratorRetryDelays(0))
	generator.SetConsentVerifier(func(*models.DailyReportConfig, *dailyreport.ResolvedAIProvider) error { return nil })
	result, err := generator.Generate(context.Background(), dailyReportGeneratorConfig(profileID), []models.DailyReportCandidate{{ArticleID: 1, Title: "Title"}})
	if err != nil {
		t.Fatalf("prompt JSON compatibility fallback failed: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("plain text calls = %d, want 2", calls.Load())
	}
	if len(result.Content.Sections) != 1 || !strings.Contains(result.Content.Sections[0].Summary, "Done") {
		t.Fatalf("unexpected report result: %+v", result.Content)
	}
}

func TestDailyReportDeepSeekUsesPortablePlainTextMode(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if _, exists := body["response_format"]; exists {
			t.Errorf("DeepSeek digest request unexpectedly used response_format: %#v", body["response_format"])
		}
		if thinking, _ := body["thinking"].(map[string]interface{}); thinking["type"] != "disabled" {
			t.Errorf("DeepSeek reasoning was not disabled for digest summaries: %#v", body["thinking"])
		}
		joined := fmt.Sprint(body["messages"])
		if strings.Contains(joined, "Sources:") {
			writeOpenAIResponse(t, w, "done [1]")
			return
		}
		writeOpenAIResponse(t, w, "[ARTICLE 1]\ndone")
	}))
	defer server.Close()

	db := newDailyReportTestDB(t)
	defer db.Close()
	endpoint := strings.Replace(server.URL, "127.0.0.1", "0.0.0.0", 1) + "/deepseek/v1/chat/completions"
	profileID, err := db.CreateAIProfile(&models.AIProfile{
		Name: "DeepSeek native JSON", Endpoint: endpoint, Model: "deepseek-test", IsDefault: true,
	})
	if err != nil {
		t.Fatalf("CreateAIProfile failed: %v", err)
	}
	generator := dailyreport.NewAIGenerator(db, ai.NewUsageTracker(db), nil, dailyreport.WithAIGeneratorRetryDelays(0))
	generator.SetConsentVerifier(func(*models.DailyReportConfig, *dailyreport.ResolvedAIProvider) error { return nil })
	result, err := generator.Generate(context.Background(), dailyReportGeneratorConfig(profileID), []models.DailyReportCandidate{{ArticleID: 1, Title: "Title"}})
	if err != nil {
		t.Fatalf("DeepSeek JSON-mode generation failed: %v", err)
	}
	if calls.Load() != 2 || len(result.Content.Sections) != 1 {
		t.Fatalf("DeepSeek result=%+v calls=%d", result.Content, calls.Load())
	}
}

func TestDailyReportOutlineOptimizationAcceptsCommonResponseVariants(t *testing.T) {
	longTitle := strings.Repeat("题", dailyreport.MaxTitleLength+10)
	longInstruction := strings.Repeat("要求", dailyreport.MaxInstructionLength)
	manyCandidates := make([]map[string]string, 0, dailyreport.MaxOutlineSections+2)
	manyTitles := make([]string, 0, dailyreport.MaxOutlineSections)
	manyIDs := make([]string, 0, dailyreport.MaxOutlineSections)
	for index := 1; index <= dailyreport.MaxOutlineSections+2; index++ {
		title := fmt.Sprintf("Section %d", index)
		manyCandidates = append(manyCandidates, map[string]string{"title": title})
		if index <= dailyreport.MaxOutlineSections {
			manyTitles = append(manyTitles, title)
			manyIDs = append(manyIDs, fmt.Sprintf("section-%d", index))
		}
	}
	manyResponse, err := json.Marshal(map[string]interface{}{"outline": manyCandidates})
	if err != nil {
		t.Fatalf("marshal oversized outline: %v", err)
	}
	tests := []struct {
		name         string
		response     string
		wantTitles   []string
		wantIDs      []string
		wantCalls    int32
		unsupported  bool
		checkLengths bool
	}{
		{
			name:       "strict outline envelope",
			response:   `{"outline":[{"id":"news","title":"News","instruction":"Important updates"}]}`,
			wantTitles: []string{"News"}, wantIDs: []string{"news"}, wantCalls: 1,
		},
		{
			name: "sections envelope and aliases without schema support", unsupported: true,
			response: `{"sections":[` +
				`{"name":"Products","description":"Product launches"},` +
				`{"heading":"Research","requirement":"Research results"},` +
				`{"title":"Policy","prompt":"Policy changes"},` +
				`{"title":"Security","content":"Security incidents"}]}`,
			wantTitles: []string{"Products", "Research", "Policy", "Security"},
			wantIDs:    []string{"section-1", "section-2", "section-3", "section-4"}, wantCalls: 2,
		},
		{
			name: "markdown direct array normalizes ids and lengths",
			response: "```json\n[" +
				fmt.Sprintf(`{"id":"dup","name":%q,"description":%q},`, longTitle, longInstruction) +
				`{"id":"dup","heading":"Second","requirement":"Requirement"},` +
				`{"prompt":"missing title is discarded"},` +
				`{"title":"Third","content":"Content"}]` + "\n```",
			wantTitles: []string{strings.Repeat("题", dailyreport.MaxTitleLength), "Second", "Third"},
			wantIDs:    []string{"dup", "section-2", "section-3"}, wantCalls: 1, checkLengths: true,
		},
		{
			name:     "extra sections are capped",
			response: string(manyResponse), wantTitles: manyTitles, wantIDs: manyIDs, wantCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				call := calls.Add(1)
				var body map[string]interface{}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("decode request: %v", err)
					return
				}
				if call == 1 {
					joined := fmt.Sprint(body["messages"])
					if !strings.Contains(joined, `{"outline"`) || !strings.Contains(joined, `"instruction"`) {
						t.Errorf("outline prompt does not describe the required contract: %s", joined)
					}
					if tt.unsupported {
						w.WriteHeader(http.StatusBadRequest)
						_, _ = w.Write([]byte(`{"error":{"message":"response_format json_schema is unsupported"}}`))
						return
					}
				}
				writeOpenAIResponse(t, w, tt.response)
			}))
			defer server.Close()

			db := newDailyReportTestDB(t)
			defer db.Close()
			endpoint := strings.Replace(server.URL, "127.0.0.1", "0.0.0.0", 1)
			profileID, err := db.CreateAIProfile(&models.AIProfile{
				Name: "Outline variants", Endpoint: endpoint, Model: "test-model", IsDefault: true,
			})
			if err != nil {
				t.Fatalf("CreateAIProfile failed: %v", err)
			}
			usage := ai.NewUsageTracker(db)
			usage.SetMinInterval(0)
			generator := dailyreport.NewAIGenerator(db, usage, nil, dailyreport.WithAIGeneratorRetryDelays(0))
			generator.SetConsentVerifier(func(*models.DailyReportConfig, *dailyreport.ResolvedAIProvider) error { return nil })
			outline, err := generator.OptimizeOutline(context.Background(), "AI and developer tools", "en", &profileID)
			if err != nil {
				t.Fatalf("OptimizeOutline failed: %v", err)
			}
			if got := calls.Load(); got != tt.wantCalls {
				t.Fatalf("request count = %d, want %d", got, tt.wantCalls)
			}
			if len(outline) != len(tt.wantTitles) {
				t.Fatalf("outline = %+v, want %d sections", outline, len(tt.wantTitles))
			}
			for index, section := range outline {
				if section.Title != tt.wantTitles[index] || section.ID != tt.wantIDs[index] {
					t.Fatalf("outline[%d] = %+v, want id=%q title=%q", index, section, tt.wantIDs[index], tt.wantTitles[index])
				}
			}
			if tt.checkLengths && (len([]rune(outline[0].Title)) != dailyreport.MaxTitleLength || len([]rune(outline[0].Instruction)) != dailyreport.MaxInstructionLength) {
				t.Fatalf("long fields were not truncated: title=%d instruction=%d", len([]rune(outline[0].Title)), len([]rune(outline[0].Instruction)))
			}
		})
	}
}

func TestDailyReportOutlineOptimizationRepairsMalformedResponseOnce(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if call == 1 {
			writeOpenAIResponseWithUsage(t, w, `{"result":"an unstructured draft"}`, 10, 2, 1)
			return
		}
		joined := fmt.Sprint(body["messages"])
		if !strings.Contains(joined, "untrusted draft") || !strings.Contains(joined, "an unstructured draft") {
			t.Errorf("repair request did not contain the bounded untrusted draft contract: %s", joined)
		}
		writeOpenAIResponseWithUsage(t, w, `{"sections":[{"heading":"Repaired","requirement":"Recovered requirement"}]}`, 12, 3, 1)
	}))
	defer server.Close()

	db := newDailyReportTestDB(t)
	defer db.Close()
	endpoint := strings.Replace(server.URL, "127.0.0.1", "0.0.0.0", 1)
	profileID, err := db.CreateAIProfile(&models.AIProfile{Name: "Outline repair", Endpoint: endpoint, Model: "test-model", IsDefault: true})
	if err != nil {
		t.Fatalf("CreateAIProfile failed: %v", err)
	}
	usage := ai.NewUsageTracker(db)
	usage.SetMinInterval(0)
	generator := dailyreport.NewAIGenerator(db, usage, statistics.NewService(db), dailyreport.WithAIGeneratorRetryDelays(0))
	generator.SetConsentVerifier(func(*models.DailyReportConfig, *dailyreport.ResolvedAIProvider) error { return nil })
	outline, err := generator.OptimizeOutline(context.Background(), "AI", "en", &profileID)
	if err != nil {
		t.Fatalf("OptimizeOutline failed after repair: %v", err)
	}
	if calls.Load() != 2 || len(outline) != 1 || outline[0].Title != "Repaired" {
		t.Fatalf("repair result=%+v calls=%d", outline, calls.Load())
	}
	currentUsage, err := usage.GetCurrentUsage()
	if err != nil || currentUsage != 27 {
		t.Fatalf("usage=%d err=%v, want 27", currentUsage, err)
	}
	stats, err := db.GetTotalStats()
	if err != nil || stats["ai_daily_report"] != 1 {
		t.Fatalf("ai_daily_report statistic=%v err=%v", stats, err)
	}
}

func TestDailyReportOutlineOptimizationStopsAfterOneFailedRepair(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writeOpenAIResponse(t, w, `{"unexpected":"still invalid"}`)
	}))
	defer server.Close()
	db := newDailyReportTestDB(t)
	defer db.Close()
	endpoint := strings.Replace(server.URL, "127.0.0.1", "0.0.0.0", 1)
	profileID, err := db.CreateAIProfile(&models.AIProfile{Name: "Invalid outline", Endpoint: endpoint, Model: "test-model", IsDefault: true})
	if err != nil {
		t.Fatalf("CreateAIProfile failed: %v", err)
	}
	usage := ai.NewUsageTracker(db)
	usage.SetMinInterval(0)
	generator := dailyreport.NewAIGenerator(db, usage, nil, dailyreport.WithAIGeneratorRetryDelays(0))
	generator.SetConsentVerifier(func(*models.DailyReportConfig, *dailyreport.ResolvedAIProvider) error { return nil })
	_, err = generator.OptimizeOutline(context.Background(), "AI", "en", &profileID)
	var generationErr *dailyreport.GenerationError
	if !errors.As(err, &generationErr) || generationErr.Code != "schema_invalid" || generationErr.Stage != "outline_repair" {
		t.Fatalf("OptimizeOutline error=%v, want outline_repair/schema_invalid", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("format repair calls=%d, want exactly 2", calls.Load())
	}
}

func TestDailyReportAIGeneratorRequiresConsentBeforeNetworkAndHonorsLimit(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writeOpenAIResponse(t, w, `{"insights":[{"summary":"unexpected","source_ids":[1]}]}`)
	}))
	defer server.Close()

	db := newDailyReportTestDB(t)
	defer db.Close()
	profileID, err := db.CreateAIProfile(&models.AIProfile{
		Name: "Consent test", Endpoint: server.URL, Model: "test-model", IsDefault: true,
	})
	if err != nil {
		t.Fatalf("CreateAIProfile failed: %v", err)
	}
	config := dailyReportGeneratorConfig(profileID)
	candidate := []models.DailyReportCandidate{{ArticleID: 1, FeedID: 1, Title: "Title", OriginalSummary: "Summary"}}

	generator := dailyreport.NewAIGenerator(db, ai.NewUsageTracker(db), nil, dailyreport.WithAIGeneratorRetryDelays(0))
	generator.SetConsentVerifier(func(*models.DailyReportConfig, *dailyreport.ResolvedAIProvider) error {
		return &dailyreport.CloudConsentRequiredError{}
	})
	if _, err := generator.Generate(context.Background(), config, candidate); err == nil {
		t.Fatal("Generate succeeded without consent")
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("AI endpoint received %d requests without consent", got)
	}
	if _, err := generator.OptimizeOutline(context.Background(), "AI", "en", &profileID); err == nil {
		t.Fatal("OptimizeOutline succeeded without consent")
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("outline endpoint received %d requests without consent", got)
	}

	generator.SetConsentVerifier(nil)
	if err := db.SetSetting("ai_usage_limit", "1"); err != nil {
		t.Fatalf("SetSetting(ai_usage_limit) failed: %v", err)
	}
	if _, err := generator.Generate(context.Background(), config, candidate); !errors.Is(err, dailyreport.ErrAIUsageLimit) {
		t.Fatalf("Generate error = %v, want ErrAIUsageLimit", err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("AI endpoint received %d requests after token limit", got)
	}
	if _, err := generator.OptimizeOutline(context.Background(), "AI", "en", &profileID); !errors.Is(err, dailyreport.ErrAIUsageLimit) {
		t.Fatalf("OptimizeOutline error = %v, want ErrAIUsageLimit", err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("outline endpoint received %d requests after token limit", got)
	}
}

func TestDailyReportAIGeneratorBlocksCrossOriginRedirectsAndBypassesProxyForLocalEndpoints(t *testing.T) {
	t.Run("cross-origin redirect", func(t *testing.T) {
		var targetCalls atomic.Int32
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			targetCalls.Add(1)
			writeOpenAIResponse(t, w, `{"insights":[]}`)
		}))
		defer target.Close()
		redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
		}))
		defer redirect.Close()

		db := newDailyReportTestDB(t)
		defer db.Close()
		profileID, err := db.CreateAIProfile(&models.AIProfile{Name: "Redirect", Endpoint: redirect.URL, Model: "test-model", IsDefault: true})
		if err != nil {
			t.Fatalf("CreateAIProfile failed: %v", err)
		}
		generator := dailyreport.NewAIGenerator(db, ai.NewUsageTracker(db), nil, dailyreport.WithAIGeneratorRetryDelays(0))
		_, err = generator.Generate(context.Background(), dailyReportGeneratorConfig(profileID), []models.DailyReportCandidate{{ArticleID: 1, FeedID: 1, Title: "Title"}})
		if err == nil {
			t.Fatal("cross-origin redirect unexpectedly succeeded")
		}
		if targetCalls.Load() != 0 {
			t.Fatalf("redirect target received %d requests", targetCalls.Load())
		}
	})

	t.Run("loopback target bypasses global proxy", func(t *testing.T) {
		var proxyCalls atomic.Int32
		proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			proxyCalls.Add(1)
			http.Error(w, "proxy should not receive local AI data", http.StatusBadGateway)
		}))
		defer proxy.Close()
		var endpointCalls atomic.Int32
		endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			call := endpointCalls.Add(1)
			content := "[ARTICLE 1]\nlocal summary"
			if call > 1 {
				content = "local report [1]"
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if _, ollama := body["prompt"]; ollama {
				writeOllamaResponse(t, w, content)
			} else {
				writeOpenAIResponse(t, w, content)
			}
		}))
		defer endpoint.Close()

		db := newDailyReportTestDB(t)
		defer db.Close()
		proxyURL, err := url.Parse(proxy.URL)
		if err != nil {
			t.Fatalf("parse proxy URL: %v", err)
		}
		_ = db.SetSetting("proxy_enabled", "true")
		_ = db.SetSetting("proxy_type", proxyURL.Scheme)
		_ = db.SetSetting("proxy_host", proxyURL.Hostname())
		_ = db.SetSetting("proxy_port", proxyURL.Port())
		profileID, err := db.CreateAIProfile(&models.AIProfile{Name: "Local", Endpoint: endpoint.URL, Model: "test-model", IsDefault: true})
		if err != nil {
			t.Fatalf("CreateAIProfile failed: %v", err)
		}
		generator := dailyreport.NewAIGenerator(db, ai.NewUsageTracker(db), nil, dailyreport.WithAIGeneratorRetryDelays(0))
		if _, err := generator.Generate(context.Background(), dailyReportGeneratorConfig(profileID), []models.DailyReportCandidate{{ArticleID: 1, FeedID: 1, Title: "Title"}}); err != nil {
			t.Fatalf("Generate failed: %v", err)
		}
		if proxyCalls.Load() != 0 || endpointCalls.Load() < 2 {
			t.Fatalf("proxy calls=%d endpoint calls=%d", proxyCalls.Load(), endpointCalls.Load())
		}
	})
}

func TestDailyReportAIGeneratorLimitsSelectionBatchesSummariesAndRetriesMissingItems(t *testing.T) {
	var batchCalls atomic.Int32
	var singleCalls atomic.Int32
	var totalCalls atomic.Int32
	var summarizedMu sync.Mutex
	summarizedIDs := make(map[string]struct{})
	articleMarker := regexp.MustCompile(`\[ARTICLE (\d+)\]`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		totalCalls.Add(1)
		body := struct {
			Prompt   string `json:"prompt"`
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		joined := body.Prompt
		for _, message := range body.Messages {
			joined += message.Content
		}
		if strings.Contains(joined, "Sources:") {
			writeOllamaResponse(t, w, "Combined [1]")
			return
		}
		matches := articleMarker.FindAllStringSubmatch(joined, -1)
		if len(matches) == 0 {
			singleCalls.Add(1)
			writeOllamaResponse(t, w, "Recovered single summary")
			return
		}
		call := batchCalls.Add(1)
		blocks := make([]string, 0, len(matches)+1)
		for index, match := range matches {
			// The first multi-item response deliberately omits its final article;
			// the generator must retry only that missing item as a smaller request.
			if call == 1 && index == len(matches)-1 {
				continue
			}
			summarizedMu.Lock()
			summarizedIDs[match[1]] = struct{}{}
			summarizedMu.Unlock()
			switch index % 4 {
			case 0:
				blocks = append(blocks, "### Article "+match[1]+":\nExtracted "+match[1])
			case 1:
				blocks = append(blocks, "- [文章 "+match[1]+"]\nExtracted "+match[1])
			case 2:
				blocks = append(blocks, "["+match[1]+"]\nExtracted "+match[1])
			default:
				blocks = append(blocks, "ID: "+match[1]+"\nExtracted "+match[1])
			}
		}
		if call == 1 && len(matches) > 0 {
			blocks = append(blocks, "[ARTICLE "+matches[0][1]+"]\nUpdated duplicate summary")
		}
		writeOllamaResponse(t, w, strings.Join(blocks, "\n"))
	}))
	defer server.Close()

	db := newDailyReportTestDB(t)
	defer db.Close()
	profileID, err := db.CreateAIProfile(&models.AIProfile{Name: "Batch test", Endpoint: server.URL, Model: "test-model", IsDefault: true})
	if err != nil {
		t.Fatalf("CreateAIProfile failed: %v", err)
	}
	if err := db.SetSetting("ai_usage_limit", "0"); err != nil {
		t.Fatalf("SetSetting(ai_usage_limit) failed: %v", err)
	}
	longContent := strings.Repeat("内容 content payload ", 2500)
	candidates := make([]models.DailyReportCandidate, 0, 48)
	for index := 0; index < 48; index++ {
		candidates = append(candidates, models.DailyReportCandidate{
			ArticleID: int64(index + 1), FeedID: int64(index%6 + 1), FeedTitle: fmt.Sprintf("Feed %d", index%6+1),
			Title: fmt.Sprintf("Article %d", index+1), URL: fmt.Sprintf("https://example.com/%d", index+1),
			Content: longContent,
		})
	}
	config := dailyReportGeneratorConfig(profileID)
	config.OutlineJSON = `[
		{"id":"one","title":"One","instruction":"news"},
		{"id":"two","title":"Two","instruction":"news"},
		{"id":"three","title":"Three","instruction":"news"},
		{"id":"four","title":"Four","instruction":"news"},
		{"id":"five","title":"Five","instruction":"news"}
	]`
	generator := dailyreport.NewAIGenerator(db, ai.NewUsageTracker(db), nil, dailyreport.WithAIGeneratorRetryDelays(0))
	if _, err := generator.Generate(context.Background(), config, candidates); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if batchCalls.Load() != 7 || singleCalls.Load() != 1 {
		t.Fatalf("summary calls: batch=%d single=%d, want 7 batches plus one missing-item retry", batchCalls.Load(), singleCalls.Load())
	}
	if len(summarizedIDs) != 39 {
		t.Fatalf("batch output covered %d unique selected articles, want 39 before one single-item retry", len(summarizedIDs))
	}
	if totalCalls.Load() != 23 {
		t.Fatalf("total AI calls=%d, want 7 summary batches + 1 retry + 15 section parts", totalCalls.Load())
	}
}

func TestDailyReportAIGeneratorShrinksTruncatedSummaryBatchesFromSixToThreeToOne(t *testing.T) {
	var mu sync.Mutex
	batchSizes := make([]int, 0, 3)
	var singleCalls atomic.Int32
	articleMarker := regexp.MustCompile(`\[ARTICLE (\d+)\]`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := struct {
			Prompt   string `json:"prompt"`
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		joined := body.Prompt
		for _, message := range body.Messages {
			joined += message.Content
		}
		if strings.Contains(joined, "Sources:") {
			writeOllamaResponseWithFinish(t, w, "Completed report paragraph [1].", "stop")
			return
		}
		matches := articleMarker.FindAllStringSubmatch(joined, -1)
		if len(matches) > 0 {
			mu.Lock()
			batchSizes = append(batchSizes, len(matches))
			mu.Unlock()
			writeOllamaResponseWithFinish(t, w, "Incomplete batch output", "length")
			return
		}
		singleCalls.Add(1)
		writeOllamaResponseWithFinish(t, w, "Complete single article summary.", "stop")
	}))
	defer server.Close()

	db := newDailyReportTestDB(t)
	defer db.Close()
	profileID, err := db.CreateAIProfile(&models.AIProfile{Name: "Shrink batches", Endpoint: server.URL, Model: "test-model", IsDefault: true})
	if err != nil {
		t.Fatalf("CreateAIProfile failed: %v", err)
	}
	candidates := make([]models.DailyReportCandidate, 0, 6)
	for index := 0; index < 6; index++ {
		candidates = append(candidates, models.DailyReportCandidate{
			ArticleID: int64(index + 1), FeedID: int64(index + 1), FeedTitle: fmt.Sprintf("Feed %d", index+1),
			Title: fmt.Sprintf("Article %d", index+1), Content: "Full article content for summary generation.",
		})
	}
	config := dailyReportGeneratorConfig(profileID)
	config.OutlineJSON = `[{"id":"highlights","title":"Highlights","instruction":"important updates"}]`
	generator := dailyreport.NewAIGenerator(db, ai.NewUsageTracker(db), nil, dailyreport.WithAIGeneratorRetryDelays(0))
	if _, err := generator.Generate(context.Background(), config, candidates); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	mu.Lock()
	gotBatchSizes := append([]int(nil), batchSizes...)
	mu.Unlock()
	if !reflect.DeepEqual(gotBatchSizes, []int{6, 3, 3}) {
		t.Fatalf("summary batch sizes=%v, want [6 3 3]", gotBatchSizes)
	}
	if singleCalls.Load() != 6 {
		t.Fatalf("single summary calls=%d, want 6", singleCalls.Load())
	}
}

func TestDailyReportAIGeneratorReusesCompleteItemsFromTruncatedBatch(t *testing.T) {
	var mu sync.Mutex
	batchSizes := make([]int, 0, 3)
	articleCalls := make(map[string]int)
	articleMarker := regexp.MustCompile(`\[ARTICLE (\d+)\]`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := struct {
			Prompt   string `json:"prompt"`
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		joined := body.Prompt
		for _, message := range body.Messages {
			joined += message.Content
		}
		if strings.Contains(joined, "Sources:") {
			writeOllamaResponseWithFinish(t, w, "Completed report paragraph [1].", "stop")
			return
		}
		matches := articleMarker.FindAllStringSubmatch(joined, -1)
		if len(matches) == 0 {
			t.Fatalf("unexpected AI request without article markers: %q", joined)
		}
		mu.Lock()
		batchSizes = append(batchSizes, len(matches))
		for _, match := range matches {
			articleCalls[match[1]]++
		}
		mu.Unlock()
		if len(matches) == 6 {
			writeOllamaResponseWithFinish(t, w, "[ARTICLE 1]\nComplete first summary.\n[ARTICLE 2]\nPartial", "length")
			return
		}
		blocks := make([]string, 0, len(matches))
		for _, match := range matches {
			blocks = append(blocks, "[ARTICLE "+match[1]+"]\nComplete summary "+match[1]+".")
		}
		writeOllamaResponseWithFinish(t, w, strings.Join(blocks, "\n"), "stop")
	}))
	defer server.Close()

	db := newDailyReportTestDB(t)
	defer db.Close()
	profileID, err := db.CreateAIProfile(&models.AIProfile{Name: "Partial batch", Endpoint: server.URL, Model: "test-model", IsDefault: true})
	if err != nil {
		t.Fatalf("CreateAIProfile failed: %v", err)
	}
	candidates := make([]models.DailyReportCandidate, 0, 6)
	for index := 0; index < 6; index++ {
		candidates = append(candidates, models.DailyReportCandidate{
			ArticleID: int64(index + 1), FeedID: int64(index + 1), FeedTitle: fmt.Sprintf("Feed %d", index+1),
			Title: fmt.Sprintf("Article %d", index+1), Content: "Full article content for summary generation.",
		})
	}
	config := dailyReportGeneratorConfig(profileID)
	config.OutlineJSON = `[{"id":"highlights","title":"Highlights","instruction":"important updates"}]`
	if _, err := dailyreport.NewAIGenerator(db, ai.NewUsageTracker(db), nil, dailyreport.WithAIGeneratorRetryDelays(0)).Generate(
		context.Background(), config, candidates,
	); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	mu.Lock()
	gotBatchSizes := append([]int(nil), batchSizes...)
	firstCalls := articleCalls["1"]
	mu.Unlock()
	if !reflect.DeepEqual(gotBatchSizes, []int{6, 3, 2}) {
		t.Fatalf("summary batch sizes=%v, want [6 3 2]", gotBatchSizes)
	}
	if firstCalls != 1 {
		t.Fatalf("completed article 1 was requested %d times, want once", firstCalls)
	}
}

func TestDailyReportProgramAssemblyCleansMarkupExtractsSectionsAndDeduplicates(t *testing.T) {
	var writingCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		joined := fmt.Sprint(body["prompt"])
		if messages, ok := body["messages"].([]interface{}); ok {
			for _, raw := range messages {
				message, _ := raw.(map[string]interface{})
				joined += fmt.Sprint(message["content"])
			}
		}
		if strings.Contains(joined, "[ARTICLE") {
			writeOllamaResponseWithFinish(t, w, "[ARTICLE 1]\nFirst summary.\n[ARTICLE 2]\nSecond summary.", "stop")
			return
		}
		writingCalls.Add(1)
		writeOllamaResponseWithFinish(t, w, `<h2>Highlights</h2>
*Alpha* [details](https://provider.invalid/internal) finding [1].
- Repeated fact [1]
- Repeated fact [1]
<ol><li>First ordered item [1]</li><li>Second ordered item [1]</li></ol>
<h2>Watch</h2>
Beta finding [2].
<script>alert('unsafe')</script>`, "stop")
	}))
	defer server.Close()

	db := newDailyReportTestDB(t)
	defer db.Close()
	profileID, err := db.CreateAIProfile(&models.AIProfile{Name: "Blocks", Endpoint: server.URL, Model: "test-model", IsDefault: true})
	if err != nil {
		t.Fatalf("CreateAIProfile failed: %v", err)
	}
	config := dailyReportGeneratorConfig(profileID)
	config.OutlineJSON = `[
		{"id":"highlights","title":"Highlights","instruction":"important"},
		{"id":"watch","title":"Watch","instruction":"follow up"}
	]`
	candidates := []models.DailyReportCandidate{
		{ArticleID: 1, FeedID: 1, Title: "Alpha", OriginalSummary: "Alpha summary"},
		{ArticleID: 2, FeedID: 2, Title: "Beta", OriginalSummary: "Beta summary"},
	}
	generator := dailyreport.NewAIGenerator(db, ai.NewUsageTracker(db), nil, dailyreport.WithAIGeneratorRetryDelays(0))
	result, err := generator.Generate(context.Background(), config, candidates)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if writingCalls.Load() != 2 || len(result.Content.Sections) != 2 {
		t.Fatalf("writing calls=%d sections=%+v", writingCalls.Load(), result.Content.Sections)
	}
	first := result.Content.Sections[0]
	second := result.Content.Sections[1]
	if len(first.Blocks) == 0 || len(second.Blocks) == 0 {
		t.Fatalf("structured blocks missing: %+v", result.Content.Sections)
	}
	if strings.Contains(first.Summary, "*") || strings.Contains(first.Summary, "https://") || strings.Contains(first.Summary, "Watch") || strings.Contains(first.Summary, "alert") {
		t.Fatalf("first section was not safely isolated: %q", first.Summary)
	}
	if strings.Count(first.Summary, "Repeated fact") != 1 {
		t.Fatalf("duplicate list item was not removed: %q", first.Summary)
	}
	if !strings.Contains(second.Summary, "Beta finding") || strings.Contains(second.Summary, "Alpha finding") {
		t.Fatalf("second section was not isolated: %q", second.Summary)
	}
	orderedFound := false
	for _, block := range first.Blocks {
		if block.Type == dailyreport.ReportBlockOrderedList && len(block.Items) == 2 {
			orderedFound = true
		}
	}
	if !orderedFound {
		t.Fatalf("HTML ordered list was not preserved as structured content: %+v", first.Blocks)
	}
	if strings.Count(result.Markdown, "## Highlights") != 1 || strings.Contains(result.Markdown, "**") || strings.Contains(result.Markdown, "<h2>") {
		t.Fatalf("Markdown was not rebuilt from safe blocks: %q", result.Markdown)
	}
}

func TestDailyReportProgramAssemblyDeduplicatesOnlyNearMatchesWithSharedSources(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		joined := fmt.Sprint(body["prompt"])
		if messages, ok := body["messages"].([]interface{}); ok {
			for _, raw := range messages {
				message, _ := raw.(map[string]interface{})
				joined += fmt.Sprint(message["content"])
			}
		}
		if strings.Contains(joined, "[ARTICLE") {
			writeOllamaResponseWithFinish(t, w, "[ARTICLE 1]\nFirst summary.\n[ARTICLE 2]\nSecond summary.", "stop")
			return
		}
		writeOllamaResponseWithFinish(t, w, `Major change affects many users today [1].

Major change affects many users today now [1].

Major change affects many users today later [2].`, "stop")
	}))
	defer server.Close()

	db := newDailyReportTestDB(t)
	defer db.Close()
	profileID, err := db.CreateAIProfile(&models.AIProfile{Name: "Near duplicate", Endpoint: server.URL, Model: "test-model", IsDefault: true})
	if err != nil {
		t.Fatalf("CreateAIProfile failed: %v", err)
	}
	config := dailyReportGeneratorConfig(profileID)
	config.OutlineJSON = `[{"id":"highlights","title":"Highlights","instruction":"important"}]`
	candidates := []models.DailyReportCandidate{
		{ArticleID: 1, FeedID: 1, Title: "Alpha", OriginalSummary: "Alpha summary"},
		{ArticleID: 2, FeedID: 2, Title: "Beta", OriginalSummary: "Beta summary"},
	}
	result, err := dailyreport.NewAIGenerator(db, ai.NewUsageTracker(db), nil, dailyreport.WithAIGeneratorRetryDelays(0)).Generate(
		context.Background(), config, candidates,
	)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if len(result.Content.Sections) != 1 {
		t.Fatalf("sections=%+v", result.Content.Sections)
	}
	section := result.Content.Sections[0]
	if strings.Count(section.Summary, "Major change") != 2 {
		t.Fatalf("same-source near duplicate was not removed or different-source text was removed: %q", section.Summary)
	}
	if len(section.Blocks) != 2 || !reflect.DeepEqual(section.Blocks[0].SourceIDs, []int{1}) || !reflect.DeepEqual(section.Blocks[1].SourceIDs, []int{2}) {
		t.Fatalf("deduplicated blocks=%+v", section.Blocks)
	}
}

func TestDailyReportProgramAssemblyContinuesTruncatedSectionsAtMostThreeTimes(t *testing.T) {
	tests := []struct {
		name       string
		finishCall int32
		wantError  bool
	}{
		{name: "first continuation completes", finishCall: 2},
		{name: "second continuation completes", finishCall: 3},
		{name: "third continuation completes", finishCall: 4},
		{name: "three continuations remain truncated", finishCall: 0, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var writingCalls atomic.Int32
			var checkpointSaves atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]interface{}
				_ = json.NewDecoder(r.Body).Decode(&body)
				joined := fmt.Sprint(body["prompt"])
				if messages, ok := body["messages"].([]interface{}); ok {
					for _, raw := range messages {
						message, _ := raw.(map[string]interface{})
						joined += fmt.Sprint(message["content"])
					}
				}
				if strings.Contains(joined, "[ARTICLE") {
					writeOllamaResponseWithFinish(t, w, "[ARTICLE 1]\nCached summary.", "stop")
					return
				}
				call := writingCalls.Add(1)
				if tt.finishCall > 0 && call == tt.finishCall {
					writeOllamaResponseWithFinish(t, w, "unfinished final sentence [1].\n- Final point [1]", "stop")
					return
				}
				writeOllamaResponseWithFinish(t, w, "Complete sentence [1]. unfinished", "length")
			}))
			defer server.Close()

			db := newDailyReportTestDB(t)
			defer db.Close()
			profileID, err := db.CreateAIProfile(&models.AIProfile{Name: "Continuation", Endpoint: server.URL, Model: "test-model", IsDefault: true})
			if err != nil {
				t.Fatalf("CreateAIProfile failed: %v", err)
			}
			generator := dailyreport.NewAIGenerator(db, ai.NewUsageTracker(db), nil, dailyreport.WithAIGeneratorRetryDelays(0))
			result, generateErr := generator.GenerateResumable(
				context.Background(), dailyReportGeneratorConfig(profileID),
				[]models.DailyReportCandidate{{ArticleID: 1, FeedID: 1, Title: "Title", OriginalSummary: "Summary"}},
				"", "", func(progress dailyreport.GenerationProgress) error {
					checkpointSaves.Add(1)
					if strings.Contains(progress.Checkpoint, "API key") {
						t.Fatal("checkpoint contains sensitive data")
					}
					return nil
				},
			)
			if tt.wantError {
				var generationErr *dailyreport.GenerationError
				if !errors.As(generateErr, &generationErr) || generationErr.Code != "output_truncated" {
					t.Fatalf("Generate error = %v", generateErr)
				}
				if writingCalls.Load() != 4 {
					t.Fatalf("writing calls=%d, want initial request plus three continuations", writingCalls.Load())
				}
				if len(result.Content.Sections) != 0 {
					t.Fatalf("truncated content was incorrectly completed: %+v", result.Content)
				}
			} else {
				if generateErr != nil {
					t.Fatalf("Generate failed: %v", generateErr)
				}
				if writingCalls.Load() != tt.finishCall {
					t.Fatalf("writing calls=%d, want %d", writingCalls.Load(), tt.finishCall)
				}
				if len(result.Content.Sections) != 1 || !strings.Contains(result.Content.Sections[0].Summary, "final sentence") {
					t.Fatalf("continued content missing: %+v", result.Content.Sections)
				}
				if strings.Count(result.Content.Sections[0].Summary, "Complete sentence") != 1 {
					t.Fatalf("overlapping continuation was duplicated: %q", result.Content.Sections[0].Summary)
				}
			}
			if checkpointSaves.Load() < writingCalls.Load() {
				t.Fatalf("checkpoint saves=%d writing calls=%d", checkpointSaves.Load(), writingCalls.Load())
			}
		})
	}
}

func TestDailyReportProgramAssemblyInfersTruncationNearTokenBoundaryWithoutFinishReason(t *testing.T) {
	var writingCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		joined := fmt.Sprint(body["prompt"])
		if messages, ok := body["messages"].([]interface{}); ok {
			for _, raw := range messages {
				message, _ := raw.(map[string]interface{})
				joined += fmt.Sprint(message["content"])
			}
		}
		if strings.Contains(joined, "[ARTICLE") {
			writeOllamaResponse(t, w, "[ARTICLE 1]\nCached summary.")
			return
		}
		if writingCalls.Add(1) == 1 {
			writeOllamaResponse(t, w, strings.Repeat("abcdefgh", 2200))
			return
		}
		writeOllamaResponseWithFinish(t, w, "Final sentence [1].", "stop")
	}))
	defer server.Close()

	db := newDailyReportTestDB(t)
	defer db.Close()
	profileID, err := db.CreateAIProfile(&models.AIProfile{Name: "Boundary", Endpoint: server.URL, Model: "test-model", IsDefault: true})
	if err != nil {
		t.Fatalf("CreateAIProfile failed: %v", err)
	}
	result, err := dailyreport.NewAIGenerator(db, ai.NewUsageTracker(db), nil, dailyreport.WithAIGeneratorRetryDelays(0)).Generate(
		context.Background(), dailyReportGeneratorConfig(profileID),
		[]models.DailyReportCandidate{{ArticleID: 1, FeedID: 1, Title: "Title", OriginalSummary: "Summary"}},
	)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if writingCalls.Load() != 2 {
		t.Fatalf("writing calls=%d, want inferred continuation", writingCalls.Load())
	}
	if len(result.Content.Sections) != 1 || !strings.Contains(result.Content.Sections[0].Summary, "Final sentence") {
		t.Fatalf("continued result=%+v", result.Content.Sections)
	}
}

func TestDailyReportSchedulerHandlesSleepStartupAndShutdown(t *testing.T) {
	t.Run("one boundary crossed while running is generated", func(t *testing.T) {
		db := newDailyReportTestDB(t)
		defer db.Close()
		clock := newManualDailyReportClock(time.Date(2026, 8, 19, 7, 59, 0, 0, time.UTC))
		service := dailyreport.NewService(db, nil, dailyreport.LocalGenerator{}, dailyreport.NoopNotifier{}, clock, time.UTC)
		config, _ := service.GetConfig()
		config.Enabled = true
		config.ScheduleTime = "08:00"
		if _, err := service.SaveConfig(config); err != nil {
			t.Fatalf("SaveConfig failed: %v", err)
		}
		scheduler := dailyreport.NewScheduler(service, db, clock)
		scheduler.Start(context.Background(), true)
		clock.waitForTimer(t)
		clock.Advance(time.Minute)
		waitForDailyReportRuns(t, db, 1)
		scheduler.Stop()
		runs, _, err := db.ListDailyReportRuns(models.DailyReportRunFilter{Limit: 10})
		if err != nil || len(runs) != 1 || runs[0].Kind != dailyreport.RunKindAuto {
			t.Fatalf("scheduled runs = %+v, err = %v", runs, err)
		}
	})

	t.Run("startup missed boundary waits for explicit choice", func(t *testing.T) {
		db := newDailyReportTestDB(t)
		defer db.Close()
		now := time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC)
		clock := newManualDailyReportClock(now)
		service := dailyreport.NewService(db, nil, dailyreport.LocalGenerator{}, dailyreport.NoopNotifier{}, clock, time.UTC)
		config, _ := service.GetConfig()
		config.Enabled = true
		config.ScheduleTime = "08:00"
		if _, err := service.SaveConfig(config); err != nil {
			t.Fatalf("SaveConfig failed: %v", err)
		}
		previous := time.Date(2026, 8, 18, 8, 0, 0, 0, time.UTC)
		config, _ = service.GetConfig()
		config.LastHandledBoundary = &previous
		if err := db.SaveDailyReportConfig(config, nil); err != nil {
			t.Fatalf("SaveDailyReportConfig failed: %v", err)
		}
		scheduler := dailyreport.NewScheduler(service, db, clock)
		scheduler.Start(context.Background(), true)
		clock.waitForTimer(t)
		status, err := service.GetStatus(context.Background())
		if err != nil || status.MissedCount != 1 {
			t.Fatalf("startup missed status = %+v, err = %v", status, err)
		}
		runs, total, err := db.ListDailyReportRuns(models.DailyReportRunFilter{Limit: 10})
		if err != nil || total != 0 || len(runs) != 0 {
			t.Fatalf("startup unexpectedly generated runs: %+v total=%d err=%v", runs, total, err)
		}
		scheduler.Stop()
	})

	t.Run("shutdown cancels and records an interrupted run", func(t *testing.T) {
		db := newDailyReportTestDB(t)
		defer db.Close()
		feedID, err := db.AddFeed(&models.Feed{Title: "Test", URL: "https://example.com/feed", Type: "rss"})
		if err != nil {
			t.Fatalf("AddFeed failed: %v", err)
		}
		now := time.Now().UTC()
		if err := db.SaveArticle(&models.Article{
			FeedID: feedID, Title: "Interrupt me", URL: "https://example.com/article",
			PublishedAt: now.Add(-time.Hour), FirstSeenAt: now.Add(-time.Hour), HasValidPublishedTime: true,
		}); err != nil {
			t.Fatalf("SaveArticle failed: %v", err)
		}
		generator := &blockingDailyReportGenerator{started: make(chan struct{})}
		service := dailyreport.NewService(db, nil, generator, dailyreport.NoopNotifier{}, dailyreport.RealClock(), time.UTC)
		scheduler := dailyreport.NewScheduler(service, db, dailyreport.RealClock())
		scheduler.Start(context.Background(), true)
		start, end := now.Add(-2*time.Hour), now
		run, err := service.StartManual(context.Background(), &start, &end)
		if err != nil {
			t.Fatalf("StartManual failed: %v", err)
		}
		select {
		case <-generator.started:
		case <-time.After(2 * time.Second):
			t.Fatal("generator did not start")
		}
		scheduler.Stop()
		stored, err := db.GetDailyReportRun(run.ID)
		if err != nil || stored == nil || stored.Status != dailyreport.RunStatusInterrupted {
			t.Fatalf("interrupted run = %+v, err = %v", stored, err)
		}
	})
}

func TestCreateArticleHTTPClientUsesFeedProxy(t *testing.T) {
	db, err := database.NewDB(":memory:")
	if err != nil {
		t.Fatalf("NewDB failed: %v", err)
	}
	if err := db.Init(); err != nil {
		t.Fatalf("db Init failed: %v", err)
	}

	h := NewHandler(db, feed.NewFetcher(db), nil, nil)
	client, err := h.createArticleHTTPClient(&models.Feed{
		ProxyEnabled: true,
		ProxyURL:     "http://127.0.0.1:3128",
	})
	if err != nil {
		t.Fatalf("createArticleHTTPClient failed: %v", err)
	}

	proxyURL := proxyURLFromClient(t, client)
	if proxyURL != "http://127.0.0.1:3128" {
		t.Fatalf("proxy URL = %q", proxyURL)
	}
}

func TestCreateArticleHTTPClientUsesGlobalProxyWhenFeedRequestsIt(t *testing.T) {
	db, err := database.NewDB(":memory:")
	if err != nil {
		t.Fatalf("NewDB failed: %v", err)
	}
	if err := db.Init(); err != nil {
		t.Fatalf("db Init failed: %v", err)
	}
	_ = db.SetSetting("proxy_enabled", "true")
	_ = db.SetSetting("proxy_type", "http")
	_ = db.SetSetting("proxy_host", "127.0.0.1")
	_ = db.SetSetting("proxy_port", "8080")

	h := NewHandler(db, feed.NewFetcher(db), nil, nil)
	client, err := h.createArticleHTTPClient(&models.Feed{ProxyEnabled: true})
	if err != nil {
		t.Fatalf("createArticleHTTPClient failed: %v", err)
	}

	proxyURL := proxyURLFromClient(t, client)
	if proxyURL != "http://127.0.0.1:8080" {
		t.Fatalf("proxy URL = %q", proxyURL)
	}
}

func proxyURLFromClient(t *testing.T, client *http.Client) string {
	t.Helper()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("unexpected transport type %T", client.Transport)
	}
	if transport.Proxy == nil {
		t.Fatalf("expected proxy to be configured")
	}
	reqURL, _ := url.Parse("https://example.com/article")
	req := &http.Request{URL: reqURL}
	proxy, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("proxy function returned error: %v", err)
	}
	if proxy == nil {
		t.Fatalf("proxy function returned nil")
	}
	return proxy.String()
}

func newDailyReportTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := database.NewDB(filepath.Join(t.TempDir(), "daily-report-test.db"))
	if err != nil {
		t.Fatalf("NewDB failed: %v", err)
	}
	if err := db.Init(); err != nil {
		db.Close()
		t.Fatalf("db Init failed: %v", err)
	}
	return db
}

func dailyReportGeneratorConfig(profileID int64) *models.DailyReportConfig {
	return &models.DailyReportConfig{
		AIProfileID: &profileID,
		Language:    "en",
		OutlineJSON: `[{"id":"highlights","title":"Highlights","instruction":"Summarize important news"}]`,
	}
}

func writeOpenAIResponse(t *testing.T, w http.ResponseWriter, content string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"choices": []map[string]interface{}{{"message": map[string]string{"content": content}}},
	}); err != nil {
		t.Errorf("write AI response failed: %v", err)
	}
}

func writeOpenAIResponseWithUsage(t *testing.T, w http.ResponseWriter, content string, input, output, reasoning int64) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"choices": []map[string]interface{}{{"message": map[string]string{"content": content, "reasoning": "private reasoning"}}},
		"usage": map[string]interface{}{
			"prompt_tokens": input, "completion_tokens": output,
			"completion_tokens_details": map[string]int64{"reasoning_tokens": reasoning},
		},
	}); err != nil {
		t.Errorf("write AI response failed: %v", err)
	}
}

func writeOpenAIResponseWithFinish(t *testing.T, w http.ResponseWriter, content, finishReason string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"choices": []map[string]interface{}{{
			"message": map[string]string{"content": content}, "finish_reason": finishReason,
		}},
	}); err != nil {
		t.Errorf("write AI response failed: %v", err)
	}
}

func writeOllamaResponse(t *testing.T, w http.ResponseWriter, content string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"response": content,
		"done":     true,
	}); err != nil {
		t.Errorf("write Ollama response failed: %v", err)
	}
}

func writeOllamaResponseWithFinish(t *testing.T, w http.ResponseWriter, content, finishReason string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"response": content, "done": true, "done_reason": finishReason,
	}); err != nil {
		t.Errorf("write Ollama response failed: %v", err)
	}
}

type manualDailyReportTimer struct {
	ch      chan time.Time
	stopped atomic.Bool
}

func (t *manualDailyReportTimer) C() <-chan time.Time { return t.ch }
func (t *manualDailyReportTimer) Stop() bool          { return !t.stopped.Swap(true) }

type manualDailyReportClock struct {
	mu      sync.Mutex
	now     time.Time
	timers  []*manualDailyReportTimer
	created chan struct{}
}

func newManualDailyReportClock(now time.Time) *manualDailyReportClock {
	return &manualDailyReportClock{now: now, created: make(chan struct{}, 16)}
}

func (c *manualDailyReportClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *manualDailyReportClock) NewTimer(_ time.Duration) dailyreport.Timer {
	timer := &manualDailyReportTimer{ch: make(chan time.Time, 1)}
	c.mu.Lock()
	c.timers = append(c.timers, timer)
	c.mu.Unlock()
	select {
	case c.created <- struct{}{}:
	default:
	}
	return timer
}

func (c *manualDailyReportClock) Advance(duration time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(duration)
	now := c.now
	timers := append([]*manualDailyReportTimer(nil), c.timers...)
	c.timers = nil
	c.mu.Unlock()
	for _, timer := range timers {
		if timer.stopped.Load() {
			continue
		}
		select {
		case timer.ch <- now:
		default:
		}
	}
}

func (c *manualDailyReportClock) waitForTimer(t *testing.T) {
	t.Helper()
	select {
	case <-c.created:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not create a timer")
	}
}

func waitForDailyReportRuns(t *testing.T, db *database.DB, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, total, err := db.ListDailyReportRuns(models.DailyReportRunFilter{Limit: 100})
		if err == nil && total >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("daily report run count did not reach %d", want)
}

func waitForDailyReportRunStatus(t *testing.T, db *database.DB, id int64, want string) *models.DailyReportRun {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		run, err := db.GetDailyReportRun(id)
		if err == nil && run != nil && run.Status == want {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	run, err := db.GetDailyReportRun(id)
	t.Fatalf("daily report run %d did not reach %s: run=%+v err=%v", id, want, run, err)
	return nil
}

type blockingDailyReportGenerator struct {
	started chan struct{}
	once    sync.Once
}

func (g *blockingDailyReportGenerator) Generate(ctx context.Context, _ *models.DailyReportConfig, _ []models.DailyReportCandidate) (dailyreport.AIResult, error) {
	g.once.Do(func() { close(g.started) })
	<-ctx.Done()
	return dailyreport.AIResult{}, ctx.Err()
}

func (*blockingDailyReportGenerator) OptimizeOutline(context.Context, string, string, *int64) ([]dailyreport.OutlineSection, error) {
	return nil, nil
}

type releasedDailyReportGenerator struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

type failedDailyReportGenerator struct{}

func (failedDailyReportGenerator) Generate(context.Context, *models.DailyReportConfig, []models.DailyReportCandidate) (dailyreport.AIResult, error) {
	return dailyreport.AIResult{InputTokens: 120, OutputTokens: 30}, &dailyreport.GenerationError{
		Code: "timeout", Stage: "finalizing", Cause: context.DeadlineExceeded,
	}
}

func (failedDailyReportGenerator) OptimizeOutline(context.Context, string, string, *int64) ([]dailyreport.OutlineSection, error) {
	return nil, nil
}

type retryInspectionGenerator struct {
	mu             sync.Mutex
	state          dailyreport.RetryState
	resumeCalls    int
	lastHash       string
	lastCheckpoint string
}

func (g *retryInspectionGenerator) Generate(context.Context, *models.DailyReportConfig, []models.DailyReportCandidate) (dailyreport.AIResult, error) {
	return dailyreport.AIResult{}, errors.New("retry test expected resumable generation")
}

func (g *retryInspectionGenerator) GenerateResumable(_ context.Context, _ *models.DailyReportConfig, candidates []models.DailyReportCandidate, hash, checkpoint string, _ dailyreport.CheckpointSaver) (dailyreport.AIResult, error) {
	g.mu.Lock()
	g.resumeCalls++
	g.lastHash = hash
	g.lastCheckpoint = checkpoint
	g.mu.Unlock()
	return dailyreport.AIResult{
		Content: dailyreport.ReportContent{Sections: []dailyreport.ReportSection{{
			ID: "overview", Title: "Overview", Summary: "Recovered report", SourceIDs: []int{1},
		}}},
		Markdown: "# Recovered report",
	}, nil
}

func (*retryInspectionGenerator) OptimizeOutline(context.Context, string, string, *int64) ([]dailyreport.OutlineSection, error) {
	return nil, nil
}

func (g *retryInspectionGenerator) InspectCheckpoint(context.Context, *models.DailyReportConfig, []models.DailyReportCandidate, string, string) (dailyreport.RetryState, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.state, nil
}

func (g *retryInspectionGenerator) setState(state dailyreport.RetryState) {
	g.mu.Lock()
	g.state = state
	g.mu.Unlock()
}

type countingDailyReportRefresher struct{ calls atomic.Int32 }

func (r *countingDailyReportRefresher) Refresh(context.Context, []int64) []dailyreport.RefreshResult {
	r.calls.Add(1)
	return nil
}

func TestDailyReportRetryPreflightDoesNotCreateDuplicateRuns(t *testing.T) {
	db := newDailyReportTestDB(t)
	defer db.Close()
	feedID, err := db.AddFeed(&models.Feed{Title: "Retry Feed", URL: "https://example.com/retry", Type: "rss"})
	if err != nil {
		t.Fatalf("AddFeed failed: %v", err)
	}
	now := time.Now().UTC()
	if err := db.SaveArticle(&models.Article{
		FeedID: feedID, Title: "Retry candidate", URL: "https://example.com/retry/article",
		OriginalSummary: "Summary", PublishedAt: now.Add(-time.Hour), FirstSeenAt: now.Add(-time.Hour), HasValidPublishedTime: true,
	}); err != nil {
		t.Fatalf("SaveArticle failed: %v", err)
	}
	original := &models.DailyReportRun{
		Kind: dailyreport.RunKindManual, Status: dailyreport.RunStatusFailed,
		PeriodStart: now.Add(-2 * time.Hour), PeriodEnd: now,
		GenerationMode: "ai", GenerationHash: "checkpoint-fingerprint",
		CheckpointJSON: `{"version":1,"stage":"merging"}`, ConfigSnapshot: `{}`,
		InputTokens: 80, OutputTokens: 20, TotalTokens: 100, AIUsed: true,
	}
	originalID, err := db.CreateDailyReportRun(original)
	if err != nil {
		t.Fatalf("CreateDailyReportRun failed: %v", err)
	}
	original.ID = originalID

	generator := &retryInspectionGenerator{}
	refresher := &countingDailyReportRefresher{}
	service := dailyreport.NewService(db, refresher, generator, dailyreport.NoopNotifier{}, dailyreport.RealClock(), time.UTC)
	generator.setState(dailyreport.RetryState{
		Action: dailyreport.RetryActionRestart, Reason: dailyreport.RetryReasonInputsChanged,
	})
	state, err := service.InspectRetry(context.Background(), original)
	if err != nil || state.Action != dailyreport.RetryActionRestart || state.Reason != dailyreport.RetryReasonInputsChanged {
		t.Fatalf("InspectRetry = %+v, err=%v", state, err)
	}
	if _, err := service.Retry(context.Background(), originalID); err == nil {
		t.Fatal("resume should be rejected after inputs changed")
	} else {
		var generationErr *dailyreport.GenerationError
		if !errors.As(err, &generationErr) || generationErr.Code != "checkpoint_invalidated" {
			t.Fatalf("Retry error = %v, want checkpoint_invalidated", err)
		}
	}
	if _, total, err := db.ListDailyReportRuns(models.DailyReportRunFilter{Limit: 10}); err != nil || total != 1 {
		t.Fatalf("invalid retry created a run: total=%d err=%v", total, err)
	}

	generator.setState(dailyreport.RetryState{
		Action: dailyreport.RetryActionResume, Reason: dailyreport.RetryReasonCheckpointValid,
	})
	retryRun, err := service.Retry(context.Background(), originalID)
	if err != nil {
		t.Fatalf("Retry from checkpoint failed: %v", err)
	}
	if retryRun.ID != originalID {
		t.Fatalf("retry run id = %d, want original id %d", retryRun.ID, originalID)
	}
	completed := waitForDailyReportRunStatus(t, db, retryRun.ID, dailyreport.RunStatusCompleted)
	if completed.RetryOfID != nil {
		t.Fatalf("in-place retry unexpectedly created a retry relation: %+v", completed.RetryOfID)
	}
	if completed.TotalTokens != 100 {
		t.Fatalf("in-place retry token audit = %d, want 100", completed.TotalTokens)
	}
	if _, total, err := db.ListDailyReportRuns(models.DailyReportRunFilter{Limit: 10}); err != nil || total != 1 {
		t.Fatalf("successful retry count = %d, err=%v", total, err)
	}
	if refresher.calls.Load() != 0 {
		t.Fatalf("retry refreshed feeds %d times, want 0", refresher.calls.Load())
	}
	generator.mu.Lock()
	defer generator.mu.Unlock()
	if generator.resumeCalls != 1 || generator.lastHash != "checkpoint-fingerprint" || generator.lastCheckpoint != original.CheckpointJSON {
		t.Fatalf("resume call = count:%d hash:%q checkpoint:%q", generator.resumeCalls, generator.lastHash, generator.lastCheckpoint)
	}
}

func TestDailyReportRestartReusesRunAndPreservesTokenAudit(t *testing.T) {
	db := newDailyReportTestDB(t)
	defer db.Close()
	feedID, err := db.AddFeed(&models.Feed{Title: "Restart Feed", URL: "https://example.com/restart", Type: "rss"})
	if err != nil {
		t.Fatalf("AddFeed failed: %v", err)
	}
	now := time.Now().UTC()
	if err := db.SaveArticle(&models.Article{
		FeedID: feedID, Title: "Restart candidate", URL: "https://example.com/restart/article",
		OriginalSummary: "Summary", PublishedAt: now.Add(-time.Hour), FirstSeenAt: now.Add(-time.Hour), HasValidPublishedTime: true,
	}); err != nil {
		t.Fatalf("SaveArticle failed: %v", err)
	}
	original := &models.DailyReportRun{
		Kind: dailyreport.RunKindManual, Status: dailyreport.RunStatusFailed,
		PeriodStart: now.Add(-2 * time.Hour), PeriodEnd: now,
		GenerationMode: "ai", GenerationHash: "stale-fingerprint",
		CheckpointJSON: `{"version":1,"stage":"extracting","input_tokens":60,"output_tokens":15}`,
		ConfigSnapshot: `{}`, InputTokens: 60, OutputTokens: 15, TotalTokens: 75, AIUsed: true,
		Error: "previous failure", FailureCode: "schema_invalid",
	}
	originalID, err := db.CreateDailyReportRun(original)
	if err != nil {
		t.Fatalf("CreateDailyReportRun failed: %v", err)
	}
	original.ID = originalID
	createdAt := original.CreatedAt

	generator := &retryInspectionGenerator{}
	refresher := &countingDailyReportRefresher{}
	service := dailyreport.NewService(db, refresher, generator, dailyreport.NoopNotifier{}, dailyreport.RealClock(), time.UTC)
	restarted, err := service.Retry(context.Background(), originalID, true)
	if err != nil {
		t.Fatalf("Restart from beginning failed: %v", err)
	}
	if restarted.ID != originalID {
		t.Fatalf("restart run id = %d, want original id %d", restarted.ID, originalID)
	}
	completed := waitForDailyReportRunStatus(t, db, originalID, dailyreport.RunStatusCompleted)
	if completed.CreatedAt != createdAt {
		t.Fatalf("restart changed created_at: got %s want %s", completed.CreatedAt, createdAt)
	}
	if completed.TotalTokens != 75 {
		t.Fatalf("restart token audit = %d, want 75", completed.TotalTokens)
	}
	if completed.Error != "" || completed.FailureCode != "" {
		t.Fatalf("restart retained stale failure: error=%q code=%q", completed.Error, completed.FailureCode)
	}
	if _, total, err := db.ListDailyReportRuns(models.DailyReportRunFilter{Limit: 10}); err != nil || total != 1 {
		t.Fatalf("restart created another history row: total=%d err=%v", total, err)
	}
	if refresher.calls.Load() != 0 {
		t.Fatalf("restart refreshed feeds %d times, want 0", refresher.calls.Load())
	}
	generator.mu.Lock()
	defer generator.mu.Unlock()
	if generator.resumeCalls != 1 || generator.lastHash != "" || generator.lastCheckpoint != "" {
		t.Fatalf("restart call = count:%d hash:%q checkpoint:%q", generator.resumeCalls, generator.lastHash, generator.lastCheckpoint)
	}
}

func TestDailyReportAIFailureRequiresExplicitLocalFallback(t *testing.T) {
	db := newDailyReportTestDB(t)
	defer db.Close()
	feedID, err := db.AddFeed(&models.Feed{Title: "AI Feed", URL: "https://example.com/ai-feed", Type: "rss"})
	if err != nil {
		t.Fatalf("AddFeed failed: %v", err)
	}
	now := time.Now().UTC()
	if err := db.SaveArticle(&models.Article{
		FeedID: feedID, Title: "Artificial intelligence release", URL: "https://example.com/ai",
		OriginalSummary: `<p>New model</p>`, PublishedAt: now.Add(-time.Hour), FirstSeenAt: now.Add(-time.Hour), HasValidPublishedTime: true,
	}); err != nil {
		t.Fatalf("SaveArticle failed: %v", err)
	}
	service := dailyreport.NewService(db, nil, failedDailyReportGenerator{}, dailyreport.NoopNotifier{}, dailyreport.RealClock(), time.UTC)
	start, end := now.Add(-2*time.Hour), now
	run, err := service.StartManual(context.Background(), &start, &end)
	if err != nil {
		t.Fatalf("StartManual failed: %v", err)
	}
	failed := waitForDailyReportRunStatus(t, db, run.ID, dailyreport.RunStatusFailed)
	if failed.ContentJSON != "" || failed.Markdown != "" || failed.TotalTokens != 150 || failed.FailureCode != "timeout" {
		t.Fatalf("AI failure was replaced or lost audit data: %+v", failed)
	}
	localRun, err := service.UseLocalFallback(context.Background(), failed.ID)
	if err != nil {
		t.Fatalf("UseLocalFallback failed: %v", err)
	}
	local := waitForDailyReportRunStatus(t, db, localRun.ID, dailyreport.RunStatusCompleted)
	if local.GenerationMode != "local" || local.RetryOfID == nil || *local.RetryOfID != failed.ID || strings.Contains(local.Markdown, "<p>") {
		t.Fatalf("explicit local fallback is invalid: %+v", local)
	}
	reloadedFailed, _ := db.GetDailyReportRun(failed.ID)
	if reloadedFailed.TotalTokens != 150 || reloadedFailed.Status != dailyreport.RunStatusFailed {
		t.Fatalf("local fallback modified original AI audit: %+v", reloadedFailed)
	}
}

func (g *releasedDailyReportGenerator) Generate(_ context.Context, _ *models.DailyReportConfig, _ []models.DailyReportCandidate) (dailyreport.AIResult, error) {
	g.once.Do(func() { close(g.started) })
	<-g.release
	content := dailyreport.ReportContent{Sections: []dailyreport.ReportSection{{
		ID: "summary", Title: "Summary", Summary: "Completed", SourceIDs: []int{1},
	}}}
	return dailyreport.AIResult{Content: content, Markdown: "# Completed"}, nil
}

func (*releasedDailyReportGenerator) OptimizeOutline(context.Context, string, string, *int64) ([]dailyreport.OutlineSection, error) {
	return nil, nil
}

func TestDailyReportCompletionDoesNotOverwriteConcurrentConfig(t *testing.T) {
	db := newDailyReportTestDB(t)
	defer db.Close()
	feedID, err := db.AddFeed(&models.Feed{Title: "Test", URL: "https://example.com/feed", Type: "rss"})
	if err != nil {
		t.Fatalf("AddFeed failed: %v", err)
	}
	now := time.Now().UTC()
	if err := db.SaveArticle(&models.Article{
		FeedID: feedID, Title: "Concurrent config", URL: "https://example.com/article",
		PublishedAt: now.Add(-time.Hour), FirstSeenAt: now.Add(-time.Hour), HasValidPublishedTime: true,
	}); err != nil {
		t.Fatalf("SaveArticle failed: %v", err)
	}
	generator := &releasedDailyReportGenerator{started: make(chan struct{}), release: make(chan struct{})}
	service := dailyreport.NewService(db, nil, generator, dailyreport.NoopNotifier{}, dailyreport.RealClock(), time.UTC)
	start, end := now.Add(-2*time.Hour), now
	if _, err := service.StartManual(context.Background(), &start, &end); err != nil {
		t.Fatalf("StartManual failed: %v", err)
	}
	select {
	case <-generator.started:
	case <-time.After(2 * time.Second):
		t.Fatal("generator did not start")
	}
	config, err := service.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig during run failed: %v", err)
	}
	config.Enabled = false
	config.Focus = "updated while report was running"
	if _, err := service.SaveConfig(config); err != nil {
		t.Fatalf("SaveConfig during run failed: %v", err)
	}
	close(generator.release)
	if !service.WaitForRuns(2 * time.Second) {
		t.Fatal("daily report did not finish")
	}
	persisted, err := service.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig after run failed: %v", err)
	}
	if persisted.Enabled || persisted.Focus != "updated while report was running" {
		t.Fatalf("completed run overwrote concurrent config: %+v", persisted)
	}
}

func TestFindMatchingFeedItem(t *testing.T) {
	t.Parallel()

	publishedAt := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	oneMinuteLater := publishedAt.Add(time.Minute)
	h := &Handler{}

	tests := []struct {
		name      string
		article   *models.Article
		items     []*gofeed.Item
		wantIndex int
	}{
		{
			name: "empty URLs fall back to title and published time",
			article: &models.Article{
				Title:       "Target article",
				URL:         "",
				PublishedAt: publishedAt,
			},
			items: []*gofeed.Item{
				{Title: "First article", Link: "", PublishedParsed: &publishedAt},
				{Title: "Target article", Link: "", PublishedParsed: &oneMinuteLater},
			},
			wantIndex: 1,
		},
		{
			name: "matching title wins when multiple items share a URL",
			article: &models.Article{
				Title:       "Target article",
				URL:         "https://example.com/shared",
				PublishedAt: publishedAt,
			},
			items: []*gofeed.Item{
				{Title: "First article", Link: "https://example.com/shared"},
				{Title: "Target article", Link: "https://example.com/shared"},
			},
			wantIndex: 1,
		},
		{
			name: "non-empty URL remains a fallback when the title changes",
			article: &models.Article{
				Title:       "Stored title",
				URL:         "https://example.com/target?utm_source=rss",
				PublishedAt: publishedAt,
			},
			items: []*gofeed.Item{
				{Title: "First article", Link: "https://example.com/first"},
				{Title: "Updated source title", Link: "https://example.com/target"},
			},
			wantIndex: 1,
		},
		{
			name: "title-only fallback tolerates whitespace differences",
			article: &models.Article{
				Title:       "Target article",
				PublishedAt: publishedAt,
			},
			items: []*gofeed.Item{
				{Title: "First article"},
				{Title: "  Target   article  "},
			},
			wantIndex: 1,
		},
		{
			name: "unmatched article returns nil",
			article: &models.Article{
				Title:       "Missing article",
				PublishedAt: publishedAt,
			},
			items: []*gofeed.Item{
				{Title: "First article"},
				{Title: "Second article"},
			},
			wantIndex: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := h.findMatchingFeedItem(tt.article, tt.items)
			if tt.wantIndex == -1 {
				if got != nil {
					t.Fatalf("findMatchingFeedItem() = %q, want nil", got.Title)
				}
				return
			}

			if got != tt.items[tt.wantIndex] {
				t.Fatalf("findMatchingFeedItem() = %v, want item %d", got, tt.wantIndex)
			}
		})
	}
}
