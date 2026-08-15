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
	if _, err := time.Parse(time.RFC3339, payload.Snapshot.UpdatedAt); err != nil {
		return nil, fmt.Errorf("monitor timestamp invalid: %w", err)
	}
	if payload.Snapshot.QBit.FreeSpaceOnDisk == nil {
		freeBytes, err := s.fetchMonitorQBitFreeSpace(r)
		if err != nil {
			return nil, err
		}
		payload.Snapshot.QBit.FreeSpaceOnDisk = &freeBytes
	} else if *payload.Snapshot.QBit.FreeSpaceOnDisk < 0 {
		return nil, fmt.Errorf("monitor free space invalid")
	}
	return &payload, nil
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
	if payload.ServerState == nil || payload.ServerState.FreeSpaceOnDisk == nil || *payload.ServerState.FreeSpaceOnDisk < 0 {
		return 0, fmt.Errorf("monitor free space invalid")
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
			FreeBytes: nonNegativePointer(snapshot.QBit.FreeSpaceOnDisk),
		},
		Queue: monitorQueue{
			ActiveTasks:   nonNegative(snapshot.QBit.ActiveTasks),
			StalledTasks:  nonNegative(snapshot.QBit.StalledTasks),
			ErrorTasks:    nonNegative(snapshot.QBit.ErrorTasks),
			TotalTasks:    nonNegative(snapshot.QBit.TotalTasks),
			DownloadSpeed: nonNegative(snapshot.QBit.DownloadSpeed),
			UploadSpeed:   nonNegative(snapshot.QBit.UploadSpeed),
		},
		Activity: monitorActivity{
			Downloads24H:       nonNegative(snapshot.MoviePilot.Downloads24H),
			Transfers24H:       nonNegative(snapshot.MoviePilot.Transfers24H),
			TransferSuccess24H: nonNegative(snapshot.MoviePilot.TransferSuccess24H),
			TransferFailed24H:  nonNegative(snapshot.MoviePilot.TransferFailed24H),
		},
		Pipeline: pipeline,
	}
}

func nonNegativePointer(value *int64) int64 {
	if value == nil {
		return 0
	}
	return nonNegative(*value)
}

func nonNegative(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
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
