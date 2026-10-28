package redact

import (
	"strings"
	"testing"
)

func TestLine(t *testing.T) {
	got := Line("API_TOKEN=super-secret-value")
	if strings.Contains(got, "super-secret-value") || !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("not redacted: %s", got)
	}
}
func TestNonSecret(t *testing.T) {
	if got := Line("PORT=8080"); got != "PORT=8080" {
		t.Fatalf("unexpected redaction %s", got)
	}
}

func TestLineRedactsEmbeddedAssignmentsAndURLCredentials(t *testing.T) {
	got := Line("database failed: password=hunter2 token:abc https://alice:s3cr3t@example.test/x")
	if got != "database failed: password=[REDACTED] token:[REDACTED] https://alice:[REDACTED]@example.test/x" {
		t.Fatalf("unexpected redaction: %q", got)
	}
}

func TestLineRedactsJSONStyleSensitiveValues(t *testing.T) {
	got := Line(`docker error: {"password":"hunter2","normal":"visible","api_token":"abc123"}`)
	if strings.Contains(got, "hunter2") || strings.Contains(got, "abc123") || !strings.Contains(got, `"normal":"visible"`) {
		t.Fatalf("unexpected JSON redaction: %q", got)
	}
}
