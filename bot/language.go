package bot

import (
	"log"
	"sync"
)

// LanguageBridge provides bridge functions for the language system
type LanguageBridge struct {
	// Function pointers (will be set by main package)
	tFunc                func(userID int64, key string, args ...interface{}) string
	setUserLangFunc      func(userID int64, lang string)
	getUserLangFunc      func(userID int64) string
	getSupportedLangsFunc func() map[string]string

	mu sync.RWMutex
}

var languageBridge *LanguageBridge

// InitLanguageBridge initializes the language bridge
func InitLanguageBridge() {
	languageBridge = &LanguageBridge{}
	log.Println("[LanguageBridge] Initialized")
}

// SetTFunc sets the translate function
func SetTFunc(fn func(userID int64, key string, args ...interface{}) string) {
	if languageBridge != nil {
		languageBridge.mu.Lock()
		languageBridge.tFunc = fn
		languageBridge.mu.Unlock()
	}
}

// SetSetUserLangFunc sets the set user language function
func SetSetUserLangFunc(fn func(userID int64, lang string)) {
	if languageBridge != nil {
		languageBridge.mu.Lock()
		languageBridge.setUserLangFunc = fn
		languageBridge.mu.Unlock()
	}
}

// SetGetUserLangFunc sets the get user language function
func SetGetUserLangFunc(fn func(userID int64) string) {
	if languageBridge != nil {
		languageBridge.mu.Lock()
		languageBridge.getUserLangFunc = fn
		languageBridge.mu.Unlock()
	}
}

// SetGetSupportedLangsFunc sets the get supported languages function
func SetGetSupportedLangsFunc(fn func() map[string]string) {
	if languageBridge != nil {
		languageBridge.mu.Lock()
		languageBridge.getSupportedLangsFunc = fn
		languageBridge.mu.Unlock()
	}
}

// Bridge methods

// T returns translated text for a user
func T(userID int64, key string, args ...interface{}) string {
	if languageBridge == nil || languageBridge.tFunc == nil {
		return key // Fallback to key if system not initialized
	}
	languageBridge.mu.RLock()
	defer languageBridge.mu.RUnlock()
	return languageBridge.tFunc(userID, key, args...)
}

// SetUserLanguage sets user's preferred language
func SetUserLanguage(userID int64, lang string) {
	if languageBridge == nil || languageBridge.setUserLangFunc == nil {
		return
	}
	languageBridge.mu.RLock()
	defer languageBridge.mu.RUnlock()
	languageBridge.setUserLangFunc(userID, lang)
}

// GetUserLanguage gets user's preferred language
func GetUserLanguage(userID int64) string {
	if languageBridge == nil || languageBridge.getUserLangFunc == nil {
		return "zh" // Default to Chinese
	}
	languageBridge.mu.RLock()
	defer languageBridge.mu.RUnlock()
	return languageBridge.getUserLangFunc(userID)
}

// GetSupportedLanguages gets list of supported languages
func GetSupportedLanguages() map[string]string {
	if languageBridge == nil || languageBridge.getSupportedLangsFunc == nil {
		return map[string]string{"zh": "简体中文", "en": "English"}
	}
	languageBridge.mu.RLock()
	defer languageBridge.mu.RUnlock()
	return languageBridge.getSupportedLangsFunc()
}
