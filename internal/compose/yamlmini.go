package compose

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

type yline struct {
	indent int
	text   string
	line   int
}

type ParseInfo struct{ Unsupported []string }

func ParseYAMLSubset(data []byte) (map[string]any, ParseInfo, error) {
	lines := []yline{}
	info := ParseInfo{}
	for n, raw := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		trim := strings.TrimLeftFunc(raw, unicode.IsSpace)
		if strings.HasPrefix(trim, "#") {
			continue
		}
		indent := len(raw) - len(trim)
		if strings.Contains(trim, "&") || strings.HasPrefix(strings.TrimSpace(trim), "<<:") || strings.Contains(trim, "*") {
			info.Unsupported = append(info.Unsupported, fmt.Sprintf("line %d uses YAML anchors/aliases/merge; static scan may be incomplete", n+1))
		}
		if strings.Contains(trim, "!include") || strings.Contains(trim, "!reset") || strings.Contains(trim, "!override") {
			info.Unsupported = append(info.Unsupported, fmt.Sprintf("line %d uses a Compose YAML tag; static scan may be incomplete", n+1))
		}
		trim = stripComment(trim)
		if strings.TrimSpace(trim) == "" {
			continue
		}
		lines = append(lines, yline{indent: indent, text: strings.TrimSpace(trim), line: n + 1})
	}
	if len(lines) == 0 {
		return map[string]any{}, info, nil
	}
	v, next, err := parseBlock(lines, 0, lines[0].indent)
	if err != nil {
		return nil, info, err
	}
	if next != len(lines) {
		return nil, info, fmt.Errorf("could not parse YAML near line %d", lines[next].line)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, info, fmt.Errorf("compose document root must be a mapping")
	}
	return m, info, nil
}

func stripComment(s string) string {
	var quote rune
	escaped := false
	for i, r := range s {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && quote == '"' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if r == '#' && (i == 0 || unicode.IsSpace(rune(s[i-1]))) {
			return strings.TrimSpace(s[:i])
		}
	}
	return strings.TrimSpace(s)
}

func parseBlock(lines []yline, i, indent int) (any, int, error) {
	if i >= len(lines) {
		return map[string]any{}, i, nil
	}
	if lines[i].indent < indent {
		return nil, i, nil
	}
	isList := strings.HasPrefix(lines[i].text, "- ") || lines[i].text == "-"
	if isList {
		arr := []any{}
		for i < len(lines) && lines[i].indent == indent && (strings.HasPrefix(lines[i].text, "- ") || lines[i].text == "-") {
			item := strings.TrimSpace(strings.TrimPrefix(lines[i].text, "-"))
			lineNo := lines[i].line
			i++
			if item == "" {
				if i >= len(lines) || lines[i].indent <= indent {
					arr = append(arr, nil)
					continue
				}
				v, ni, err := parseBlock(lines, i, lines[i].indent)
				if err != nil {
					return nil, i, err
				}
				arr = append(arr, v)
				i = ni
				continue
			}
			if k, rest, ok := splitKV(item); ok {
				m := map[string]any{}
				if rest == "" {
					if i < len(lines) && lines[i].indent > indent {
						v, ni, err := parseBlock(lines, i, lines[i].indent)
						if err != nil {
							return nil, i, err
						}
						m[k] = v
						i = ni
					} else {
						m[k] = map[string]any{}
					}
				} else {
					m[k] = parseScalar(rest)
				}
				if i < len(lines) && lines[i].indent > indent {
					v, ni, err := parseBlock(lines, i, lines[i].indent)
					if err != nil {
						return nil, i, err
					}
					if mm, ok := v.(map[string]any); ok {
						for kk, vv := range mm {
							m[kk] = vv
						}
					} else {
						return nil, i, fmt.Errorf("line %d: expected mapping continuation", lineNo)
					}
					i = ni
				}
				arr = append(arr, m)
			} else {
				arr = append(arr, parseScalar(item))
				if i < len(lines) && lines[i].indent > indent {
					return nil, i, fmt.Errorf("line %d: scalar list item cannot own nested block", lineNo)
				}
			}
		}
		return arr, i, nil
	}
	m := map[string]any{}
	for i < len(lines) && lines[i].indent == indent && !strings.HasPrefix(lines[i].text, "-") {
		k, rest, ok := splitKV(lines[i].text)
		if !ok {
			return nil, i, fmt.Errorf("line %d: expected key: value", lines[i].line)
		}
		i++
		if rest == "" {
			if i < len(lines) && lines[i].indent > indent {
				v, ni, err := parseBlock(lines, i, lines[i].indent)
				if err != nil {
					return nil, i, err
				}
				m[k] = v
				i = ni
			} else {
				m[k] = map[string]any{}
			}
		} else if rest == "|" || rest == ">" {
			parts := []string{}
			for i < len(lines) && lines[i].indent > indent {
				parts = append(parts, lines[i].text)
				i++
			}
			m[k] = strings.Join(parts, "\n")
		} else {
			m[k] = parseScalar(rest)
		}
	}
	return m, i, nil
}

func splitKV(s string) (string, string, bool) {
	var quote rune
	depth := 0
	for i, r := range s {
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if r == '[' || r == '{' {
			depth++
		}
		if r == ']' || r == '}' {
			depth--
		}
		if r == ':' && depth == 0 && (i+1 == len(s) || unicode.IsSpace(rune(s[i+1]))) {
			k := strings.TrimSpace(s[:i])
			if k == "" {
				return "", "", false
			}
			return unquote(k), strings.TrimSpace(s[i+1:]), true
		}
	}
	return "", "", false
}

func parseScalar(s string) any {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		return unquote(s)
	}
	if s == "null" || s == "~" {
		return nil
	}
	if s == "true" {
		return true
	}
	if s == "false" {
		return false
	}
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		inner := strings.TrimSpace(s[1 : len(s)-1])
		if inner == "" {
			return []any{}
		}
		ps := splitCSV(inner)
		out := make([]any, 0, len(ps))
		for _, p := range ps {
			out = append(out, parseScalar(p))
		}
		return out
	}
	if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
		inner := strings.TrimSpace(s[1 : len(s)-1])
		m := map[string]any{}
		if inner == "" {
			return m
		}
		for _, p := range splitCSV(inner) {
			k, v, ok := splitKV(p)
			if ok {
				m[k] = parseScalar(v)
			}
		}
		return m
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}
	return s
}

func splitCSV(s string) []string {
	var out []string
	start := 0
	var q rune
	depth := 0
	for i, r := range s {
		if q != 0 {
			if r == q {
				q = 0
			}
			continue
		}
		if r == '\'' || r == '"' {
			q = r
			continue
		}
		if r == '[' || r == '{' {
			depth++
		}
		if r == ']' || r == '}' {
			depth--
		}
		if r == ',' && depth == 0 {
			out = append(out, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	out = append(out, strings.TrimSpace(s[start:]))
	return out
}
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		if v, e := strconv.Unquote(s); e == nil {
			return v
		}
	}
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'")
	}
	return s
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}
func asList(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	return nil
}
func asString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case int64:
		return strconv.FormatInt(x, 10)
	case bool:
		return strconv.FormatBool(x)
	}
	return ""
}
func stringList(v any) []string {
	a := asList(v)
	out := []string{}
	if a != nil {
		for _, x := range a {
			if s := asString(x); s != "" {
				out = append(out, s)
			}
		}
	} else if s := asString(v); s != "" {
		out = append(out, s)
	}
	return out
}
