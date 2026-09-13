package mihoyo

import (
	"encoding/json"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type recordObject map[string]json.RawMessage

func recordMap(raw json.RawMessage) recordObject {
	var result recordObject
	_ = json.Unmarshal(raw, &result)
	return result
}

func recordInt(raw json.RawMessage, min, max int64) *int64 {
	value := strings.TrimSpace(string(raw))
	if strings.HasPrefix(value, `"`) {
		if json.Unmarshal(raw, &value) != nil {
			return nil
		}
		value = strings.TrimSpace(value)
	}
	if value == "" {
		return nil
	}
	for i, c := range value {
		if c < '0' || c > '9' {
			if c != '-' || i != 0 {
				return nil
			}
		}
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < min || n > max {
		return nil
	}
	return &n
}

func recordBool(raw json.RawMessage) *bool {
	value := strings.TrimSpace(string(raw))
	if strings.HasPrefix(value, `"`) {
		if json.Unmarshal(raw, &value) != nil {
			return nil
		}
		value = strings.ToLower(strings.TrimSpace(value))
	}
	var b bool
	switch value {
	case "true", "1":
		b = true
	case "false", "0":
		b = false
	default:
		return nil
	}
	return &b
}

func recordText(raw json.RawMessage, max int) string {
	var value string
	if json.Unmarshal(raw, &value) != nil || !utf8.ValidString(value) {
		return ""
	}
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) > max || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return ""
	}
	return value
}
