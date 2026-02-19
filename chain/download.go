package chain

import (
	"fmt"
	"log"
	"strconv"
)

// DownloadChain handles download operations
type DownloadChain struct {
	*ChainBase
}

// NewDownloadChain creates a new download chain
func NewDownloadChain(jellyseerrURL, apiKey, qbURL, username, password string) *DownloadChain {
	return &DownloadChain{
		ChainBase: NewChainBase(jellyseerrURL, apiKey),
	}
}

// TorrentFile represents a torrent file
type TorrentFile struct {
	URL     string `json:"url"`
	Magnet  string `json:"magnet"`
	Hash    string `json:"hash"`
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Seasons []int  `json:"seasons"`
}

// DownloadResult represents the result of a download operation
type DownloadResult struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	TorrentID string `json:"torrentId"`
}

// DownloadFromURL downloads a torrent from URL
func (d *DownloadChain) DownloadFromURL(torrentURL string) (*DownloadResult, error) {
	// Send to Jellyseerr for download
	endpoint := "/api/v1/download/push"

	payload := map[string]string{
		"magnet":   torrentURL,
		"source":   "telegram-bot",
	}

	var result map[string]interface{}
	err := d.postJellyseerrRequest(endpoint, payload, &result)
	if err != nil {
		return &DownloadResult{
			Success: false,
			Message: fmt.Sprintf("Download failed: %v", err),
		}, err
	}

	log.Printf("[DownloadChain] Download initiated: URL=%s", torrentURL)

	return &DownloadResult{
		Success:   true,
		Message:   "Download started",
	}, nil
}

// SendToJellyseerr sends a media request to Jellyseerr
func (d *DownloadChain) SendToJellyseerr(mediaID int, mediaType string) (*DownloadResult, error) {
	// This uses the SubscribeChain's Subscribe method
	subChain := &SubscribeChain{ChainBase: d.ChainBase}

	request, err := subChain.Subscribe(mediaID, mediaType, nil)
	if err != nil {
		return &DownloadResult{
			Success: false,
			Message: err.Error(),
		}, err
	}

	return &DownloadResult{
		Success:   true,
		Message:   "Request created successfully",
		TorrentID: strconv.Itoa(request.ID),
	}, nil
}

// GetTorrentStatus checks the status of a torrent
func (d *DownloadChain) GetTorrentStatus(hash string) (map[string]interface{}, error) {
	// This would interact with qBittorrent WebAPI
	// For now, return a placeholder
	return map[string]interface{}{
		"state": "downloading",
		"progress": 50.0,
	}, nil
}
