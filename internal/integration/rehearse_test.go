//go:build integration

package integration_test

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/maliroq/composedrill/internal/model"
	"github.com/maliroq/composedrill/internal/rehearse"
)

func dockerReady() bool {
	return exec.Command("docker", "info").Run() == nil && exec.Command("docker", "compose", "version").Run() == nil
}
func fixture(t *testing.T, name string) string {
	p, err := filepath.Abs(filepath.Join("..", "..", "examples", name, "recovery-bundle"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestStatelessRecovery(t *testing.T) {
	if !dockerReady() {
		t.Skip("Docker daemon unavailable")
	}
	rep, err := rehearse.Run(context.Background(), rehearse.Options{Root: t.TempDir(), Backup: fixture(t, "stateless"), Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != "PASS" {
		t.Fatalf("expected PASS, got %s: %+v", rep.Status, rep)
	}
}
func TestBrokenBackupDetected(t *testing.T) {
	if !dockerReady() {
		t.Skip("Docker daemon unavailable")
	}
	rep, err := rehearse.Run(context.Background(), rehearse.Options{Root: t.TempDir(), Backup: fixture(t, "broken"), Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != "FAIL" {
		t.Fatalf("expected FAIL, got %s: %+v", rep.Status, rep)
	}
}
func TestPostgresLogicalRestore(t *testing.T) {
	if !dockerReady() {
		t.Skip("Docker daemon unavailable")
	}
	rep, err := rehearse.Run(context.Background(), rehearse.Options{Root: t.TempDir(), Backup: fixture(t, "postgres"), Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != "PASS" {
		t.Fatalf("expected PASS, got %s: %+v", rep.Status, rep)
	}
}

func TestSQLiteRecovery(t *testing.T) {
	if !dockerReady() {
		t.Skip("Docker daemon unavailable")
	}
	rep, err := rehearse.Run(context.Background(), rehearse.Options{Root: t.TempDir(), Backup: fixture(t, "sqlite"), Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != "PASS" {
		t.Fatalf("expected PASS, got %s: %+v", rep.Status, rep)
	}
}

func TestBindMountedRecovery(t *testing.T) {
	if !dockerReady() {
		t.Skip("Docker daemon unavailable")
	}
	rep, err := rehearse.Run(context.Background(), rehearse.Options{Root: t.TempDir(), Backup: fixture(t, "bind"), Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != "PASS" {
		t.Fatalf("expected PASS, got %s: %+v", rep.Status, rep)
	}
}

const integrationBusyboxImage = "busybox@sha256:fd7dc98638c8e305f4dc34e979f1c0fdfdcaeb0fbf8fcff77ae834b6da3d7e6e"

func makeVolumeBundle(t *testing.T, anonymous bool) string {
	t.Helper()
	d := t.TempDir()
	compose := "services:\n  app:\n    image: " + integrationBusyboxImage + "\n    command: [\"sh\", \"-c\", \"sleep 3600\"]\n    volumes:\n"
	if anonymous {
		compose += "      - /data\n"
	} else {
		compose += "      - data:/data\n"
	}
	compose += "    healthcheck:\n      test: [\"CMD-SHELL\", \"grep -qx restored /data/marker\"]\n      interval: 1s\n      timeout: 1s\n      retries: 10\n"
	if !anonymous {
		compose += "volumes:\n  data:\n"
	}
	composePath := filepath.Join(d, "compose.yaml")
	if err := os.WriteFile(composePath, []byte(compose), 0o600); err != nil {
		t.Fatal(err)
	}

	tarPath := filepath.Join(d, "data.tar")
	f, err := os.Create(tarPath)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	body := []byte("restored\n")
	if err := tw.WriteHeader(&tar.Header{Name: "marker", Mode: 0o644, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	composeBytes, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatal(err)
	}
	tarBytes, err := os.ReadFile(tarPath)
	if err != nil {
		t.Fatal(err)
	}
	composeSum := sha256.Sum256(composeBytes)
	tarSum := sha256.Sum256(tarBytes)
	entry := model.BackupEntry{Kind: "volume", Service: "app", Target: "/data", Artifact: "data.tar", SHA256: hex.EncodeToString(tarSum[:])}
	if !anonymous {
		entry.Volume = "data"
	}
	manifest := model.BackupManifest{
		Format:        "composedrill-recovery-bundle",
		Version:       1,
		ComposeSHA256: hex.EncodeToString(composeSum[:]),
		Entries: []model.BackupEntry{
			{Kind: "compose", Artifact: "compose.yaml", SHA256: hex.EncodeToString(composeSum[:])},
			entry,
		},
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "composedrill-backup.json"), manifestBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestNamedVolumeArchiveRecovery(t *testing.T) {
	if !dockerReady() {
		t.Skip("Docker daemon unavailable")
	}
	rep, err := rehearse.Run(context.Background(), rehearse.Options{Root: t.TempDir(), Backup: makeVolumeBundle(t, false), Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != "PASS" {
		t.Fatalf("expected PASS, got %s: %+v", rep.Status, rep)
	}
}

func TestAnonymousVolumeArchiveRecovery(t *testing.T) {
	if !dockerReady() {
		t.Skip("Docker daemon unavailable")
	}
	rep, err := rehearse.Run(context.Background(), rehearse.Options{Root: t.TempDir(), Backup: makeVolumeBundle(t, true), Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != "PASS" {
		t.Fatalf("expected PASS, got %s: %+v", rep.Status, rep)
	}
}

func makeMariaDBBundle(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	compose := `services:
  db:
    image: mariadb:11.4
    environment:
      MARIADB_ROOT_PASSWORD: fixture-root-password
      MARIADB_DATABASE: app
    volumes:
      - dbdata:/var/lib/mysql
    healthcheck:
      test: ["CMD-SHELL", "mariadb-admin ping -h 127.0.0.1 -uroot -p\"$MARIADB_ROOT_PASSWORD\" --silent"]
      interval: 2s
      timeout: 2s
      retries: 30
volumes:
  dbdata:
`
	checks := `{"version":1,"checks":[{"name":"probe-row","service":"db","type":"command","command":["sh","-ceu","mariadb -uroot -p\"$MARIADB_ROOT_PASSWORD\" -D \"$MARIADB_DATABASE\" -Nse 'SELECT marker FROM recovery_probe WHERE id=1'"],"expect_contains":"backup-restored"}]}`
	dump := "CREATE TABLE recovery_probe (id INT PRIMARY KEY, marker VARCHAR(64) NOT NULL);\nINSERT INTO recovery_probe (id, marker) VALUES (1, 'backup-restored');\n"

	files := map[string][]byte{
		"compose.yaml":             []byte(compose),
		"composedrill-checks.json": []byte(checks),
		"database.sql":             []byte(dump),
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(d, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	composeSum := sha256.Sum256(files["compose.yaml"])
	checksSum := sha256.Sum256(files["composedrill-checks.json"])
	dumpSum := sha256.Sum256(files["database.sql"])
	manifest := model.BackupManifest{
		Format:        "composedrill-recovery-bundle",
		Version:       1,
		ComposeSHA256: hex.EncodeToString(composeSum[:]),
		Entries: []model.BackupEntry{
			{Kind: "compose", Artifact: "compose.yaml", SHA256: hex.EncodeToString(composeSum[:])},
			{Kind: "checks", Artifact: "composedrill-checks.json", SHA256: hex.EncodeToString(checksSum[:])},
			{Kind: "database", Service: "db", Engine: "mariadb", Strategy: "mysql-sql", Artifact: "database.sql", SHA256: hex.EncodeToString(dumpSum[:])},
		},
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "composedrill-backup.json"), manifestBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestMariaDBLogicalRestore(t *testing.T) {
	if !dockerReady() {
		t.Skip("Docker daemon unavailable")
	}
	rep, err := rehearse.Run(context.Background(), rehearse.Options{Root: t.TempDir(), Backup: makeMariaDBBundle(t), Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != "PASS" {
		t.Fatalf("expected PASS, got %s: %+v", rep.Status, rep)
	}
}
