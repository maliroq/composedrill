package rehearse

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maliroq/composedrill/internal/model"
)

func serviceWith(config, data, startup, health string) map[string]*model.ServiceRecovery {
	return map[string]*model.ServiceRecovery{
		"app": {
			Service:       "app",
			Configuration: config,
			DataRestore:   data,
			Startup:       startup,
			Health:        health,
		},
	}
}

func TestFinalizeCleanupFailureAlwaysFails(t *testing.T) {
	rep := model.RecoveryReport{RunID: "test", CleanupStatus: "FAIL: cleanup refused"}
	got, err := finalize(rep, serviceWith("PASS", "PASS", "MANUAL CHECK REQUIRED", "MANUAL CHECK REQUIRED"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "FAIL" {
		t.Fatalf("cleanup failure must force FAIL, got %q", got.Status)
	}
}

func TestFinalizeHighFindingForcesFail(t *testing.T) {
	rep := model.RecoveryReport{
		RunID:         "test",
		CleanupStatus: "PASS",
		Findings:      []model.Finding{{Severity: "HIGH", Code: "bad-config", Message: "invalid recovery configuration"}},
	}
	got, err := finalize(rep, serviceWith("PASS", "PASS", "PASS", "PASS"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "FAIL" {
		t.Fatalf("HIGH finding must force FAIL, got %q", got.Status)
	}
}

func TestFinalizeEvidenceWriteFailureIsReturned(t *testing.T) {
	parent := t.TempDir()
	blocker := filepath.Join(parent, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	rep := model.RecoveryReport{RunID: "test", CleanupStatus: "PASS"}
	_, err := finalize(rep, serviceWith("PASS", "PASS", "PASS", "PASS"), blocker)
	if err == nil {
		t.Fatal("expected evidence write failure")
	}
	if !strings.Contains(err.Error(), "write recovery evidence") {
		t.Fatalf("expected explicit evidence-write error, got %q", err)
	}
}

func TestMySQLRestoreArgsSelectClientAndDatabase(t *testing.T) {
	args, err := databaseRestoreArgs(&model.BackupEntry{Service: "db", Engine: "mariadb", Strategy: "mysql-sql"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"command -v mariadb", "command -v mysql", "MYSQL_DATABASE", "MARIADB_DATABASE"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("restore command missing %q: %s", want, joined)
		}
	}
}

func TestPreflightRefusesHostFileLoadingKeysInFlowAndJSONSyntax(t *testing.T) {
	cases := [][]byte{
		[]byte(`services: {web: {image: x, extends: {file: ../prod.yml, service: web}}}`),
		[]byte(`{"services":{"web":{"image":"x","label_file":"/tmp/prod.labels"}}}`),
		[]byte(`include: ../prod.yml`),
	}
	for _, raw := range cases {
		if err := preflightComposeSource(raw); err == nil {
			t.Fatalf("expected pre-canonicalization refusal for %s", raw)
		}
	}
}
