package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestAggregateServiceStatusUsesWorstState(t *testing.T) {
	svcs := []any{
		map[string]any{"health": "PASS"},
		map[string]any{"health": "MANUAL CHECK REQUIRED"},
		map[string]any{"health": "FAIL"},
	}
	if got := aggregateServiceStatus(svcs, "health"); got != "FAIL" {
		t.Fatalf("aggregate health = %q, want FAIL", got)
	}
}

func TestBackupManifestStatusRequiresDigest(t *testing.T) {
	if got := backupManifestStatus(map[string]any{}); got != "UNKNOWN" {
		t.Fatalf("missing manifest status = %q, want UNKNOWN", got)
	}
	if got := backupManifestStatus(map[string]any{"backup_manifest_sha256": "abc"}); got != "PASS" {
		t.Fatalf("present manifest status = %q, want PASS", got)
	}
}

func TestColorStatusHonorsDisabledMode(t *testing.T) {
	if got := colorStatus("FAIL", false); got != "FAIL" {
		t.Fatalf("disabled color output = %q, want plain FAIL", got)
	}
	if got := colorStatus("PASS", true); got == "PASS" {
		t.Fatalf("enabled color output should contain ANSI escape")
	}
}

func TestColorEnabledHonorsNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if colorEnabled(os.Stdout) {
		t.Fatal("NO_COLOR must disable color")
	}
}

func TestPrintReportCanonicalSummaryIsPlainOnNonTTY(t *testing.T) {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	printReport(map[string]any{
		"run_id":                 "run-1",
		"status":                 "FAIL",
		"backup_manifest_sha256": "abc",
		"cleanup_status":         "PASS",
		"evidence_path":          ".composedrill/evidence/run-1.json",
		"services": []any{map[string]any{
			"service":       "app",
			"configuration": "PASS",
			"data_restore":  "PASS",
			"startup":       "PASS",
			"health":        "FAIL",
			"reason":        "unhealthy",
		}},
	})
	_ = w.Close()
	os.Stdout = old
	b, _ := io.ReadAll(r)
	out := string(b)
	for _, want := range []string{
		"BACKUP / MANIFEST        PASS",
		"DATA RESTORE             PASS",
		"CONTAINER STARTUP        PASS",
		"APPLICATION HEALTH       FAIL",
		"RESULT                   FAIL",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("non-TTY output must not contain ANSI escapes: %q", out)
	}
}
