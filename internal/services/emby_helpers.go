package services

import "net/http"

// embydoGet sends a GET request to Emby with API key in X-Emby-Token header.
// The path parameter is the URL path after embyURL (e.g., "/Users?IsDisabled=false").
func embydoGet(client *http.Client, embyURL, embyAPIKey, path string) (*http.Response, error) {
	url := embyURL + path
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Emby-Token", embyAPIKey)
	return client.Do(req)
}
