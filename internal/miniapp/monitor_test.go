package miniapp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const monitorPath = "/api/miniapp/v1/monitor"

type monitorRoundTripperFunc func(*http.Request) (*http.Response, error)

func (fn monitorRoundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestMonitorRequiresTelegramInitData(t *testing.T) {
	handler := NewServer(Deps{BotToken: miniAppTestToken}).Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, monitorPath, nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestMonitorAllowsOnlyGET(t *testing.T) {
	handler := NewServer(Deps{BotToken: miniAppTestToken}).Handler()
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, signedRequest(t, method, monitorPath, `{}`, 101))
			if response.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestMonitorProjectsOnlyPublicOverviewFields(t *testing.T) {
	updatedAt := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/overview" {
			t.Errorf("upstream request=%s %s", r.Method, r.URL.Path)
		}
		assertNoSensitiveMonitorHeaders(t, r)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"snapshot": map[string]any{
				"ts": updatedAt,
				"qbit": map[string]any{
					"status": "ok", "connection_status": "connected", "free_space_on_disk": 987654321,
					"active_tasks": 4, "stalled_tasks": 1, "error_tasks": 2, "total_tasks": 9,
					"download_speed": 345678, "upload_speed": 12345, "api_latency_ms": 7,
					"raw_response": "must-not-leak",
				},
				"moviepilot": map[string]any{
					"status": "ok", "downloads_24h": 8, "transfers_24h": 7,
					"transfer_success_24h": 6, "transfer_failed_24h": 1,
					"downloadhistory_total": 999, "internal_url": "http://private.invalid",
				},
				"emby":       map[string]any{"status": "ok", "session_count": 5},
				"hermes":     map[string]any{"status": "ok", "cron_active": 3},
				"containers": map[string]any{"qbittorrent": map[string]any{"restart_count": 12}},
				"collector":  map[string]any{"status": "ok", "error": "internal_error_category"},
			},
			"alerts": []map[string]any{{"message": "sensitive alert text", "key": "private.key"}},
		})
	}))
	defer upstream.Close()

	handler := NewServer(Deps{
		BotToken:           miniAppTestToken,
		MonitorOverviewURL: upstream.URL + "/overview",
		HTTPClient:         upstream.Client(),
	}).Handler()
	response := httptest.NewRecorder()
	request := signedRequest(t, http.MethodGet, monitorPath, "", 101)
	request.Header.Set("Authorization", "Bearer miniapp-secret")
	request.Header.Set("Cookie", "session=miniapp-secret")
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	want := map[string]any{
		"updated_at": updatedAt,
		"state":      "ok",
		"storage":    map[string]any{"free_bytes": 987654321},
		"queue": map[string]any{
			"active_tasks": 4, "stalled_tasks": 1, "error_tasks": 2, "total_tasks": 9,
			"download_speed": 345678, "upload_speed": 12345,
		},
		"activity": map[string]any{
			"downloads_24h": 8, "transfers_24h": 7,
			"transfer_success_24h": 6, "transfer_failed_24h": 1,
		},
		"pipeline": []any{
			map[string]any{"key": "download", "label": "下载", "state": "ok"},
			map[string]any{"key": "organize", "label": "整理", "state": "ok"},
			map[string]any{"key": "library", "label": "媒体库", "state": "ok"},
		},
	}
	var got, normalizedWant any
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid response JSON: %v", err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(wantJSON, &normalizedWant); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, normalizedWant) {
		t.Fatalf("public projection mismatch\n got: %s\nwant: %s", response.Body.String(), wantJSON)
	}
}

