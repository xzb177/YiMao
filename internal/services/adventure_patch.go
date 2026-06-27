//go:build ignore

package services

import (
	"encoding/json"
	"strings"
)

func cleanAIJSON(raw string) string {
	cleaned := strings.TrimSpace(raw)
	if strings.HasPrefix(cleaned, "```") {
		lines := strings.Split(cleaned, "\n")
		var jsonLines []string
		inBlock := false
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "```") {
				inBlock = !inBlock
				continue
			}
			if inBlock || (!strings.HasPrefix(trimmed, "```") && len(jsonLines) > 0) {
				jsonLines = append(jsonLines, line)
			}
		}
		if len(jsonLines) > 0 {
			cleaned = strings.Join(jsonLines, "\n")
		}
	}
	if idx := strings.Index(cleaned, "{"); idx >= 0 {
		if endIdx := strings.LastIndex(cleaned, "}"); endIdx > idx {
			cleaned = cleaned[idx : endIdx+1]
		} else {
			// JSON被截断，尝试补全
			cleaned = cleaned[idx:]
			cleaned = completeTruncatedJSON(cleaned)
		}
	}
	return cleaned
}

// completeTruncatedJSON 尝试补全被截断的JSON
func completeTruncatedJSON(s string) string {
	openBraces := 0
	openBrackets := 0
	inString := false
	escaped := false
	
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && inString {
			escaped = true
			continue
		}
		if c == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if c == '{' {
			openBraces++
		} else if c == '}' {
			openBraces--
		} else if c == '[' {
			openBrackets++
		} else if c == ']' {
			openBrackets--
		}
	}
	
	if inString {
		s += "\""
	}
	
	for i := 0; i < openBrackets; i++ {
		s += "]"
	}
	
	for i := 0; i < openBraces; i++ {
		s += "}"
	}
	
	var test interface{}
	if json.Unmarshal([]byte(s), &test) == nil {
		return s
	}
	
	return aggressiveJSONFix(s)
}

func aggressiveJSONFix(s string) string {
	lastComma := strings.LastIndex(s, ",")
	if lastComma > 0 {
		s = s[:lastComma]
	}
	
	lastQuote := strings.LastIndex(s, "\"")
	if lastQuote > 0 {
		beforeQuote := s[:lastQuote]
		if strings.HasSuffix(beforeQuote, ": ") || strings.HasSuffix(beforeQuote, ":") {
			s = beforeQuote
			lastComma = strings.LastIndex(s, ",")
			if lastComma > 0 {
				s = s[:lastComma]
			}
		}
	}
	
	openBraces := 0
	openBrackets := 0
	inString := false
	escaped := false
	
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && inString {
			escaped = true
			continue
		}
		if c == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if c == '{' {
			openBraces++
		} else if c == '}' {
			openBraces--
		} else if c == '[' {
			openBrackets++
		} else if c == ']' {
			openBrackets--
		}
	}
	
	if inString {
		s += "\""
	}
	
	for i := 0; i < openBrackets; i++ {
		s += "]"
	}
	for i := 0; i < openBraces; i++ {
		s += "}"
	}
	
	return s
}
