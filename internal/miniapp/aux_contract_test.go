package miniapp

import (
	"os"
	"strings"
	"testing"
)

func TestAuxiliaryScreenAPIContractsAndRoutes(t *testing.T) {
	b, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"function wishesPage()", "function issuesPage()", "function settingsPage()",
		"function loadWishes()", "function loadIssues()", "function loadSettings()",
		"/api/miniapp/v1/wishes", "/api/miniapp/v1/issues", "/api/miniapp/v1/me",
		"method:\"POST\"", "tmdb_id:Number(mediaID(x))", "type:mediaType(x)", "season:season",
		"JSON.stringify({title:t,description:d,media_type:\"\",media_id:\"\",media_title:\"\"})",
		"raw===\"wishes\"", "raw===\"issues\"", "raw===\"settings\"",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing auxiliary contract %q", want)
		}
	}
	if strings.Contains(s, "?initData=") || strings.Contains(s, "initDataUnsafe?.user?.id") {
		t.Fatal("Mini App auth must use only X-Telegram-Init-Data")
	}
	if strings.Count(s, "/api/miniapp/v1/wishes") < 2 || strings.Count(s, "/api/miniapp/v1/issues") < 2 {
		t.Fatal("wishes/issues must each retain GET and POST paths")
	}
}

func TestAuxiliaryScreensHaveConcreteEmptyStateActions(t *testing.T) {
	b, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{"aux-empty", "goSearch()", "issueForm()", "auxGrid", "goHome()"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing concrete auxiliary next step %q", want)
		}
	}
}