func TestMonitorFallsBackToQBitMainDataWhenOverviewOmitsFreeSpace(t *testing.T) {
	for _, freeBytes := range []int64{987654321, 0} {
		t.Run(fmt.Sprint(freeBytes), func(t *testing.T) {
			updatedAt := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
			overview := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/overview" {
					t.Errorf("overview request=%s %s", r.Method, r.URL.Path)
				}
				assertNoSensitiveMonitorHeaders(t, r)
				_, _ = fmt.Fprintf(w, `{"snapshot":{"ts":%q,"qbit":{"status":"ok","connection_status":"connected"},"moviepilot":{"status":"ok"},"emby":{"status":"ok"}}}`, updatedAt)
			}))
			defer overview.Close()

			qbit := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/v2/sync/maindata" {
					t.Errorf("qbit request=%s %s", r.Method, r.URL.Path)
				}
				assertNoSensitiveMonitorHeaders(t, r)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"server_state": map[string]any{
						"free_space_on_disk": freeBytes,
						"connection_status":  "must-not-project",
					},
					"torrents": map[string]any{"private-hash": map[string]any{"name": "must-not-project"}},
				})
			}))
			defer qbit.Close()

			deps := Deps{
				BotToken:           miniAppTestToken,
				MonitorOverviewURL: overview.URL + "/overview",
				HTTPClient:         overview.Client(),
			}
			setMonitorQBitMainDataURL(t, &deps, qbit.URL+"/api/v2/sync/maindata")
			handler := NewServer(deps).Handler()
			response := httptest.NewRecorder()
			request := signedRequest(t, http.MethodGet, monitorPath, "", 101)
			request.Header.Set("Authorization", "Bearer miniapp-secret")
			request.Header.Set("Cookie", "session=miniapp-secret")
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			var body monitorResponse
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Storage.FreeBytes != freeBytes {
				t.Fatalf("free_bytes=%d want %d", body.Storage.FreeBytes, freeBytes)
			}
			if strings.Contains(response.Body.String(), "private-hash") || strings.Contains(response.Body.String(), "must-not-project") {
				t.Fatalf("qBit private data leaked: %s", response.Body.String())
			}
		})
	}
}

func TestMonitorOverviewFreeSpaceTakesPriorityWithoutFallbackRequest(t *testing.T) {
	updatedAt := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	overview := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertNoSensitiveMonitorHeaders(t, r)
		_, _ = fmt.Fprintf(w, `{"snapshot":{"ts":%q,"qbit":{"status":"ok","connection_status":"connected","free_space_on_disk":0},"moviepilot":{"status":"ok"},"emby":{"status":"ok"}}}`, updatedAt)
	}))
	defer overview.Close()
	var fallbackHits atomic.Int32
	qbit := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackHits.Add(1)
		http.Error(w, "must not be requested", http.StatusInternalServerError)
	}))
	defer qbit.Close()

	deps := Deps{BotToken: miniAppTestToken, MonitorOverviewURL: overview.URL, HTTPClient: overview.Client()}
	setMonitorQBitMainDataURL(t, &deps, qbit.URL+"/api/v2/sync/maindata")
	response := httptest.NewRecorder()
	NewServer(deps).Handler().ServeHTTP(response, signedRequest(t, http.MethodGet, monitorPath, "", 101))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if fallbackHits.Load() != 0 {
		t.Fatalf("fallback hits=%d want 0", fallbackHits.Load())
	}
	var body monitorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Storage.FreeBytes != 0 {
		t.Fatalf("free_bytes=%d want true zero", body.Storage.FreeBytes)
	}
}

