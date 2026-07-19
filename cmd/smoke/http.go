package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxSmokeResponseBytes = 1 << 20

func requireHTTPServiceURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return errors.New("MoviePilot URL must be HTTP(S) without embedded credentials")
	}
	return nil
}

func requireLoopbackURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil {
		return errors.New("STAGING_BASE_URL must be a plain local HTTP URL")
	}
	switch strings.ToLower(parsed.Hostname()) {
	case "127.0.0.1", "localhost", "::1":
		return nil
	default:
		return errors.New("STAGING_BASE_URL must resolve to loopback")
	}
}

func requestJSON(timeout time.Duration, method, endpoint string, headers map[string]string, payload interface{}) (status int, raw []byte, err error) {
	var body io.Reader
	if payload != nil {
		encoded, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return 0, nil, marshalErr
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		return 0, nil, errors.New("create request")
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, errors.New("request failed")
	}
	defer resp.Body.Close()
	raw, err = io.ReadAll(io.LimitReader(resp.Body, maxSmokeResponseBytes))
	if err != nil {
		return resp.StatusCode, nil, errors.New("read response")
	}
	return resp.StatusCode, raw, nil
}

func requestJSONWithRetry(timeout time.Duration, method, endpoint string, headers map[string]string, payload interface{}) (status int, raw []byte, err error) {
	for attempt := 0; attempt < 3; attempt++ {
		status, raw, err = requestJSON(timeout, method, endpoint, headers, payload)
		if err == nil && status < http.StatusInternalServerError {
			return status, raw, nil
		}
		if attempt < 2 {
			time.Sleep(time.Duration(200*(1<<attempt)) * time.Millisecond)
		}
	}
	return status, raw, err
}

func telegramEndpoint(token, method string) string {
	return "https://api.telegram.org/bot" + token + "/" + method
}

func measured(name string, fn func() (string, error)) checkResult {
	started := time.Now()
	detail, err := fn()
	result := checkResult{Name: name, Status: "pass", Detail: detail, DurationMS: time.Since(started).Milliseconds()}
	if err != nil {
		result.Status = "fail"
		result.Detail = err.Error()
	}
	return result
}

func skipped(name, detail string) checkResult {
	return checkResult{Name: name, Status: "skip", Detail: detail}
}
