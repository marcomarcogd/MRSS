package update

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"MRSS/internal/handlers/core"
)

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
