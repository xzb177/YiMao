package callback

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// CallbackParser parses and formats callback data
type CallbackParser struct {
	separator string
}

// Callback represents a parsed callback query
type Callback struct {
	Action string                 // The action to perform (search, subscribe, download, page, cancel)
	Data   map[string]string     // Additional data
	Raw    string                 // Original raw data
}

// NewCallbackParser creates a new callback parser
func NewCallbackParser() *CallbackParser {
	return &CallbackParser{
		separator: ":",
	}
}

// Parse parses a callback string into a Callback struct
// Format: action:key1:value1:key2:value2
// Example: search:id:123:type:movie
func (p *CallbackParser) Parse(data string) (*Callback, error) {
	if data == "" {
		return nil, fmt.Errorf("empty callback data")
	}

	parts := strings.Split(data, p.separator)
	if len(parts) < 1 {
		return nil, fmt.Errorf("invalid callback format")
	}

	callback := &Callback{
		Action: parts[0],
		Data:   make(map[string]string),
		Raw:    data,
	}

	// Parse key-value pairs
	for i := 1; i < len(parts); i += 2 {
		if i+1 < len(parts) {
			key := parts[i]
			value := parts[i+1]
			callback.Data[key] = value
		}
	}

	log.Printf("[Callback] Parsed: action=%s, data=%v", callback.Action, callback.Data)

	return callback, nil
}

// Format creates a callback string from action and optional data
// Example: Format("search", "id", "123", "type", "movie") -> "search:id:123:type:movie"
func (p *CallbackParser) Format(action string, args ...string) string {
	if len(args) == 0 {
		return action
	}

	parts := []string{action}
	parts = append(parts, args...)

	return strings.Join(parts, p.separator)
}

// FormatWithData creates a callback string from action and data map
func (p *CallbackParser) FormatWithData(action string, data map[string]string) string {
	parts := []string{action}

	for key, value := range data {
		parts = append(parts, key, value)
	}

	return strings.Join(parts, p.separator)
}

// ParseJSON parses JSON-encoded callback data
// Example: {"action":"search","data":{"id":"123","type":"movie"}}
func (p *CallbackParser) ParseJSON(jsonStr string) (*Callback, error) {
	var callback Callback
	err := json.Unmarshal([]byte(jsonStr), &callback)
	if err != nil {
		return nil, err
	}
	callback.Raw = jsonStr
	return &callback, nil
}

// FormatJSON formats a callback to JSON
func (p *CallbackParser) FormatJSON(callback *Callback) (string, error) {
	data, err := json.Marshal(callback)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// GetIntValue gets an integer value from callback data
func (c *Callback) GetIntValue(key string) (int, error) {
	valStr, exists := c.Data[key]
	if !exists {
		return 0, fmt.Errorf("key %s not found", key)
	}

	var val int
	_, err := fmt.Sscanf(valStr, "%d", &val)
	return val, err
}

// GetStringValue gets a string value from callback data
func (c *Callback) GetStringValue(key string) (string, error) {
	val, exists := c.Data[key]
	if !exists {
		return "", fmt.Errorf("key %s not found", key)
	}
	return val, nil
}

// HasData checks if a key exists in callback data
func (c *Callback) HasData(key string) bool {
	_, exists := c.Data[key]
	return exists
}

// Common callback builders

// BuildSearchCallback builds a search callback
func BuildSearchCallback(mediaID, mediaType string) string {
	p := NewCallbackParser()
	return p.FormatWithData("search", map[string]string{
		"id":   mediaID,
		"type": mediaType,
	})
}

// BuildSubscribeCallback builds a subscribe callback
func BuildSubscribeCallback(mediaID, mediaType string, season int) string {
	p := NewCallbackParser()
	data := map[string]string{
		"id":   mediaID,
		"type": mediaType,
	}
	if season > 0 {
		data["season"] = fmt.Sprintf("%d", season)
	}
	return p.FormatWithData("subscribe", data)
}

// BuildDownloadCallback builds a download callback
func BuildDownloadCallback(torrentID string) string {
	p := NewCallbackParser()
	return p.FormatWithData("download", map[string]string{
		"id": torrentID,
	})
}

// BuildPageCallback builds a page navigation callback
func BuildPageCallback(page int) string {
	p := NewCallbackParser()
	return p.Format("page", fmt.Sprintf("%d", page))
}

// BuildSelectCallback builds a selection callback
func BuildSelectCallback(index string, page int) string {
	p := NewCallbackParser()
	return p.FormatWithData("select", map[string]string{
		"index": index,
		"page":  fmt.Sprintf("%d", page),
	})
}

// BuildCancelCallback builds a cancel callback
func BuildCancelCallback() string {
	return "cancel"
}
