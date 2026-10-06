package dashboard

import (
	"net/http"
	"net/http/httptest"
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
