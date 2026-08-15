package miniapp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	maxMonitorOverviewBody = 256 << 10
	maxMonitorSnapshotAge  = 3 * time.Minute
	maxMonitorFutureSkew   = 5 * time.Minute

	maxMonitorTaskCount     int64 = 10_000_000
	maxMonitorActivityCount int64 = 1_000_000_000
	maxMonitorBytesPerSec   int64 = 1 << 50
	maxMonitorFreeBytes     int64 = 1 << 62
)

type monitorOverviewPayload struct {
	Snapshot *monitorSnapshot `json:"snapshot"`
}

type monitorSnapshot struct {
	UpdatedAt  string              `json:"ts"`
	QBit       monitorQBitSnapshot `json:"qbit"`
	MoviePilot monitorServiceState `json:"moviepilot"`
	Emby       monitorServiceState `json:"emby"`
}

type monitorQBitSnapshot struct {
	Status           string `json:"status"`
	ConnectionStatus string `json:"connection_status"`
	FreeSpaceOnDisk  *int64 `json:"free_space_on_disk"`
	ActiveTasks      int64  `json:"active_tasks"`
	StalledTasks     int64  `json:"stalled_tasks"`
	ErrorTasks       int64  `json:"error_tasks"`
	TotalTasks       int64  `json:"total_tasks"`
	DownloadSpeed    int64  `json:"download_speed"`
	UploadSpeed      int64  `json:"upload_speed"`
}

type monitorServiceState struct {
	Status             string `json:"status"`
	Downloads24H       int64  `json:"downloads_24h"`
	Transfers24H       int64  `json:"transfers_24h"`
	TransferSuccess24H int64  `json:"transfer_success_24h"`
	TransferFailed24H  int64  `json:"transfer_failed_24h"`
}

type monitorResponse struct {
	UpdatedAt string            `json:"updated_at"`
	State     string            `json:"state"`
	Storage   monitorStorage    `json:"storage"`
	Queue     monitorQueue      `json:"queue"`
	Activity  monitorActivity   `json:"activity"`
	Pipeline  []monitorPipeline `json:"pipeline"`
}

type monitorStorage struct {
	FreeBytes int64 `json:"free_bytes"`
}

type monitorQueue struct {
	ActiveTasks   int64 `json:"active_tasks"`
	StalledTasks  int64 `json:"stalled_tasks"`
	ErrorTasks    int64 `json:"error_tasks"`
	TotalTasks    int64 `json:"total_tasks"`
	DownloadSpeed int64 `json:"download_speed"`
	UploadSpeed   int64 `json:"upload_speed"`
}

type monitorActivity struct {
	Downloads24H       int64 `json:"downloads_24h"`
	Transfers24H       int64 `json:"transfers_24h"`
	TransferSuccess24H int64 `json:"transfer_success_24h"`
	TransferFailed24H  int64 `json:"transfer_failed_24h"`
}

type monitorPipeline struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	State string `json:"state"`
}

