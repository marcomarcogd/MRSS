package freshrss

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
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
				if r.URL.Path != root+"/reader/api/0/stream/items/contents" && r.Header.Get("Authorization") != "GoogleLogin auth=local-test-token" {
					http.Error(w, "missing auth", http.StatusUnauthorized)
					return
				}
				switch {
				case r.URL.Path == root+"/reader/api/0/tag/list":
					_, _ = fmt.Fprintf(w, `{"tags":[{"id":"user/%s/label/Technology"},{"id":"user/%s/state/com.google/read"}]}`, userID, userID)
				case provider == ProviderMiniflux && r.URL.Path == root+"/reader/api/0/stream/items/ids":
					if r.URL.Query().Get("xt") != TagRead || r.URL.Query().Get("c") != "next-page" {
						t.Error("stream filtering or pagination was lost")
					}
					_, _ = w.Write([]byte(`{"itemRefs":[{"id":"item-1"}],"continuation":"last-page"}`))
				case provider == ProviderMiniflux && r.URL.Path == root+"/reader/api/0/token":
					_, _ = w.Write([]byte("write-test-token"))
				case provider == ProviderMiniflux && r.URL.Path == root+"/reader/api/0/stream/items/contents":
					if err := r.ParseForm(); err != nil || r.Form.Get("T") != "write-test-token" || !reflect.DeepEqual(r.Form["i"], []string{"item-1"}) {
						t.Error("Miniflux item contents request was not preserved")
					}
					_, _ = fmt.Fprintf(w, `{"items":[{"id":"item-1","title":"Article","canonical":[{"href":"https://example.com/article"}],"summary":{"content":"Article body"},"published":1710000000,"updated":%d,"origin":{"streamId":"feed/1"}}]}`, updated)
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

func TestMinifluxGetStreamContentsUsesItemIDsThenContents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/reader/api/0/stream/items/ids":
			if r.Method != http.MethodGet || r.URL.Query().Get("s") != "feed/42" {
				t.Errorf("unexpected item IDs request: %s %s", r.Method, r.URL)
			}
			if got := r.Header.Get("Authorization"); got != "GoogleLogin auth=auth-token" {
				t.Errorf("authorization = %q", got)
			}
			if r.URL.Query().Get("n") != "100" || r.URL.Query().Get("c") != "100" || !reflect.DeepEqual(r.URL.Query()["xt"], []string{TagRead, TagStarred}) {
				t.Errorf("unexpected pagination/filter parameters: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"itemRefs":[{"id":"123"},{"id":"124"}],"continuation":"200"}`))
		case "/reader/api/0/token":
			_, _ = w.Write([]byte("auth-token"))
		case "/reader/api/0/stream/items/contents":
			if r.Method != http.MethodPost {
				t.Errorf("contents method = %s", r.Method)
			}
			if err := r.ParseForm(); err != nil {
				t.Error(err)
				return
			}
			if r.Form.Get("T") != "auth-token" || r.Form.Get("output") != "json" || !reflect.DeepEqual(r.Form["i"], []string{"123", "124"}) {
				t.Errorf("unexpected contents form: %v", r.Form)
			}
			_, _ = w.Write([]byte(`{"updated":1710000000,"items":[{"id":"123","title":"Entry","canonical":[{"href":"https://example.com/article"}],"summary":{"content":"body"},"published":1710000000,"updated":1710000001,"categories":["user/1/state/com.google/read"],"origin":{"streamId":"feed/42"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClientForProvider(server.URL, "user", "password", string(ProviderMiniflux))
	client.authToken = "auth-token"
	result, err := client.GetStreamContents(context.Background(), "feed/42", []string{TagRead, TagStarred}, 100, "100")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0].URL != "https://example.com/article" {
		t.Fatalf("unexpected items: %#v", result.Items)
	}
	if result.Items[0].Updated.Unix() != 1710000001 {
		t.Fatalf("updated = %d", result.Items[0].Updated.Unix())
	}
	if result.Continuation != "200" {
		t.Fatalf("continuation = %q", result.Continuation)
	}
}

func TestMinifluxStreamContentsFailuresAndEmptyPages(t *testing.T) {
	for _, tc := range []struct {
		name, ids, contents                    string
		idsStatus, tokenStatus, contentsStatus int
		wantErr                                bool
	}{
		{name: "empty", ids: `{"itemRefs":[]}`, idsStatus: 200},
		{name: "IDs unauthorized", idsStatus: 401, wantErr: true},
		{name: "invalid IDs JSON", idsStatus: 200, ids: `{`, wantErr: true},
		{name: "token failure", idsStatus: 200, ids: `{"itemRefs":[{"id":"123"}]}`, tokenStatus: 401, wantErr: true},
		{name: "contents failure", idsStatus: 200, ids: `{"itemRefs":[{"id":"123"}]}`, tokenStatus: 200, contentsStatus: 500, wantErr: true},
		{name: "invalid contents JSON", idsStatus: 200, ids: `{"itemRefs":[{"id":"123"}]}`, tokenStatus: 200, contentsStatus: 200, contents: `{`, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				status, body := 0, ""
				switch r.URL.Path {
				case "/reader/api/0/stream/items/ids":
					status, body = tc.idsStatus, tc.ids
				case "/reader/api/0/token":
					status, body = tc.tokenStatus, "auth-token"
				case "/reader/api/0/stream/items/contents":
					status, body = tc.contentsStatus, tc.contents
				}
				if status == 0 {
					t.Errorf("unexpected request: %s", r.URL)
					status = http.StatusInternalServerError
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			client := NewClientForProvider(server.URL, "user", "password", string(ProviderMiniflux))
			defer client.httpClient.CloseIdleConnections()
			client.authToken = "auth-token"
			result, err := client.GetStreamContents(context.Background(), "feed/42", nil, 100, "")
			if (err != nil) != tc.wantErr {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.wantErr && (result == nil || len(result.Items) != 0 || result.Continuation != "") {
				t.Fatalf("unexpected empty page: %+v", result)
			}
		})
	}
}

func TestFreshRSSStreamContentsRetainsOriginalEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/greader.php/reader/api/0/stream/contents/feed/42" || r.Header.Get("Authorization") != "GoogleLogin auth=auth-token" {
			t.Errorf("unexpected FreshRSS request: %s %s", r.Method, r.URL)
		}
		_, _ = w.Write([]byte(`{"items":[],"continuation":"next"}`))
	}))
	defer server.Close()
	client := NewClientForProvider(server.URL, "user", "password", string(ProviderFreshRSS))
	defer client.httpClient.CloseIdleConnections()
	client.authToken = "auth-token"
	result, err := client.GetStreamContents(context.Background(), "feed/42", nil, 100, "")
	if err != nil || result == nil || result.Continuation != "next" {
		t.Fatalf("unexpected FreshRSS result: %+v, %v", result, err)
	}
}
