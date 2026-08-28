package miniapp

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

var rawHTMLVisibleEscapeRE = regexp.MustCompile(`\\[nrt"]`)
var executableHTMLBlockRE = regexp.MustCompile(`(?is)<(?:script|style)\b[^>]*>.*?</(?:script|style)>`)

func htmlOutsideExecutableBlocks(html string) string {
	return executableHTMLBlockRE.ReplaceAllString(html, "")
}

func assertNoRawVisibleHTMLEscapes(t *testing.T, name, html string) {
	t.Helper()
	outside := htmlOutsideExecutableBlocks(html)
	if matches := rawHTMLVisibleEscapeRE.FindAllString(outside, -1); len(matches) != 0 {
		t.Fatalf("%s contains raw visible HTML escapes outside script/style: %v", name, matches)
	}
}

func TestMiniAppSourceHasNoRawVisibleHTMLEscapes(t *testing.T) {
	assertNoRawVisibleHTMLEscapes(t, "web/index.html", miniAppSource(t))
}

func TestServedMiniAppHasNoRawVisibleHTMLEscapes(t *testing.T) {
	handler := NewServer(Deps{}).Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/miniapp", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d", response.Code)
	}
	assertNoRawVisibleHTMLEscapes(t, "served /miniapp", response.Body.String())
}
