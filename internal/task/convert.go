package task

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func asMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, val := range m {
			out[fmt.Sprint(k)] = val
		}
		return out, true
	default:
		return nil, false
	}
}

func asString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case uint64:
		return strconv.FormatUint(t, 10)
	case float64:
		if t == float64(int(t)) {
			return strconv.Itoa(int(t))
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(t)
	}
}

func asInt(v any) (int, bool) {
	switch t := v.(type) {
	case int:
		return t, true
	case int64:
		return int(t), true
	case uint64:
		return int(t), true
	case float64:
		if t == float64(int(t)) {
			return int(t), true
		}
		return 0, false
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(t))
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

func asBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		return s == "true" || s == "yes" || s == "1"
	default:
		return false
	}
}

func asStringSlice(v any) []string {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return nil
		}
		return []string{s}
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := strings.TrimSpace(asString(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		out := make([]string, 0, len(t))
		for _, s := range t {
			if strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		if s := strings.TrimSpace(asString(v)); s != "" {
			return []string{s}
		}
		return nil
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func stringMap(v any) map[string]string {
	if v == nil {
		return nil
	}
	if m, ok := asMap(v); ok {
		out := make(map[string]string, len(m))
		for _, k := range sortedKeys(m) {
			out[k] = asString(m[k])
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	list := asStringSlice(v)
	if len(list) == 0 {
		return nil
	}
	out := make(map[string]string, len(list))
	for _, item := range list {
		k, val, found := strings.Cut(item, "=")
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if !found {
			out[k] = ""
			continue
		}
		out[k] = val
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func dependsOn(v any) []string {
	if v == nil {
		return nil
	}
	if m, ok := asMap(v); ok {
		return sortedKeys(m)
	}
	return asStringSlice(v)
}

func commandSpec(v any) (parts []string, shell string) {
	switch t := v.(type) {
	case nil:
		return nil, ""
	case string:
		return nil, t
	default:
		return asStringSlice(v), ""
	}
}

func envKey(name string) string {
	var b strings.Builder
	b.Grow(len(name) + 5)
	for _, r := range strings.ToUpper(name) {
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	s := strings.Trim(b.String(), "_")
	if s == "" {
		return "PORT"
	}
	if strings.HasSuffix(s, "_PORT") {
		return s
	}
	return s + "_PORT"
}

func portNameFromVar(hostVar string) string {
	s := strings.TrimSuffix(strings.ToLower(hostVar), "_port")
	s = strings.ReplaceAll(s, "_", "-")
	if s == "" {
		return ""
	}
	return s
}
