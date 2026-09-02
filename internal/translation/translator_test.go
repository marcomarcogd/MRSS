package translation

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"MRSS/internal/config"
)

type countingTranslator struct {
	calls int
}

func (t *countingTranslator) Translate(text, targetLang string) (string, error) {
	t.calls++
	return fmt.Sprintf("[%s] %s", targetLang, text), nil
}

type memoryTranslationCache struct {
	items map[string]string
}

func (c *memoryTranslationCache) key(hash, targetLang, provider string) string {
	return hash + "\x00" + targetLang + "\x00" + provider
}

func (c *memoryTranslationCache) GetCachedTranslation(hash, targetLang, provider string) (string, bool, error) {
	value, ok := c.items[c.key(hash, targetLang, provider)]
	return value, ok, nil
}

func (c *memoryTranslationCache) SetCachedTranslation(hash, _ string, targetLang, translated, provider string) error {
	c.items[c.key(hash, targetLang, provider)] = translated
	return nil
}

func TestMockTranslator(t *testing.T) {
	translator := NewMockTranslator()
	text := "Hello"
	targetLang := "es"

	translated, err := translator.Translate(text, targetLang)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}

	expected := "[ES] Hello"
	if translated != expected {
		t.Errorf("Expected '%s', got '%s'", expected, translated)
	}

	// Test idempotency (mock implementation detail)
	translated2, err := translator.Translate(translated, targetLang)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if translated2 != expected {
		t.Errorf("Expected '%s', got '%s'", expected, translated2)
	}
}

func TestCachedTranslatorReusesResultsByLanguageAndProvider(t *testing.T) {
	cache := &memoryTranslationCache{items: make(map[string]string)}
	base := &countingTranslator{}
	google := NewCachedTranslator(base, cache, "google")

	first, err := google.Translate("Hello", "zh")
	if err != nil {
		t.Fatalf("first Translate failed: %v", err)
	}
	second, err := google.Translate("Hello", "zh")
	if err != nil {
		t.Fatalf("cached Translate failed: %v", err)
	}
	if first != second || base.calls != 1 {
		t.Fatalf("cache was not reused: first=%q second=%q calls=%d", first, second, base.calls)
	}

	if _, err := google.Translate("Hello", "ja"); err != nil {
		t.Fatalf("different-language Translate failed: %v", err)
	}
	deepl := NewCachedTranslator(base, cache, "deepl")
	if _, err := deepl.Translate("Hello", "zh"); err != nil {
		t.Fatalf("different-provider Translate failed: %v", err)
	}
	if base.calls != 3 {
		t.Fatalf("language/provider cache keys were not isolated; calls=%d, want 3", base.calls)
	}
}

func TestBaiduTranslator_EmptyText(t *testing.T) {
	translator := NewBaiduTranslator("app_id", "secret_key")
	result, err := translator.Translate("", "zh")
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if result != "" {
		t.Errorf("Expected empty string, got '%s'", result)
	}
}

func TestBaiduLangMapping(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"en", "en"},
		{"zh", "zh"},
		{"ja", "jp"},
		{"es", "spa"},
		{"fr", "fra"},
		{"unknown", "unknown"},
	}

	for _, tt := range tests {
		result := mapToBaiduLang(tt.input)
		if result != tt.expected {
			t.Errorf("mapToBaiduLang(%s) = %s, want %s", tt.input, result, tt.expected)
		}
	}
}

func TestAITranslator_EmptyText(t *testing.T) {
	translator := NewAITranslator("api_key", "", "")
	result, err := translator.Translate("", "zh")
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if result != "" {
		t.Errorf("Expected empty string, got '%s'", result)
	}
}

func TestAITranslator_Defaults(t *testing.T) {
	defaults := config.Get()
	translator := NewAITranslator("api_key", "", "")
	if translator.Endpoint != defaults.AIEndpoint {
		t.Errorf("Expected default endpoint %s, got '%s'", defaults.AIEndpoint, translator.Endpoint)
	}
	if translator.Model != defaults.AIModel {
		t.Errorf("Expected default model %s, got '%s'", defaults.AIModel, translator.Model)
	}
}

