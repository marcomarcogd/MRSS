package freshrss

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewClientForProviderUsesCorrectGoogleReaderRoot(t *testing.T) {
	tests := []struct {
		name      string
		provider  string
		serverURL string
		wantRoot  string
	}{
		{
			name:      "FreshRSS appends greader endpoint",
			provider:  string(ProviderFreshRSS),
			serverURL: "https://freshrss.example.com/",
			wantRoot:  "https://freshrss.example.com/api/greader.php",
		},
		{
			name:      "Miniflux uses server root",
			provider:  string(ProviderMiniflux),
			serverURL: "https://miniflux.example.com/miniflux/",
			wantRoot:  "https://miniflux.example.com/miniflux",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClientForProvider(tt.serverURL, "user", "password", tt.provider)
			if client.baseURL != tt.wantRoot {
				t.Fatalf("baseURL = %q, want %q", client.baseURL, tt.wantRoot)
			}
		})
	}
}

func TestGoogleReaderUpdatedTimeSupportsSecondsAndMilliseconds(t *testing.T) {
	if got := googleReaderUpdatedTime(1_710_000_300).Unix(); got != 1_710_000_300 {
		t.Fatalf("seconds timestamp = %d", got)
	}
	if got := googleReaderUpdatedTime(1_710_000_300_000).Unix(); got != 1_710_000_300 {
		t.Fatalf("milliseconds timestamp = %d", got)
	}
}

func TestProvidersUseGoogleReaderLoginCategoriesAndStreams(t *testing.T) {
	for _, provider := range []Provider{ProviderFreshRSS, ProviderMiniflux} {
		t.Run(string(provider), func(t *testing.T) {
			root, userID, updated := "/reader", "42", int64(1_710_000_300)
			if provider == ProviderFreshRSS {
				root, userID, updated = "/reader/api/greader.php", "-", updated*1000
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == root+"/accounts/ClientLogin" {
					if err := r.ParseForm(); err != nil || r.Form.Get("Email") != "test-user" || r.Form.Get("Passwd") != "test-password" {
						t.Error("Google Reader login form was not preserved")
						http.Error(w, "invalid login", http.StatusBadRequest)
						return
					}
					_, _ = w.Write([]byte("Auth=local-test-token\n"))
					return
				}
				if r.Header.Get("Authorization") != "GoogleLogin auth=local-test-token" {
					http.Error(w, "missing auth", http.StatusUnauthorized)
					return
				}
				switch {
				case r.URL.Path == root+"/reader/api/0/tag/list":
					_, _ = fmt.Fprintf(w, `{"tags":[{"id":"user/%s/label/Technology"},{"id":"user/%s/state/com.google/read"}]}`, userID, userID)
				case strings.HasPrefix(r.URL.Path, root+"/reader/api/0/stream/contents/"):
					if r.URL.Query().Get("xt") != TagRead || r.URL.Query().Get("c") != "next-page" {
						t.Error("stream filtering or pagination was lost")
					}
					_, _ = fmt.Fprintf(w, `{"items":[{"id":"item-1","title":"Article","canonical":[{"href":"https://example.com/article"}],"summary":{"content":"Article body"},"published":1710000000,"updated":%d,"origin":{"streamId":"feed/1"}}],"continuation":"last-page"}`, updated)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			client := NewClientForProvider(server.URL+"/reader/", "test-user", "test-password", string(provider))
			ctx := context.Background()
			if err := client.Login(ctx); err != nil {
				t.Fatal(err)
			}
			categories, err := client.GetCategories(ctx)
			if err != nil || len(categories) != 1 || categories[0].Label != "Technology" {
				t.Fatalf("categories=%+v err=%v", categories, err)
			}
			stream, err := client.GetStreamContents(ctx, "user/-/state/com.google/reading-list", []string{TagRead}, 50, "next-page")
			if err != nil {
				t.Fatal(err)
			}
			if len(stream.Items) != 1 || stream.Continuation != "last-page" || stream.Items[0].Updated.Unix() != 1_710_000_300 || stream.Items[0].Content != "Article body" || stream.Items[0].OriginStreamID != "feed/1" {
				t.Fatalf("stream=%+v", stream)
			}
		})
	}
}
