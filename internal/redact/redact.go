package redact

import (
	"regexp"
)

var (
	sensitiveKey        = regexp.MustCompile(`(?i)(pass(word|wd)?|secret|token|api[_-]?key|private[_-]?key|credential|cookie|authorization)`)
	sensitiveAssignment = regexp.MustCompile(`(?i)(\b[A-Za-z0-9_.-]*(?:pass(?:word|wd)?|secret|token|api[_-]?key|private[_-]?key|credential|cookie|authorization)[A-Za-z0-9_.-]*\b\s*[=:]\s*)([^\s,;]+)`)
	urlCredential       = regexp.MustCompile(`(?i)(https?://[^\s/:@]+:)([^\s/@]+)(@)`)
	jsonSensitiveValue  = regexp.MustCompile(`(?i)("[^"]*(?:pass(?:word|wd)?|secret|token|api[_-]?key|private[_-]?key|credential|cookie|authorization)[^"]*"\s*:\s*")([^"]*)(")`)
)

func Key(k string) bool { return sensitiveKey.MatchString(k) }

func Value(v string) string {
	if v == "" {
		return v
	}
	return "[REDACTED]"
}

func Line(s string) string {
	out := sensitiveAssignment.ReplaceAllString(s, `${1}[REDACTED]`)
	out = jsonSensitiveValue.ReplaceAllString(out, `${1}[REDACTED]${3}`)
	out = urlCredential.ReplaceAllString(out, `${1}[REDACTED]${3}`)
	return out
}