func TestMonitorQBitFallbackFailuresAreGeneric503(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		delay      time.Duration
		missingURL bool
	}{
		{name: "missing-url", missingURL: true},
		{name: "non-2xx", status: http.StatusBadGateway, body: `{"upstream":"qbit secret"}`},
		{name: "timeout", status: http.StatusOK, body: `{"server_state":{"free_space_on_disk":1}}`, delay: 100 * time.Millisecond},
		{name: "oversized", status: http.StatusOK, body: strings.Repeat("x", maxMonitorOverviewBody+1)},
		{name: "invalid-json", status: http.StatusOK, body: `{qbit-secret`},
		{name: "missing-server-state", status: http.StatusOK, body: `{}`},
		{name: "missing-value", status: http.StatusOK, body: `{"server_state":{}}`},
		{name: "null", status: http.StatusOK, body: `{"server_state":{"free_space_on_disk":null}}`},
		{name: "negative", status: http.StatusOK, body: `{"server_state":{"free_space_on_disk":-1}}`},
		{name: "excessive", status: http.StatusOK, body: `{"server_state":{"free_space_on_disk":4611686018427387905}}`},
		{name: "fractional", status: http.StatusOK, body: `{"server_state":{"free_space_on_disk":1.5}}`},
		{name: "string", status: http.StatusOK, body: `{"server_state":{"free_space_on_disk":"1"}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updatedAt := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
			overviewBody := fmt.Sprintf(`{"snapshot":{"ts":%q,"qbit":{"status":"ok","connection_status":"connected"},"moviepilot":{"status":"ok"},"emby":{"status":"ok"}}}`, updatedAt)
			overview := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assertNoSensitiveMonitorHeaders(t, r)
				_, _ = w.Write([]byte(overviewBody))
			}))
			defer overview.Close()

			var qbit *httptest.Server
			var qbitHits atomic.Int32
			qbitURL := ""
			if !tt.missingURL {
				qbit = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					qbitHits.Add(1)
					assertNoSensitiveMonitorHeaders(t, r)
					if tt.delay > 0 {
						time.Sleep(tt.delay)
					}
					w.WriteHeader(tt.status)
					_, _ = w.Write([]byte(tt.body))
				}))
				defer qbit.Close()
				qbitURL = qbit.URL + "/api/v2/sync/maindata?private=qbit-url-secret"
			}

			client := overview.Client()
			if tt.name == "timeout" {
				overviewHost := strings.TrimPrefix(overview.URL, "http://")
				client = &http.Client{
					Timeout: 10 * time.Millisecond,
					Transport: monitorRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
						assertNoSensitiveMonitorHeaders(t, request)
						if request.URL.Host == overviewHost {
							return &http.Response{
								StatusCode: http.StatusOK,
								Header:     make(http.Header),
								Body:       io.NopCloser(strings.NewReader(overviewBody)),
								Request:    request,
							}, nil
						}
						qbitHits.Add(1)
						<-request.Context().Done()
						return nil, request.Context().Err()
					}),
				}
			}
			deps := Deps{BotToken: miniAppTestToken, MonitorOverviewURL: overview.URL, HTTPClient: client}
			setMonitorQBitMainDataURL(t, &deps, qbitURL)
			response := httptest.NewRecorder()
			NewServer(deps).Handler().ServeHTTP(response, signedRequest(t, http.MethodGet, monitorPath, "", 101))
			if response.Code != http.StatusServiceUnavailable {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if response.Body.String() != "监控数据暂时不可用\n" {
				t.Fatalf("non-generic error body=%q", response.Body.String())
			}
			for _, secret := range []string{"qbit secret", "qbit-secret", "qbit-url-secret", qbitURL} {
				if secret != "" && strings.Contains(response.Body.String(), secret) {
					t.Fatalf("error leaked %q: %s", secret, response.Body.String())
				}
			}
			wantHits := int32(1)
			if tt.missingURL {
				wantHits = 0
			}
			if qbitHits.Load() != wantHits {
				t.Fatalf("qbit hits=%d want %d", qbitHits.Load(), wantHits)
			}
		})
	}
}

func TestMonitorRejectsFutureTimestampsAndInvalidAggregates(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	tests := []struct {
		name   string
		mutate func(*monitorSnapshot)
	}{
		{name: "future-timestamp", mutate: func(snapshot *monitorSnapshot) { snapshot.UpdatedAt = now.Add(6 * time.Minute).Format(time.RFC3339) }},
		{name: "negative-free-space", mutate: func(snapshot *monitorSnapshot) { value := int64(-1); snapshot.QBit.FreeSpaceOnDisk = &value }},
		{name: "negative-active-tasks", mutate: func(snapshot *monitorSnapshot) { snapshot.QBit.ActiveTasks = -1 }},
		{name: "negative-stalled-tasks", mutate: func(snapshot *monitorSnapshot) { snapshot.QBit.StalledTasks = -1 }},
		{name: "negative-error-tasks", mutate: func(snapshot *monitorSnapshot) { snapshot.QBit.ErrorTasks = -1 }},
		{name: "negative-total-tasks", mutate: func(snapshot *monitorSnapshot) { snapshot.QBit.TotalTasks = -1 }},
		{name: "negative-download-speed", mutate: func(snapshot *monitorSnapshot) { snapshot.QBit.DownloadSpeed = -1 }},
		{name: "negative-upload-speed", mutate: func(snapshot *monitorSnapshot) { snapshot.QBit.UploadSpeed = -1 }},
		{name: "negative-downloads-24h", mutate: func(snapshot *monitorSnapshot) { snapshot.MoviePilot.Downloads24H = -1 }},
		{name: "negative-transfers-24h", mutate: func(snapshot *monitorSnapshot) { snapshot.MoviePilot.Transfers24H = -1 }},
		{name: "negative-transfer-success-24h", mutate: func(snapshot *monitorSnapshot) { snapshot.MoviePilot.TransferSuccess24H = -1 }},
		{name: "negative-transfer-failed-24h", mutate: func(snapshot *monitorSnapshot) { snapshot.MoviePilot.TransferFailed24H = -1 }},
		{name: "excessive-task-count", mutate: func(snapshot *monitorSnapshot) { snapshot.QBit.TotalTasks = 10_000_001 }},
		{name: "excessive-speed", mutate: func(snapshot *monitorSnapshot) { snapshot.QBit.DownloadSpeed = (1 << 50) + 1 }},
		{name: "excessive-activity-count", mutate: func(snapshot *monitorSnapshot) { snapshot.MoviePilot.Downloads24H = 1_000_000_001 }},
		{name: "excessive-free-space", mutate: func(snapshot *monitorSnapshot) { value := int64((1 << 62) + 1); snapshot.QBit.FreeSpaceOnDisk = &value }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			freeBytes := int64(0)
			snapshot := monitorSnapshot{
				UpdatedAt: now.Format(time.RFC3339),
				QBit: monitorQBitSnapshot{
					Status: "ok", ConnectionStatus: "connected", FreeSpaceOnDisk: &freeBytes,
				},
				MoviePilot: monitorServiceState{Status: "ok"},
				Emby:       monitorServiceState{Status: "ok"},
			}
			tt.mutate(&snapshot)
			payload, err := json.Marshal(monitorOverviewPayload{Snapshot: &snapshot})
			if err != nil {
				t.Fatal(err)
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(payload)
			}))
			defer upstream.Close()

			response := httptest.NewRecorder()
			NewServer(Deps{
				BotToken: miniAppTestToken, MonitorOverviewURL: upstream.URL, HTTPClient: upstream.Client(),
			}).Handler().ServeHTTP(response, signedRequest(t, http.MethodGet, monitorPath, "", 101))
			if response.Code != http.StatusServiceUnavailable {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if response.Body.String() != "监控数据暂时不可用\n" {
				t.Fatalf("non-generic error body=%q", response.Body.String())
			}
		})
	}
}

func TestValidateMonitorSnapshotAcceptsBoundaryValues(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	freeBytes := maxMonitorFreeBytes
	snapshot := &monitorSnapshot{
		UpdatedAt: now.Add(maxMonitorFutureSkew).Format(time.RFC3339),
		QBit: monitorQBitSnapshot{
			Status:           "ok",
			ConnectionStatus: "connected",
			FreeSpaceOnDisk:  &freeBytes,
			ActiveTasks:      maxMonitorTaskCount,
			StalledTasks:     maxMonitorTaskCount,
			ErrorTasks:       maxMonitorTaskCount,
			TotalTasks:       maxMonitorTaskCount,
			DownloadSpeed:    maxMonitorBytesPerSec,
			UploadSpeed:      maxMonitorBytesPerSec,
		},
		MoviePilot: monitorServiceState{
			Status:             "ok",
			Downloads24H:       maxMonitorActivityCount,
			Transfers24H:       maxMonitorActivityCount,
			TransferSuccess24H: maxMonitorActivityCount,
			TransferFailed24H:  maxMonitorActivityCount,
		},
		Emby: monitorServiceState{Status: "ok"},
	}
	if err := validateMonitorSnapshot(snapshot, now); err != nil {
		t.Fatalf("boundary snapshot rejected: %v", err)
	}
}

func TestMonitorUpstreamFailuresAreGeneric503(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		delay  time.Duration
	}{
		{name: "non-2xx", status: http.StatusBadGateway, body: `{"internal":"upstream secret"}`},
		{name: "timeout", status: http.StatusOK, body: `{}`, delay: 100 * time.Millisecond},
		{name: "oversized", status: http.StatusOK, body: strings.Repeat("x", 256<<10+1)},
		{name: "invalid-json", status: http.StatusOK, body: `{not-json`},
		{name: "missing-snapshot", status: http.StatusOK, body: `{}`},
		{name: "missing-timestamp", status: http.StatusOK, body: `{"snapshot":{}}`},
		{name: "invalid-timestamp", status: http.StatusOK, body: `{"snapshot":{"ts":"not-a-time"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				if tc.delay > 0 {
					time.Sleep(tc.delay)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer upstream.Close()
			client := upstream.Client()
			if tc.name == "timeout" {
				client.Timeout = 10 * time.Millisecond
			}
			handler := NewServer(Deps{
				BotToken: miniAppTestToken, MonitorOverviewURL: upstream.URL, HTTPClient: client,
			}).Handler()
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, signedRequest(t, http.MethodGet, monitorPath, "", 101))
			gotHits := hits.Load()
			if tc.name == "timeout" {
				if gotHits > 1 {
					t.Fatalf("upstream hits=%d want at most 1", gotHits)
				}
			} else if gotHits != 1 {
				t.Fatalf("upstream hits=%d want 1", gotHits)
			}
			if response.Code != http.StatusServiceUnavailable {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if got := response.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control=%q want no-store", got)
			}
			if response.Body.String() != "监控数据暂时不可用\n" {
				t.Fatalf("non-generic error body=%q", response.Body.String())
			}
			for _, secret := range []string{"upstream secret", "internal", "not-json"} {
				if strings.Contains(response.Body.String(), secret) {
					t.Fatalf("error leaked %q: %s", secret, response.Body.String())
				}
			}
		})
	}
}

func TestMonitorMarksOldSnapshotStale(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"snapshot":{"ts":"2020-01-01T00:00:00Z","qbit":{"status":"ok","connection_status":"connected","free_space_on_disk":0},"moviepilot":{"status":"ok"},"emby":{"status":"ok"}}}`))
	}))
	defer upstream.Close()
	handler := NewServer(Deps{
		BotToken: miniAppTestToken, MonitorOverviewURL: upstream.URL, HTTPClient: upstream.Client(),
	}).Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, signedRequest(t, http.MethodGet, monitorPath, "", 101))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.State != "stale" {
		t.Fatalf("state=%q want stale", body.State)
	}
}

func assertNoSensitiveMonitorHeaders(t *testing.T, r *http.Request) {
	t.Helper()
	for _, name := range []string{"X-Telegram-Init-Data", "Authorization", "Cookie"} {
		if value := r.Header.Get(name); value != "" {
			t.Errorf("sensitive header %s was forwarded: %q", name, value)
		}
	}
}

func setMonitorQBitMainDataURL(t *testing.T, deps *Deps, value string) {
	t.Helper()
	field := reflect.ValueOf(deps).Elem().FieldByName("MonitorQBitMainDataURL")
	if !field.IsValid() {
		t.Fatal("Deps.MonitorQBitMainDataURL is missing")
	}
	field.SetString(value)
}
