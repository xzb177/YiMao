package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func checkAppHealth(cfg *smokeConfig) checkResult {
	return measured("app_health", func() (string, error) {
		status, raw, err := requestJSONWithRetry(cfg.requestTimeout, http.MethodGet, cfg.baseURL+"/health", nil, nil)
		if err != nil {
			return "", err
		}
		var response struct {
			Status       string            `json:"status"`
			Dependencies map[string]string `json:"dependencies"`
		}
		if err := json.Unmarshal(raw, &response); err != nil {
			return "", errors.New("invalid health JSON")
		}
		if status != http.StatusOK || response.Status != "ok" {
			return "", fmt.Errorf("health status=%d state=%s", status, response.Status)
		}
		if response.Dependencies["moviepilot"] != "ok" {
			return "", errors.New("MoviePilot dependency is not healthy")
		}
		return "service and MoviePilot dependency healthy", nil
	})
}

func checkAPIAuth(cfg *smokeConfig) checkResult {
	return measured("api_auth", func() (string, error) {
		status, _, err := requestJSONWithRetry(cfg.requestTimeout, http.MethodGet, cfg.baseURL+"/debug", nil, nil)
		if err != nil {
			return "", err
		}
		if status != http.StatusUnauthorized {
			return "", fmt.Errorf("missing API key returned %d, want 401", status)
		}
		status, raw, err := requestJSONWithRetry(cfg.requestTimeout, http.MethodGet, cfg.baseURL+"/debug", map[string]string{"X-API-Key": cfg.apiKey}, nil)
		if err != nil {
			return "", err
		}
		if status != http.StatusOK || !json.Valid(raw) {
			return "", fmt.Errorf("valid API key returned %d or invalid JSON", status)
		}
		return "unauthorized denied; authorized accepted", nil
	})
}

func checkTelegramIdentity(cfg *smokeConfig) checkResult {
	return measured("telegram_identity", func() (string, error) {
		status, raw, err := requestJSONWithRetry(cfg.requestTimeout, http.MethodGet, telegramEndpoint(cfg.telegramToken, "getMe"), nil, nil)
		if err != nil {
			return "", err
		}
		var response struct {
			OK     bool `json:"ok"`
			Result struct {
				Username string `json:"username"`
				IsBot    bool   `json:"is_bot"`
			} `json:"result"`
		}
		if status != http.StatusOK || json.Unmarshal(raw, &response) != nil || !response.OK || !response.Result.IsBot {
			return "", errors.New("Telegram getMe failed")
		}
		if !strings.EqualFold(response.Result.Username, cfg.expectedBot) {
			return "", errors.New("bot username does not match staging expectation")
		}
		return "isolated bot identity matched", nil
	})
}

func checkTelegramWebhook(cfg *smokeConfig) checkResult {
	return measured("telegram_polling_mode", func() (string, error) {
		status, raw, err := requestJSONWithRetry(cfg.requestTimeout, http.MethodGet, telegramEndpoint(cfg.telegramToken, "getWebhookInfo"), nil, nil)
		if err != nil {
			return "", err
		}
		var response struct {
			OK     bool `json:"ok"`
			Result struct {
				URL string `json:"url"`
			} `json:"result"`
		}
		if status != http.StatusOK || json.Unmarshal(raw, &response) != nil || !response.OK {
			return "", errors.New("Telegram getWebhookInfo failed")
		}
		if response.Result.URL != "" {
			return "", errors.New("staging bot still has a webhook configured")
		}
		return "webhook empty; polling mode active", nil
	})
}

func checkTelegramCommands(cfg *smokeConfig) checkResult {
	return measured("telegram_private_commands", func() (string, error) {
		payload := map[string]interface{}{"scope": map[string]string{"type": "all_private_chats"}}
		status, raw, err := requestJSONWithRetry(cfg.requestTimeout, http.MethodPost, telegramEndpoint(cfg.telegramToken, "getMyCommands"), nil, payload)
		if err != nil {
			return "", err
		}
		var response struct {
			OK     bool `json:"ok"`
			Result []struct {
				Command string `json:"command"`
			} `json:"result"`
		}
		if status != http.StatusOK || json.Unmarshal(raw, &response) != nil || !response.OK {
			return "", errors.New("Telegram getMyCommands failed")
		}
		got := make(map[string]bool)
		for _, command := range response.Result {
			got[command.Command] = true
		}
		for _, required := range []string{"start", "search", "requests", "help"} {
			if !got[required] {
				return "", fmt.Errorf("missing private command %s", required)
			}
		}
		return "private command menu registered", nil
	})
}

func checkMoviePilotReadOnly(cfg *smokeConfig) checkResult {
	return measured("moviepilot_read_only", func() (string, error) {
		endpoint := cfg.moviePilotURL + "/api/v1/system/setting/APP"
		status, raw, err := requestJSONWithRetry(cfg.requestTimeout, http.MethodGet, endpoint, map[string]string{"X-API-Key": cfg.moviePilotKey}, nil)
		if err != nil {
			return "", err
		}
		if status != http.StatusOK {
			return "", fmt.Errorf("MoviePilot authenticated read returned %d", status)
		}
		if len(raw) == 0 {
			return "", errors.New("MoviePilot returned an empty response")
		}
		return "authenticated read-only request succeeded", nil
	})
}

func checkTelegramSendDelete(cfg *smokeConfig) checkResult {
	if cfg.chatID == "" {
		return skipped("telegram_send_delete", "STAGING_SMOKE_CHAT_ID not configured")
	}
	return measured("telegram_send_delete", func() (string, error) {
		payload := map[string]interface{}{
			"chat_id":              cfg.chatID,
			"text":                 "🧪 YiMao staging smoke " + time.Now().UTC().Format(time.RFC3339),
			"disable_notification": true,
		}
		status, raw, err := requestJSON(cfg.requestTimeout, http.MethodPost, telegramEndpoint(cfg.telegramToken, "sendMessage"), nil, payload)
		if err != nil {
			return "", err
		}
		var response struct {
			OK     bool `json:"ok"`
			Result struct {
				MessageID int64 `json:"message_id"`
			} `json:"result"`
		}
		if status != http.StatusOK || json.Unmarshal(raw, &response) != nil || !response.OK || response.Result.MessageID == 0 {
			return "", errors.New("Telegram staging message failed")
		}
		deletePayload := map[string]interface{}{"chat_id": cfg.chatID, "message_id": response.Result.MessageID}
		deleteStatus, deleteRaw, deleteErr := requestJSON(cfg.requestTimeout, http.MethodPost, telegramEndpoint(cfg.telegramToken, "deleteMessage"), nil, deletePayload)
		if deleteErr != nil {
			return "", deleteErr
		}
		var deleted struct {
			OK     bool `json:"ok"`
			Result bool `json:"result"`
		}
		if deleteStatus != http.StatusOK || json.Unmarshal(deleteRaw, &deleted) != nil || !deleted.OK || !deleted.Result {
			return "", errors.New("smoke message could not be deleted")
		}
		return "test message sent silently and deleted", nil
	})
}
