package services

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetAllSubscriptionsAllowsResponseAboveGenericLimit(t *testing.T) {
	padding := strings.Repeat("x", maxResponseBodySize+1024)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":1,"name":"large","extra":"` + padding + `"}]`))
	}))
	defer server.Close()

	client := NewMoviePilotClient(server.URL, "test-key", "")
	subscriptions, err := client.GetAllSubscriptions()
	if err != nil {
		t.Fatalf("GetAllSubscriptions returned error: %v", err)
	}
	if len(subscriptions) != 1 || subscriptions[0].ID != 1 {
		t.Fatalf("unexpected subscriptions: %#v", subscriptions)
	}
}

func TestMakeRequestRejectsResponseAboveGenericLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", maxResponseBodySize+1)))
	}))
	defer server.Close()

	client := NewMoviePilotClient(server.URL, "test-key", "")
	_, err := client.makeRequest(http.MethodGet, "/large", nil)
	if err == nil || !strings.Contains(err.Error(), "response body exceeds") {
		t.Fatalf("makeRequest error = %v, want explicit response limit error", err)
	}
}
