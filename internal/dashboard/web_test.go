package dashboard

import (
	"bytes"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestDashboardEmbedsWebAssets(t *testing.T) {
	server := testServer(t, 8765)
	for _, test := range []struct {
		path        string
		contentType string
		contains    string
	}{
		{path: "/", contentType: "text/html", contains: "Usage Dashboard"},
		{path: "/assets/style.css", contentType: "text/css", contains: "color-scheme: dark"},
		{path: "/assets/app.js", contentType: "javascript", contains: "/api/stats"},
	} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765"+test.path, nil)
		server.handler().ServeHTTP(rr, req)
		if rr.Code != http.StatusOK || !strings.Contains(rr.Header().Get("Content-Type"), test.contentType) || !strings.Contains(rr.Body.String(), test.contains) {
			t.Errorf("GET %s status=%d type=%q body=%q", test.path, rr.Code, rr.Header().Get("Content-Type"), rr.Body.String()[:min(160, rr.Body.Len())])
		}
	}
}

func TestDashboardServesCanonicalLogoAndLoginAssets(t *testing.T) {
	server := testServer(t, 8765)
	canonicalLogo, err := os.ReadFile("../../docs/images/logo.png")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/assets/logo.png", "/favicon.ico"} {
		rr := httptest.NewRecorder()
		server.handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765"+path, nil))
		if rr.Code != http.StatusOK || !strings.Contains(rr.Header().Get("Content-Type"), "image/png") {
			t.Fatalf("GET %s status=%d content-type=%q", path, rr.Code, rr.Header().Get("Content-Type"))
		}
		if !bytes.Equal(rr.Body.Bytes(), canonicalLogo) {
			t.Errorf("GET %s did not serve docs/images/logo.png", path)
		}
	}
	for _, path := range []string{"/assets/login-duck-base.png", "/assets/login-duck-hands.png"} {
		rr := httptest.NewRecorder()
		server.handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765"+path, nil))
		if rr.Code != http.StatusOK || !strings.Contains(rr.Header().Get("Content-Type"), "image/png") || rr.Body.Len() == 0 {
			t.Fatalf("GET %s status=%d content-type=%q bytes=%d", path, rr.Code, rr.Header().Get("Content-Type"), rr.Body.Len())
		}
		illustration, err := png.Decode(bytes.NewReader(rr.Body.Bytes()))
		if err != nil {
			t.Errorf("GET %s returned invalid PNG: %v", path, err)
			continue
		}
		if illustration.Bounds().Dx() != 1254 || illustration.Bounds().Dy() != 1254 {
			t.Errorf("GET %s size=%v, want 1254x1254 aligned layers", path, illustration.Bounds())
		}
		_, _, _, alpha := illustration.At(0, 0).RGBA()
		if alpha != 0 {
			t.Errorf("GET %s is not transparent at the canvas corner", path)
		}
	}
}

func TestDashboardStatsVisualizationsAreConnectedToAPIFields(t *testing.T) {
	app, err := webFiles.ReadFile("web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	page, err := webFiles.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"renderActivityGrid", "renderTokenComposition", "renderModelComparison", "renderRecoveryChart", "dailyActivity", "user_tokens_estimate", "assistant_tokens_estimate", "context_tokens_estimate", "vqd_refresh_count", "header_refresh_count", "activityGridWindow"} {
		if !bytes.Contains(app, []byte(expected)) && !bytes.Contains(page, []byte(expected)) {
			t.Errorf("dashboard assets do not yet include %q", expected)
		}
	}
}

func TestDashboardActivityGridUsesSemanticTable(t *testing.T) {
	app, err := webFiles.ReadFile("web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	page, err := webFiles.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`<table class="activity-grid-table"`, `scope = "row"`, "visually-hidden", "activity not tracked"} {
		if !bytes.Contains(page, []byte(expected)) && !bytes.Contains(app, []byte(expected)) {
			t.Errorf("activity grid accessibility markup is missing %q", expected)
		}
	}
}

func TestDashboardLoginFormUsesPOSTFallback(t *testing.T) {
	page, err := webFiles.ReadFile("web/login.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`method="post"`, `action="/auth/login"`, `href="/favicon.ico"`} {
		if !bytes.Contains(page, []byte(expected)) {
			t.Errorf("login page is missing %q", expected)
		}
	}
}