func (s *Server) handleMonitor(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.auth(w, r); !ok {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")

	payload, err := s.fetchMonitorOverview(r)
	if err != nil {
		http.Error(w, "监控数据暂时不可用", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, projectMonitorOverview(payload.Snapshot, time.Now()))
}

func (s *Server) fetchMonitorOverview(r *http.Request) (*monitorOverviewPayload, error) {
	if s.deps.MonitorOverviewURL == "" || s.deps.HTTPClient == nil {
		return nil, fmt.Errorf("monitor unavailable")
	}
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, s.deps.MonitorOverviewURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := s.deps.HTTPClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("monitor status")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxMonitorOverviewBody+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxMonitorOverviewBody {
		return nil, fmt.Errorf("monitor body too large")
	}
	var payload monitorOverviewPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if payload.Snapshot == nil {
		return nil, fmt.Errorf("monitor snapshot missing")
	}
	if err := validateMonitorSnapshot(payload.Snapshot, time.Now()); err != nil {
		return nil, err
	}
	if payload.Snapshot.QBit.FreeSpaceOnDisk == nil {
		freeBytes, err := s.fetchMonitorQBitFreeSpace(r)
		if err != nil {
			return nil, err
		}
		payload.Snapshot.QBit.FreeSpaceOnDisk = &freeBytes
	}
	return &payload, nil
}

func validateMonitorSnapshot(snapshot *monitorSnapshot, now time.Time) error {
	updated, err := time.Parse(time.RFC3339, snapshot.UpdatedAt)
	if err != nil {
		return fmt.Errorf("monitor timestamp invalid: %w", err)
	}
	if updated.After(now.Add(maxMonitorFutureSkew)) {
		return fmt.Errorf("monitor timestamp invalid")
	}

	qbitValues := []struct {
		name  string
		value int64
		max   int64
	}{
		{name: "active tasks", value: snapshot.QBit.ActiveTasks, max: maxMonitorTaskCount},
		{name: "stalled tasks", value: snapshot.QBit.StalledTasks, max: maxMonitorTaskCount},
		{name: "error tasks", value: snapshot.QBit.ErrorTasks, max: maxMonitorTaskCount},
		{name: "total tasks", value: snapshot.QBit.TotalTasks, max: maxMonitorTaskCount},
		{name: "download speed", value: snapshot.QBit.DownloadSpeed, max: maxMonitorBytesPerSec},
		{name: "upload speed", value: snapshot.QBit.UploadSpeed, max: maxMonitorBytesPerSec},
	}
	for _, value := range qbitValues {
		if err := validateMonitorAggregate(value.name, value.value, value.max); err != nil {
			return err
		}
	}

	activityValues := []struct {
		name  string
		value int64
	}{
		{name: "downloads 24h", value: snapshot.MoviePilot.Downloads24H},
		{name: "transfers 24h", value: snapshot.MoviePilot.Transfers24H},
		{name: "transfer success 24h", value: snapshot.MoviePilot.TransferSuccess24H},
		{name: "transfer failed 24h", value: snapshot.MoviePilot.TransferFailed24H},
	}
	for _, value := range activityValues {
		if err := validateMonitorAggregate(value.name, value.value, maxMonitorActivityCount); err != nil {
			return err
		}
	}
	if snapshot.QBit.FreeSpaceOnDisk != nil {
		if err := validateMonitorAggregate("free space", *snapshot.QBit.FreeSpaceOnDisk, maxMonitorFreeBytes); err != nil {
			return err
		}
	}
	return nil
}

func validateMonitorAggregate(name string, value, max int64) error {
	if value < 0 || value > max {
		return fmt.Errorf("monitor %s invalid", name)
	}
	return nil
}

func (s *Server) fetchMonitorQBitFreeSpace(r *http.Request) (int64, error) {
	if s.deps.MonitorQBitMainDataURL == "" || s.deps.HTTPClient == nil {
		return 0, fmt.Errorf("monitor unavailable")
	}
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, s.deps.MonitorQBitMainDataURL, nil)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := s.deps.HTTPClient.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return 0, fmt.Errorf("monitor status")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxMonitorOverviewBody+1))
	if err != nil {
		return 0, err
	}
	if len(body) > maxMonitorOverviewBody {
		return 0, fmt.Errorf("monitor body too large")
	}
	var payload struct {
		ServerState *struct {
			FreeSpaceOnDisk *int64 `json:"free_space_on_disk"`
		} `json:"server_state"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0, err
	}
	if payload.ServerState == nil || payload.ServerState.FreeSpaceOnDisk == nil {
		return 0, fmt.Errorf("monitor free space invalid")
	}
	if err := validateMonitorAggregate("free space", *payload.ServerState.FreeSpaceOnDisk, maxMonitorFreeBytes); err != nil {
		return 0, err
	}
	return *payload.ServerState.FreeSpaceOnDisk, nil
}

func projectMonitorOverview(snapshot *monitorSnapshot, now time.Time) monitorResponse {
	if snapshot == nil {
		snapshot = &monitorSnapshot{}
	}
	pipeline := []monitorPipeline{
		{Key: "download", Label: "下载", State: downloadPipelineState(snapshot.QBit)},
		{Key: "organize", Label: "整理", State: servicePipelineState(snapshot.MoviePilot.Status)},
		{Key: "library", Label: "媒体库", State: servicePipelineState(snapshot.Emby.Status)},
	}
	return monitorResponse{
		UpdatedAt: snapshot.UpdatedAt,
		State:     overallMonitorState(snapshot.UpdatedAt, pipeline, now),
		Storage: monitorStorage{
			FreeBytes: monitorPointerValue(snapshot.QBit.FreeSpaceOnDisk),
		},
		Queue: monitorQueue{
			ActiveTasks:   snapshot.QBit.ActiveTasks,
			StalledTasks:  snapshot.QBit.StalledTasks,
			ErrorTasks:    snapshot.QBit.ErrorTasks,
			TotalTasks:    snapshot.QBit.TotalTasks,
			DownloadSpeed: snapshot.QBit.DownloadSpeed,
			UploadSpeed:   snapshot.QBit.UploadSpeed,
		},
		Activity: monitorActivity{
			Downloads24H:       snapshot.MoviePilot.Downloads24H,
			Transfers24H:       snapshot.MoviePilot.Transfers24H,
			TransferSuccess24H: snapshot.MoviePilot.TransferSuccess24H,
			TransferFailed24H:  snapshot.MoviePilot.TransferFailed24H,
		},
		Pipeline: pipeline,
	}
}

func monitorPointerValue(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func downloadPipelineState(qbit monitorQBitSnapshot) string {
	if qbit.Status == "" || qbit.Status == "unknown" || qbit.ConnectionStatus == "" || qbit.ConnectionStatus == "unknown" {
		return "unknown"
	}
	if qbit.Status == "ok" && qbit.ConnectionStatus == "connected" {
		return "ok"
	}
	return "degraded"
}

func servicePipelineState(status string) string {
	switch status {
	case "ok":
		return "ok"
	case "", "unknown":
		return "unknown"
	default:
		return "degraded"
	}
}

func overallMonitorState(updatedAt string, pipeline []monitorPipeline, now time.Time) string {
	updated, err := time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return "unknown"
	}
	if updated.After(now.Add(maxMonitorFutureSkew)) {
		return "unknown"
	}
	if now.Sub(updated) > maxMonitorSnapshotAge {
		return "stale"
	}
	okCount, unknownCount := 0, 0
	for _, stage := range pipeline {
		switch stage.State {
		case "ok":
			okCount++
		case "unknown":
			unknownCount++
		}
	}
	if okCount == len(pipeline) {
		return "ok"
	}
	if unknownCount == len(pipeline) {
		return "unknown"
	}
	return "degraded"
}
