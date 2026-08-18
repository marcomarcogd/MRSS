package update

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"MRSS/internal/handlers/core"
)

type updateDownloadRoundTripper func(*http.Request) (*http.Response, error)

func (f updateDownloadRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestHandleDownloadUpdate_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/update/download", nil)
	rr := httptest.NewRecorder()

	HandleDownloadUpdate(&core.Handler{}, rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected %d got %d", http.StatusMethodNotAllowed, rr.Code)
	}
}

func TestMRSSReleaseConfiguration(t *testing.T) {
	if releaseRepository != "marcomarcogd/MRSS" {
		t.Fatalf("unexpected release repository %q", releaseRepository)
	}
	if !strings.Contains(githubReleaseDownloadURLPrefix, "/marcomarcogd/MRSS/") {
		t.Fatalf("download prefix points to the wrong repository: %s", githubReleaseDownloadURLPrefix)
	}
	if !isMRSSReleaseAsset("MRSS-1.5.0-windows-amd64-installer.exe") {
		t.Fatalf("new MRSS asset name should be accepted")
	}
	if isMRSSReleaseAsset("MrRSS-1.4.2-windows-amd64-installer.exe") {
		t.Fatalf("legacy asset name should not be selected for MRSS updates")
	}
}

func TestHandleDownloadUpdate_InvalidURLPrefix(t *testing.T) {
	body := bytes.NewReader([]byte(`{"download_url":"https://malicious.com/file","asset_name":"app.zip"}`))
	req := httptest.NewRequest(http.MethodPost, "/update/download", body)
	rr := httptest.NewRecorder()

	HandleDownloadUpdate(&core.Handler{}, rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid url prefix, got %d", rr.Code)
	}
}

func TestHandleDownloadUpdate_InvalidAssetName(t *testing.T) {
	body := bytes.NewReader([]byte(`{"download_url":"https://github.com/marcomarcogd/MRSS/releases/download/v1/app.zip","asset_name":"../secret"}`))
	req := httptest.NewRequest(http.MethodPost, "/update/download", body)
	rr := httptest.NewRecorder()

	HandleDownloadUpdate(&core.Handler{}, rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid asset name, got %d", rr.Code)
	}
}

func TestHandleDownloadUpdate_RetriesUseUniqueDirectories(t *testing.T) {
	originalTransport := http.DefaultTransport
	http.DefaultTransport = updateDownloadRoundTripper(func(req *http.Request) (*http.Response, error) {
		if !strings.HasPrefix(req.URL.String(), githubReleaseDownloadURLPrefix) {
			t.Fatalf("unexpected download URL: %s", req.URL)
		}
		content := "test installer"
		return &http.Response{
			StatusCode:    http.StatusOK,
			Body:          io.NopCloser(strings.NewReader(content)),
			ContentLength: int64(len(content)),
			Header:        make(http.Header),
			Request:       req,
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	assetName := "MRSS-9.9.9-windows-amd64-installer.exe"
	download := func() string {
		t.Helper()
		body := bytes.NewReader([]byte(`{"download_url":"https://github.com/marcomarcogd/MRSS/releases/download/v9.9.9/MRSS-9.9.9-windows-amd64-installer.exe","asset_name":"MRSS-9.9.9-windows-amd64-installer.exe"}`))
		req := httptest.NewRequest(http.MethodPost, "/update/download", body)
		rr := httptest.NewRecorder()

		HandleDownloadUpdate(&core.Handler{}, rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var result struct {
			Success  bool   `json:"success"`
			FilePath string `json:"file_path"`
		}
		if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if !result.Success || filepath.Base(result.FilePath) != assetName {
			t.Fatalf("unexpected download response: %#v", result)
		}
		if data, err := os.ReadFile(result.FilePath); err != nil || string(data) != "test installer" {
			t.Fatalf("downloaded file mismatch: data=%q err=%v", data, err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(result.FilePath)) })
		return result.FilePath
	}

	firstPath := download()
	secondPath := download()
	if firstPath == secondPath || filepath.Dir(firstPath) == filepath.Dir(secondPath) {
		t.Fatalf("retry reused a locked download path: first=%s second=%s", firstPath, secondPath)
	}
}
