package services

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestEmbyMediaAvailabilityContextCancelsUserDiscovery(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Users" {
			http.NotFound(w, r)
			return
		}
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()

	client := NewMoviePilotClient("", "", "")
	client.SetEmbyConfig(server.URL, "test-key")
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := client.EmbyMediaAvailabilityByTMDBContext(ctx, 101, MediaTypeMovie)
		result <- err
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("Emby user discovery did not start")
	}
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error=%v, want context.Canceled", err)
		}
	case <-time.After(250 * time.Millisecond):
		close(release)
		<-result
		t.Fatal("Emby user discovery ignored request cancellation")
	}
}

func TestEmbyMediaAvailabilityRequiresCompleteCount(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantExists bool
		wantErr    bool
	}{
		{name: "missing count", body: `{}`, wantErr: true},
		{name: "negative count", body: `{"TotalRecordCount":-1}`, wantErr: true},
		{name: "explicit zero", body: `{"TotalRecordCount":0}`},
		{name: "positive missing items", body: `{"TotalRecordCount":1}`, wantErr: true},
		{name: "positive missing item id", body: `{"TotalRecordCount":1,"Items":[{}]}`, wantErr: true},
		{name: "positive empty item id", body: `{"TotalRecordCount":1,"Items":[{"Id":""}]}`, wantErr: true},
		{name: "positive complete", body: `{"TotalRecordCount":1,"Items":[{"Id":"item-1"}]}`, wantExists: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			client := NewMoviePilotClient("", "", "")
			client.SetEmbyConfig(server.URL, "test-key")
			client.SetEmbyUserID("test-user")
			exists, err := client.EmbyMediaAvailabilityByTMDBContext(context.Background(), 101, MediaTypeMovie)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v, wantErr=%v", err, tc.wantErr)
			}
			if exists != tc.wantExists {
				t.Fatalf("exists=%v, want %v", exists, tc.wantExists)
			}
		})
	}
}
