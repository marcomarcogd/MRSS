package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
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
	if config.ScheduleTime != "08:00" || config.FeedScope != "all" || config.Enabled {
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
	if _, err := h.DailyReportService.SaveConfig(config); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
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
	if _, err := h.DailyReportService.SaveConfig(config); err != nil {
		t.Fatalf("SaveConfig(first) failed: %v", err)
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
	}{
		{name: "429 is retried twice", statusCode: http.StatusTooManyRequests, failures: 2, wantCalls: 4},
		{name: "5xx is retried twice", statusCode: http.StatusServiceUnavailable, failures: 2, wantCalls: 4},
		{name: "ordinary 4xx is not retried", statusCode: http.StatusBadRequest, failures: 1, wantCalls: 1, wantError: true},
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
				response := `{"insights":[{"summary":"verified insight","source_ids":[1,999]}]}`
				if strings.Contains(userPrompt, "Fill the requested outline") {
					response = `{"sections":[{"id":"highlights","title":"Highlights","summary":"verified report","source_ids":[1,999]}]}`
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
				ArticleID: 1, FeedID: 1, Title: "A trusted title", FeedTitle: "Test feed", Summary: "Cached summary",
			}})
			if tt.wantError {
				if generateErr == nil {
					t.Fatal("Generate succeeded, want an ordinary 4xx error")
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
	candidate := []models.DailyReportCandidate{{ArticleID: 1, FeedID: 1, Title: "Title", Summary: "Summary"}}

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
			content := `{"insights":[{"summary":"local","source_ids":[1]}]}`
			if call > 1 {
				content = `{"sections":[{"id":"highlights","title":"Highlights","summary":"local","source_ids":[1]}]}`
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

func TestDailyReportAIGeneratorBatchesLargeCachedArticles(t *testing.T) {
	var extractionCalls atomic.Int32
	var totalCalls atomic.Int32
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
		if strings.Contains(joined, "Fill the requested outline") {
			writeOllamaResponse(t, w, `{"sections":[{"id":"highlights","title":"Highlights","summary":"Combined","source_ids":[1]}]}`)
			return
		}
		extractionCalls.Add(1)
		writeOllamaResponse(t, w, `{"insights":[{"summary":"Extracted","source_ids":[1]}]}`)
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
	candidates := make([]models.DailyReportCandidate, 0, 8)
	for index := 0; index < 8; index++ {
		candidates = append(candidates, models.DailyReportCandidate{
			ArticleID: int64(index + 1), FeedID: 1, Title: "Article", Content: longContent,
		})
	}
	generator := dailyreport.NewAIGenerator(db, ai.NewUsageTracker(db), nil, dailyreport.WithAIGeneratorRetryDelays(0))
	if _, err := generator.Generate(context.Background(), dailyReportGeneratorConfig(profileID), candidates); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if extractionCalls.Load() < 2 || totalCalls.Load() < 3 {
		t.Fatalf("Large input was not batched: extraction=%d total=%d", extractionCalls.Load(), totalCalls.Load())
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
