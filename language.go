package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
)

// LanguageSystem handles multi-language support
type LanguageSystem struct {
	supportedLanguages map[string]string    // code -> name
	translations      map[string]map[string]string // code -> key -> text
	userLanguages    map[int64]string           // userID -> language code
	mutex            sync.RWMutex
	storageFile      string
}

var languageSys *LanguageSystem

// Language codes
const (
	LangZH = "zh" // Chinese (Simplified)
	LangEN = "en" // English
)

// Translation keys
const (
	KeyWelcome           = "welcome"
	KeyHelp              = "help"
	KeySearchPlaceholder = "search_placeholder"
	KeyNoResults        = "no_results"
	KeyRequestSuccess    = "request_success"
	KeyRequestFailed     = "request_failed"
	KeyBindRequired      = "bind_required"
	KeyAdminOnly         = "admin_only"
)

// InitLanguageSystem initializes the language system
func InitLanguageSystem() {
	translations := map[string]map[string]string{
		LangZH: {
			KeyWelcome:           "👋 欢迎使用云海看板娘！",
			KeyHelp:              "📖 *使用帮助*",
			KeySearchPlaceholder: "🔍 输入内容名搜索...",
			KeyNoResults:        "未找到相关内容",
			KeyRequestSuccess:    "✅ 请求已发送",
			KeyRequestFailed:     "❌ 请求失败",
			KeyBindRequired:      "请先绑定账号",
			KeyAdminOnly:         "❌ 仅管理员可用",
		},
		LangEN: {
			KeyWelcome:           "👋 Welcome to Cloud Sea Bot!",
			KeyHelp:              "📖 *Help*",
			KeySearchPlaceholder: "🔍 Search content...",
			KeyNoResults:        "No results found",
			KeyRequestSuccess:    "✅ Request sent",
			KeyRequestFailed:     "❌ Request failed",
			KeyBindRequired:      "Please bind your account first",
			KeyAdminOnly:         "❌ Admin only",
		},
	}

	languageSys = &LanguageSystem{
		supportedLanguages: map[string]string{
			LangZH: "简体中文",
			LangEN: "English",
		},
		translations:      translations,
		userLanguages:    make(map[int64]string),
		storageFile:      "user_languages.json",
	}

	// Load existing data
	languageSys.load()

	log.Println("LanguageSystem initialized")
}

// T returns translated text for a key
func (l *LanguageSystem) T(userID int64, key string, args ...interface{}) string {
	l.mutex.RLock()
	defer l.mutex.RUnlock()

	// Get user's language or default to Chinese
	lang := l.getUserLanguage(userID)
	if _, ok := l.translations[lang]; !ok {
		lang = LangZH
	}

	// Get translation
	text, ok := l.translations[lang][key]
	if !ok {
		// Fallback to Chinese
		text, _ = l.translations[LangZH][key]
	}

	// Simple argument replacement
	for i, arg := range args {
		placeholder := fmt.Sprintf("{%d}", i+1)
		text = fmt.Sprintf(text, placeholder)
		text = fmt.Sprintf(text, arg)
	}

	return text
}

// getUserLanguage gets user's preferred language
func (l *LanguageSystem) getUserLanguage(userID int64) string {
	if lang, ok := l.userLanguages[userID]; ok {
		return lang
	}
	return LangZH // Default to Chinese
}

// SetUserLanguage sets user's preferred language
func (l *LanguageSystem) SetUserLanguage(userID int64, lang string) {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	l.userLanguages[userID] = lang
	l.save()
}

// GetLanguage gets current language code for user
func (l *LanguageSystem) GetLanguage(userID int64) string {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	return l.getUserLanguage(userID)
}

// GetSupportedLanguages returns list of supported languages
func (l *LanguageSystem) GetSupportedLanguages() map[string]string {
	return languageSys.supportedLanguages
}

// save saves language data to file
func (l *LanguageSystem) save() {
	data, _ := json.MarshalIndent(languageSys.userLanguages, "", "  ")
	os.WriteFile(l.storageFile, data, 0644)
}

// load loads language data from file
func (l *LanguageSystem) load() {
	data, err := os.ReadFile(l.storageFile)
	if err == nil {
		json.Unmarshal(data, &languageSys.userLanguages)
		log.Printf("LanguageSystem: Loaded %d user language preferences", len(languageSys.userLanguages))
	}
}

// GetTranslations returns all translations for a language
func (l *LanguageSystem) GetTranslations(lang string) map[string]string {
	l.mutex.RLock()
	defer l.mutex.RUnlock()

	if translations, ok := l.translations[lang]; ok {
		// Return a copy to avoid concurrent map access issues
		result := make(map[string]string)
		for k, v := range translations {
			result[k] = v
		}
		return result
	}
	return nil
}

// AddTranslation adds or updates a translation
func (l *LanguageSystem) AddTranslation(lang, key, text string) {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	if _, ok := l.translations[lang]; !ok {
		l.translations[lang] = make(map[string]string)
	}
	l.translations[lang][key] = text
}

// GetUserLanguageWithFlag returns language code and whether it was explicitly set
func (l *LanguageSystem) GetUserLanguageWithFlag(userID int64) (string, bool) {
	l.mutex.RLock()
	defer l.mutex.RUnlock()

	lang, explicitlySet := l.userLanguages[userID]
	if lang == "" {
		lang = LangZH
		explicitlySet = false
	}
	return lang, explicitlySet
}
