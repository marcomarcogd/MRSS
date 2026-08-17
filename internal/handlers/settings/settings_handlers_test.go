package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"MRSS/internal/database"
	"MRSS/internal/handlers/core"
)

func stubStartupOperations(t *testing.T, enable, disable func() error) {
	t.Helper()
	previousEnable := enableStartupRegistration
	previousDisable := disableStartupRegistration
	enableStartupRegistration = enable
	disableStartupRegistration = disable
	t.Cleanup(func() {
		enableStartupRegistration = previousEnable
		disableStartupRegistration = previousDisable
	})
}

func postSettings(t *testing.T, h *core.Handler, payload map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal settings payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	HandleSettings(h, w, req)
	return w
}

func setupHandlerWithDB(t *testing.T) *core.Handler {
	t.Helper()
	db, err := database.NewDB(":memory:")
	if err != nil {
		t.Fatalf("NewDB error: %v", err)
	}
	if err := db.Init(); err != nil {
		t.Fatalf("db Init error: %v", err)
	}
	return core.NewHandler(db, nil, nil, nil)
}

func TestHandleSettings_GET(t *testing.T) {
	h := setupHandlerWithDB(t)

	// Set a custom value
	h.DB.SetSetting("language", "xx-YY")

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	w := httptest.NewRecorder()

	HandleSettings(h, w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var data map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if data["language"] != "xx-YY" {
		t.Fatalf("expected language xx-YY, got %s", data["language"])
	}
}

func TestHandleSettings_POST(t *testing.T) {
	h := setupHandlerWithDB(t)

	payload := map[string]string{
		"update_interval":     "15",
		"translation_enabled": "true",
		"deepl_api_key":       "deadbeef",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	HandleSettings(h, w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	// Verify settings saved
	v, _ := h.DB.GetSetting("update_interval")
	if v != "15" {
		t.Fatalf("expected update_interval 15, got %s", v)
	}

	v2, _ := h.DB.GetSetting("translation_enabled")
	if v2 != "true" {
		t.Fatalf("expected translation_enabled true, got %s", v2)
	}

	// Encrypted key should be retrievable via GetEncryptedSetting
	dec, err := h.DB.GetEncryptedSetting("deepl_api_key")
	if err != nil {
		t.Fatalf("GetEncryptedSetting error: %v", err)
	}
	if dec != "deadbeef" {
		t.Fatalf("expected deepl_api_key decrypted to be deadbeef, got %s", dec)
	}
}

func TestHandleSettings_POSTDisablingFreshRSSCleansSyncedData(t *testing.T) {
	h := setupHandlerWithDB(t)

	if err := h.DB.SetSetting("freshrss_enabled", "true"); err != nil {
		t.Fatalf("SetSetting freshrss_enabled: %v", err)
	}

	res, err := h.DB.Exec(`
		INSERT INTO feeds (title, url, is_freshrss_source, freshrss_stream_id)
		VALUES (?, ?, 1, ?)
	`, "FreshRSS Feed", "https://example.com/freshrss.xml", "feed/1")
	if err != nil {
		t.Fatalf("insert FreshRSS feed: %v", err)
	}
	feedID, _ := res.LastInsertId()

	res, err = h.DB.Exec(`
		INSERT INTO articles (feed_id, title, url, published_at, unique_id)
		VALUES (?, ?, ?, datetime('now'), ?)
	`, feedID, "FreshRSS Article", "https://example.com/article", "fresh-article")
	if err != nil {
		t.Fatalf("insert FreshRSS article: %v", err)
	}
	articleID, _ := res.LastInsertId()

	if err := h.DB.SetArticleContent(articleID, "<p>cached</p>"); err != nil {
		t.Fatalf("SetArticleContent: %v", err)
	}
	if err := h.DB.EnqueueSyncChange(articleID, "https://example.com/article", database.SyncActionMarkRead); err != nil {
		t.Fatalf("EnqueueSyncChange: %v", err)
	}

	payload := map[string]string{"freshrss_enabled": "false"}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	HandleSettings(h, w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", resp.StatusCode, w.Body.String())
	}

	assertCount := func(query string, want int, args ...any) {
		t.Helper()
		var got int
		if err := h.DB.QueryRow(query, args...).Scan(&got); err != nil {
			t.Fatalf("count query failed %q: %v", query, err)
		}
		if got != want {
			t.Fatalf("query %q got %d, want %d", query, got, want)
		}
	}

	assertCount("SELECT COUNT(*) FROM feeds WHERE is_freshrss_source = 1", 0)
	assertCount("SELECT COUNT(*) FROM articles WHERE feed_id = ?", 0, feedID)
	assertCount("SELECT COUNT(*) FROM article_contents WHERE article_id = ?", 0, articleID)
	assertCount("SELECT COUNT(*) FROM freshrss_sync_queue", 0)

	enabled, err := h.DB.GetSetting("freshrss_enabled")
	if err != nil {
		t.Fatalf("GetSetting freshrss_enabled: %v", err)
	}
	if enabled != "false" {
		t.Fatalf("expected freshrss_enabled false, got %q", enabled)
	}
}

func TestHandleSettings_POSTUnchangedStartupSkipsSystemOperation(t *testing.T) {
	h := setupHandlerWithDB(t)
	if err := h.DB.SetSetting("startup_on_boot", "true"); err != nil {
		t.Fatalf("SetSetting startup_on_boot: %v", err)
	}

	enableCalls := 0
	disableCalls := 0
	stubStartupOperations(t, func() error {
		enableCalls++
		return nil
	}, func() error {
		disableCalls++
		return nil
	})

	w := postSettings(t, h, map[string]string{"startup_on_boot": "true"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}
	if enableCalls != 0 || disableCalls != 0 {
		t.Fatalf("startup operations called for unchanged value: enable=%d disable=%d", enableCalls, disableCalls)
	}
}

func TestHandleSettings_POSTChangedStartupAppliesOnce(t *testing.T) {
	h := setupHandlerWithDB(t)
	if err := h.DB.SetSetting("startup_on_boot", "false"); err != nil {
		t.Fatalf("SetSetting startup_on_boot: %v", err)
	}

	enableCalls := 0
	disableCalls := 0
	stubStartupOperations(t, func() error {
		enableCalls++
		return nil
	}, func() error {
		disableCalls++
		return nil
	})

	w := postSettings(t, h, map[string]string{"startup_on_boot": "true"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}
	if enableCalls != 1 || disableCalls != 0 {
		t.Fatalf("unexpected startup operations: enable=%d disable=%d", enableCalls, disableCalls)
	}
	value, err := h.DB.GetSetting("startup_on_boot")
	if err != nil {
		t.Fatalf("GetSetting startup_on_boot: %v", err)
	}
	if value != "true" {
		t.Fatalf("startup_on_boot = %q, want true", value)
	}
}

func TestHandleSettings_POSTStartupOperationFailureDoesNotPersist(t *testing.T) {
	h := setupHandlerWithDB(t)
	if err := h.DB.SetSetting("startup_on_boot", "false"); err != nil {
		t.Fatalf("SetSetting startup_on_boot: %v", err)
	}

	operationErr := errors.New("forced startup failure")
	stubStartupOperations(t, func() error { return operationErr }, func() error { return nil })

	w := postSettings(t, h, map[string]string{"startup_on_boot": "true"})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
	value, err := h.DB.GetSetting("startup_on_boot")
	if err != nil {
		t.Fatalf("GetSetting startup_on_boot: %v", err)
	}
	if value != "false" {
		t.Fatalf("startup_on_boot = %q after operation failure, want false", value)
	}
}

func TestHandleSettings_POSTPersistenceFailureReturnsError(t *testing.T) {
	h := setupHandlerWithDB(t)
	if err := h.DB.SetSetting("update_interval", "30"); err != nil {
		t.Fatalf("SetSetting update_interval: %v", err)
	}
	if _, err := h.DB.Exec(`
		CREATE TRIGGER fail_update_interval_insert
		BEFORE INSERT ON settings
		WHEN NEW.key = 'update_interval'
		BEGIN
			SELECT RAISE(ABORT, 'forced settings failure');
		END;
	`); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	w := postSettings(t, h, map[string]string{"update_interval": "31"})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
	value, err := h.DB.GetSetting("update_interval")
	if err != nil {
		t.Fatalf("GetSetting update_interval: %v", err)
	}
	if value != "30" {
		t.Fatalf("update_interval = %q after persistence failure, want 30", value)
	}
}

func TestHandleSettings_POSTStartupPersistenceFailureRollsBackSystemOperation(t *testing.T) {
	h := setupHandlerWithDB(t)
	if err := h.DB.SetSetting("startup_on_boot", "false"); err != nil {
		t.Fatalf("SetSetting startup_on_boot: %v", err)
	}
	if _, err := h.DB.Exec(`
		CREATE TRIGGER fail_startup_setting_insert
		BEFORE INSERT ON settings
		WHEN NEW.key = 'startup_on_boot'
		BEGIN
			SELECT RAISE(ABORT, 'forced startup setting failure');
		END;
	`); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	enableCalls := 0
	disableCalls := 0
	stubStartupOperations(t, func() error {
		enableCalls++
		return nil
	}, func() error {
		disableCalls++
		return nil
	})

	w := postSettings(t, h, map[string]string{"startup_on_boot": "true"})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
	if enableCalls != 1 || disableCalls != 1 {
		t.Fatalf("expected apply and rollback once, got enable=%d disable=%d", enableCalls, disableCalls)
	}
	value, err := h.DB.GetSetting("startup_on_boot")
	if err != nil {
		t.Fatalf("GetSetting startup_on_boot: %v", err)
	}
	if value != "false" {
		t.Fatalf("startup_on_boot = %q after persistence failure, want false", value)
	}
}
