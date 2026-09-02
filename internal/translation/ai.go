package translation

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"MRSS/internal/ai"
	"MRSS/internal/config"
)

// AITranslator implements translation using OpenAI-compatible APIs (GPT, Claude, etc.).
type AITranslator struct {
	APIKey        string
	Endpoint      string
	Model         string
	SystemPrompt  string
	CustomHeaders string
	httpClient    *http.Client
	client        *ai.Client
}

// NewAITranslator creates a new AI translator with the given credentials.
// endpoint should be the full API URL (e.g., "https://api.openai.com/v1/chat/completions" for OpenAI, "http://localhost:11434/api/generate" for Ollama)
// model should be the model name (e.g., "gpt-4o-mini", "claude-3-haiku-20240307")
func NewAITranslator(apiKey, endpoint, model string) *AITranslator {
	defaults := config.Get()
	// Default to OpenAI endpoint if not specified
	if endpoint == "" {
		endpoint = defaults.AIEndpoint
	}
	// Default to a cost-effective model if not specified
	if model == "" {
		model = defaults.AIModel
	}

	httpClient, err := CreateHTTPClientWithProxy(nil, 30*time.Second)
	if err != nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	translator := &AITranslator{
		APIKey:        apiKey,
		Endpoint:      strings.TrimSuffix(endpoint, "/"),
		Model:         model,
		SystemPrompt:  "",
		CustomHeaders: "",
		httpClient:    httpClient,
	}
	translator.rebuildClient()
	return translator
}

// NewAITranslatorWithDB creates a new AI translator with database for proxy support
func NewAITranslatorWithDB(apiKey, endpoint, model string, db DBInterface) *AITranslator {
	defaults := config.Get()
	if endpoint == "" {
		endpoint = defaults.AIEndpoint
	}
	if model == "" {
		model = defaults.AIModel
	}

	httpClient, err := CreateHTTPClientWithProxy(db, 30*time.Second)
	if err != nil {
		// Fallback to default client if proxy creation fails
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	translator := &AITranslator{
		APIKey:        apiKey,
		Endpoint:      strings.TrimSuffix(endpoint, "/"),
		Model:         model,
		SystemPrompt:  "",
		CustomHeaders: "",
		httpClient:    httpClient,
	}
	translator.rebuildClient()
	return translator
}

func (t *AITranslator) rebuildClient() {
	clientConfig := ai.ClientConfig{
		APIKey:        t.APIKey,
		Endpoint:      t.Endpoint,
		Model:         t.Model,
		SystemPrompt:  t.SystemPrompt,
		CustomHeaders: t.CustomHeaders,
		Timeout:       30 * time.Second,
	}
	t.client = ai.NewClientWithHTTPClient(clientConfig, t.httpClient)
}

// SetSystemPrompt sets a custom system prompt for the translator.
func (t *AITranslator) SetSystemPrompt(prompt string) {
	t.SystemPrompt = prompt
	t.rebuildClient()
}

// SetCustomHeaders sets custom headers for AI requests.
func (t *AITranslator) SetCustomHeaders(headers string) {
	t.CustomHeaders = headers
	t.rebuildClient()
}

// Translate translates text to the target language using an OpenAI-compatible API.
// Automatically detects and adapts to different API formats (Gemini, OpenAI, Ollama).
func (t *AITranslator) Translate(text, targetLang string) (string, error) {
	if text == "" {
		return "", nil
	}

	langName := getLanguageName(targetLang)

	// Use custom system prompt if provided, otherwise use default
	systemPrompt := t.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = "You are a translator. Translate the given text accurately. Output ONLY the translated text, nothing else."
	}
	userPrompt := fmt.Sprintf("Translate to %s:\n%s", langName, text)

	// Use the universal client which handles format detection automatically
	result, err := t.client.RequestWithThinking(systemPrompt, userPrompt)
	if err != nil {
		return "", err
	}

	// Clean up the response:
	// 1) remove model thinking blocks if they leaked into main content
	// 2) trim quotes/whitespace
	translated := ai.RemoveThinkingTags(result.Content)
	translated = strings.Trim(translated, "\"'")
	return translated, nil
}

// getLanguageName converts a language code to a human-readable name.
func getLanguageName(code string) string {
	langNames := map[string]string{
		"en":    "English",
		"zh":    "Simplified Chinese",
		"zh-TW": "Traditional Chinese",
		"es":    "Spanish",
		"fr":    "French",
		"de":    "German",
		"ja":    "Japanese",
		"ko":    "Korean",
		"pt":    "Portuguese",
		"ru":    "Russian",
		"it":    "Italian",
		"ar":    "Arabic",
	}
	if name, ok := langNames[code]; ok {
		return name
	}
	return code
}