func TestAITranslatorKeepsHTTPClientWhenConfigurationChanges(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test-Header") != "kept" {
			t.Fatalf("custom header was not sent")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"translated"}}]}`))
	}))
	defer server.Close()

	translator := NewAITranslator("test-key", server.URL, "test-model")
	originalClient := translator.httpClient
	translator.SetSystemPrompt("Translate exactly")
	translator.SetCustomHeaders(`{"X-Test-Header":"kept"}`)

	if translator.httpClient != originalClient {
		t.Fatal("configuration changes replaced the injected HTTP client")
	}
	translated, err := translator.Translate("hello", "zh")
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if translated != "translated" {
		t.Fatalf("unexpected translation %q", translated)
	}
}

func TestFactoryAIProviderUsesGlobalProxyClient(t *testing.T) {
	settings := &mockSettingsProvider{settings: map[string]string{
		"proxy_enabled":  "true",
		"proxy_type":     "http",
		"proxy_host":     "127.0.0.1",
		"proxy_port":     "3128",
		"proxy_username": "user",
		"proxy_password": "password",
	}}
	factory := NewFactory(settings)
	provider := factory.createAIProvider(&aiConfig{
		APIKey:        "key",
		Endpoint:      "https://api.example.com/v1/chat/completions",
		Model:         "model",
		SystemPrompt:  "prompt",
		CustomHeaders: `{"X-Test":"value"}`,
	})

	transport, ok := provider.(*aiProvider).translator.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("unexpected transport type %T", provider.(*aiProvider).translator.httpClient.Transport)
	}
	request := &http.Request{URL: mustParseURL(t, "https://api.example.com")}
	proxyURL, err := transport.Proxy(request)
	if err != nil {
		t.Fatalf("proxy lookup failed: %v", err)
	}
	if proxyURL == nil || proxyURL.String() != "http://user:password@127.0.0.1:3128" {
		t.Fatalf("unexpected proxy URL %v", proxyURL)
	}
	if !transport.ForceAttemptHTTP2 {
		t.Fatalf("expected actual translation transport to attempt HTTP/2")
	}
}

func mustParseURL(t *testing.T, rawURL string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("failed to parse URL: %v", err)
	}
	return parsed
}

func TestGetLanguageName(t *testing.T) {
	tests := []struct {
		code     string
		expected string
	}{
		{"en", "English"},
		{"zh", "Simplified Chinese"},
		{"zh-TW", "Traditional Chinese"},
		{"ja", "Japanese"},
		{"unknown", "unknown"},
	}

	for _, tt := range tests {
		result := getLanguageName(tt.code)
		if result != tt.expected {
			t.Errorf("getLanguageName(%s) = %s, want %s", tt.code, result, tt.expected)
		}
	}
}

// mockSettingsProvider implements SettingsProvider for testing
type mockSettingsProvider struct {
	settings map[string]string
}

func (m *mockSettingsProvider) GetSetting(key string) (string, error) {
	return m.settings[key], nil
}

func (m *mockSettingsProvider) GetEncryptedSetting(key string) (string, error) {
	// In tests, just return the plain value (mock doesn't encrypt)
	return m.settings[key], nil
}

func TestDynamicTranslator_DefaultsToGoogle(t *testing.T) {
	provider := &mockSettingsProvider{
		settings: map[string]string{},
	}
	translator := NewDynamicTranslator(provider)

	// Should return empty for empty text without error
	result, err := translator.Translate("", "zh")
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if result != "" {
		t.Errorf("Expected empty string, got '%s'", result)
	}
}

func TestDynamicTranslator_RequiresCredentials(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		settings map[string]string
		wantErr  bool
	}{
		{
			name:     "deepl_no_key",
			provider: "deepl",
			settings: map[string]string{"translation_provider": "deepl"},
			wantErr:  true, // No API key
		},
		{
			name:     "baidu_no_credentials",
			provider: "baidu",
			settings: map[string]string{"translation_provider": "baidu"},
			wantErr:  true, // No app ID or secret key
		},
		{
			name:     "baidu_partial",
			provider: "baidu",
			settings: map[string]string{"translation_provider": "baidu", "baidu_app_id": "id"},
			wantErr:  true, // No secret key
		},
		{
			name:     "ai_no_key",
			provider: "ai",
			settings: map[string]string{"translation_provider": "ai"},
			wantErr:  true, // No API key
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &mockSettingsProvider{settings: tt.settings}
			translator := NewDynamicTranslator(provider)
			_, err := translator.Translate("Hello", "zh")
			if (err != nil) != tt.wantErr {
				t.Errorf("Translate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCreateHTTPClientWithProxy_EnabledAndDisabled(t *testing.T) {
	// Disabled
	provider1 := &mockSettingsProvider{settings: map[string]string{"proxy_enabled": "false"}}
	c1, err := CreateHTTPClientWithProxy(provider1, 1*time.Second)
	if err != nil {
		t.Fatalf("CreateHTTPClientWithProxy error: %v", err)
	}
	tr1, ok := c1.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("unexpected transport type")
	}
	if tr1.Proxy != nil {
		t.Fatalf("expected no proxy when disabled")
	}

	// Enabled
	provider2 := &mockSettingsProvider{settings: map[string]string{
		"proxy_enabled":  "true",
		"proxy_type":     "http",
		"proxy_host":     "127.0.0.1",
		"proxy_port":     "3128",
		"proxy_username": "u",
		"proxy_password": "p",
	}}
	c2, err := CreateHTTPClientWithProxy(provider2, 1*time.Second)
	if err != nil {
		t.Fatalf("CreateHTTPClientWithProxy error: %v", err)
	}
	tr2, ok := c2.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("unexpected transport type")
	}
	if tr2.Proxy == nil {
		t.Fatalf("expected proxy to be configured when enabled")
	}
}
