package rehearse

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maliroq/composedrill/internal/model"
)

func TestTransformIsolation(t *testing.T) {
	bundle := t.TempDir()
	work := t.TempDir()
	raw := []byte(`{"services":{"web":{"image":"nginx:1.27","container_name":"prod-web","ports":[{"target":80,"published":"8080","protocol":"tcp"}],"volumes":[{"type":"bind","source":"/srv/prod","target":"/data"}],"networks":{"default":null}}},"networks":{"default":{"name":"prod_default"}},"volumes":{}}`)
	tr, err := transformCompose(raw, "run1", "composedrill-run1", work, bundle, []model.Check{{Service: "web", Type: "http", Port: 80}})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(tr.JSON, &doc); err != nil {
		t.Fatal(err)
	}
	svc := doc["services"].(map[string]any)["web"].(map[string]any)
	if _, ok := svc["container_name"]; ok {
		t.Fatal("container_name survived")
	}
	if _, ok := svc["ports"]; ok {
		t.Fatal("production host port publication survived isolation transform")
	}
	mount := svc["volumes"].([]any)[0].(map[string]any)
	if strings.Contains(mount["source"].(string), "/srv/prod") {
		t.Fatal("production bind survived")
	}
	net := doc["networks"].(map[string]any)["default"].(map[string]any)
	if net["internal"] != true {
		t.Fatal("network not internal")
	}
}

func TestTransformDoesNotPublishCheckPorts(t *testing.T) {
	raw := []byte(`{"services":{"web":{"image":"nginx:1.27"}}}`)
	tr, err := transformCompose(raw, "run1", "composedrill-run1", t.TempDir(), t.TempDir(), []model.Check{{Service: "web", Type: "http", Port: 80}})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(tr.JSON, &doc); err != nil {
		t.Fatal(err)
	}
	svc := doc["services"].(map[string]any)["web"].(map[string]any)
	if _, ok := svc["ports"]; ok {
		t.Fatal("HTTP check must not cause a host port publication")
	}
	net := doc["networks"].(map[string]any)["default"].(map[string]any)
	if net["internal"] != true {
		t.Fatal("network probe fix weakened internal network isolation")
	}
}

func TestTransformRejectsPrivileged(t *testing.T) {
	raw := []byte(`{"services":{"bad":{"image":"x","privileged":true}}}`)
	if _, err := transformCompose(raw, "r", "composedrill-r", t.TempDir(), t.TempDir(), nil); err == nil {
		t.Fatal("expected refusal")
	}
}
func TestTransformRejectsExternalSecretFile(t *testing.T) {
	raw := []byte(`{"services":{"web":{"image":"x"}},"secrets":{"key":{"file":"/etc/prod-secret"}}}`)
	if _, err := transformCompose(raw, "r", "composedrill-r", t.TempDir(), t.TempDir(), nil); err == nil {
		t.Fatal("expected secret refusal")
	}
}
func TestBundleChecksumMismatch(t *testing.T) {
	d := t.TempDir()
	_ = os.WriteFile(filepath.Join(d, "compose.yaml"), []byte("services: {}\n"), 0600)
	m := `{"format":"composedrill-recovery-bundle","version":1,"created_at":"2026-10-26T00:00:00Z","entries":[{"kind":"compose","artifact":"compose.yaml","sha256":"deadbeef"}]}`
	_ = os.WriteFile(filepath.Join(d, "composedrill-backup.json"), []byte(m), 0600)
	if _, err := stageBundle(d, t.TempDir()); err == nil {
		t.Fatal("expected checksum mismatch")
	}
}
func TestCancelledWaitReturns(t *testing.T) {
	dir := t.TempDir()
	docker := filepath.Join(dir, "docker")
	_ = os.WriteFile(docker, []byte("#!/bin/sh\nsleep 5\n"), 0700)
	old := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+old)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := dockerCLI{composeFile: "x", project: "composedrill-x", env: safeDockerEnv()}
	start := time.Now()
	st, _ := d.waitService(ctx, "web", 10*time.Second)
	if st != "FAIL" || time.Since(start) > 2*time.Second {
		t.Fatalf("cancel did not stop promptly: %s %v", st, time.Since(start))
	}
}
func TestCleanupRefusesInvalidIdentity(t *testing.T) {
	if err := CleanupRun(context.Background(), "", "prod"); err == nil {
		t.Fatal("expected refusal")
	}
}

func TestCommandCheckExpectedOutput(t *testing.T) {
	dir := t.TempDir()
	docker := filepath.Join(dir, "docker")
	if err := os.WriteFile(docker, []byte("#!/bin/sh\nprintf 'backup-restored\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	d := dockerCLI{composeFile: "x", project: "composedrill-x", env: safeDockerEnv()}
	r := d.runCheck(context.Background(), model.Check{Name: "marker", Service: "db", Type: "command", Command: []string{"probe"}, ExpectContains: "backup-restored"})
	if r.Status != "PASS" {
		t.Fatalf("expected PASS, got %+v", r)
	}
	r = d.runCheck(context.Background(), model.Check{Name: "wrong", Service: "db", Type: "command", Command: []string{"probe"}, ExpectContains: "different"})
	if r.Status != "FAIL" {
		t.Fatalf("expected FAIL, got %+v", r)
	}
}

func TestCleanupWorkspaceBounded(t *testing.T) {
	run := "20261026T120000Z-abc123"
	p := filepath.Join(t.TempDir(), ".composedrill", "runs", run)
	if err := os.MkdirAll(p, 0700); err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(p)))
	if err := CleanupWorkspace(root, run); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("workspace still exists or unexpected stat error: %v", err)
	}
	if err := CleanupWorkspace(root, "../escape"); err == nil {
		t.Fatal("expected traversal refusal")
	}
}

func TestPreflightRejectsExternalFileFeatures(t *testing.T) {
	cases := [][]byte{
		[]byte("include:\n  - ../prod/compose.yaml\n"),
		[]byte("services:\n  app:\n    extends:\n      file: ../prod/base.yaml\n      service: app\n"),
		[]byte("services:\n  app:\n    label_file: ../prod/labels.env\n"),
	}
	for _, tc := range cases {
		if err := preflightComposeSource(tc); err == nil {
			t.Fatalf("expected conservative refusal for %q", string(tc))
		}
	}
}

func TestTransformRejectsEnvFileOutsideBundle(t *testing.T) {
	raw := []byte(`{"services":{"web":{"image":"nginx:1.27","env_file":[{"path":"/etc/prod.env"}]}}}`)
	if _, err := transformCompose(raw, "r", "composedrill-r", t.TempDir(), t.TempDir(), nil); err == nil {
		t.Fatal("expected env_file refusal")
	}
}

func TestProfiledServiceIsDetected(t *testing.T) {
	raw := []byte(`{"services":{"debug":{"image":"alpine:3.20","profiles":["debug"]}}}`)
	tr, err := transformCompose(raw, "r", "composedrill-r", t.TempDir(), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !tr.Profiled["debug"] {
		t.Fatal("profile-gated service not detected")
	}
}

func TestDirectoryArtifactRejectsSymlink(t *testing.T) {
	d := t.TempDir()
	artifact := filepath.Join(d, "data")
	if err := os.MkdirAll(artifact, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(d, "outside.txt")
	if err := os.WriteFile(target, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(artifact, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := hashPath(artifact); err == nil {
		t.Fatal("expected symlink refusal")
	}
}

func TestDefaultChecksMustBeDeclared(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "composedrill-checks.json"), []byte(`{"version":1,"checks":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadChecks("", root, model.BackupManifest{}); err == nil {
		t.Fatal("expected undeclared checks refusal")
	}
}

func TestInterpolationEnvMustBeDeclared(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("PASSWORD=fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := interpolationEnvFile(root, root, model.BackupManifest{}); err == nil {
		t.Fatal("expected undeclared .env refusal")
	}
}

func TestParsePSRequiresAllReplicasHealthy(t *testing.T) {
	state, health := parsePS(`[{"State":"running","Health":"healthy"},{"State":"running","Health":"unhealthy"}]`)
	if health != "unhealthy" {
		t.Fatalf("expected unhealthy aggregate, got state=%s health=%s", state, health)
	}
	state, health = parsePS(`[{"State":"running","Health":"healthy"},{"State":"running","Health":"healthy"}]`)
	if state != "running" || health != "healthy" {
		t.Fatalf("expected all healthy, got state=%s health=%s", state, health)
	}
}

func TestAnonymousVolumeBecomesDisposableNamedVolume(t *testing.T) {
	raw := []byte(`{"services":{"app":{"image":"alpine:3.20","volumes":[{"type":"volume","target":"/data"}]}}}`)
	tr, err := transformCompose(raw, "r", "composedrill-r", t.TempDir(), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	mounts := tr.Persistent["app"]
	if len(mounts) != 1 || mounts[0].Source == "" || tr.VolumeNames[mounts[0].Source] == "" {
		t.Fatalf("anonymous volume not isolated: %+v %+v", mounts, tr.VolumeNames)
	}
}

func TestTransformRejectsNamespaceAndHostEscapeFeatures(t *testing.T) {
	cases := []string{
		`{"services":{"bad":{"image":"x","network_mode":"container:prod"}}}`,
		`{"services":{"bad":{"image":"x","pid":"host"}}}`,
		`{"services":{"bad":{"image":"x","volumes_from":["container:prod"]}}}`,
		`{"services":{"bad":{"image":"x","cap_add":["SYS_ADMIN"]}}}`,
		`{"services":{"bad":{"image":"x","extra_hosts":["host.docker.internal:host-gateway"]}}}`,
		`{"services":{"bad":{"image":"x","security_opt":["seccomp=unconfined"]}}}`,
	}
	for _, raw := range cases {
		if _, err := transformCompose([]byte(raw), "r", "composedrill-r", t.TempDir(), t.TempDir(), nil); err == nil {
			t.Fatalf("expected unsafe runtime refusal for %s", raw)
		}
	}
}

func TestDisabledHealthcheckDoesNotCountAsVerification(t *testing.T) {
	cases := []string{
		`{"services":{"app":{"image":"x","healthcheck":{"disable":true}}}}`,
		`{"services":{"app":{"image":"x","healthcheck":{"test":["NONE"]}}}}`,
	}
	for _, raw := range cases {
		tr, err := transformCompose([]byte(raw), "r", "composedrill-r", t.TempDir(), t.TempDir(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if tr.Health["app"] {
			t.Fatalf("disabled healthcheck must not count as active: %s", raw)
		}
	}
}

func TestExplicitChecksMustBeInsideDeclaredBundle(t *testing.T) {
	bundle := t.TempDir()
	external := filepath.Join(t.TempDir(), "checks.json")
	if err := os.WriteFile(external, []byte(`{"version":1,"checks":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadChecks(external, bundle, model.BackupManifest{}); err == nil {
		t.Fatal("expected external checks file refusal")
	}
}

func TestTransformRejectsEmptyServices(t *testing.T) {
	if _, err := transformCompose([]byte(`{"services":{}}`), "r", "composedrill-r", t.TempDir(), t.TempDir(), nil); err == nil {
		t.Fatal("expected empty services refusal")
	}
}

func TestCleanupRefusesContainerWithMismatchedOwnershipLabels(t *testing.T) {
	dir := t.TempDir()
	writeFakeDocker(t, dir, `#!/bin/sh
set -eu
if [ "$1" = "ps" ]; then printf '%s\n' 'container123'; exit 0; fi
if [ "$1" = "inspect" ]; then
  printf '%s\n' '[{"Config":{"Labels":{"io.composedrill.run":"other-run","io.composedrill.managed":"true","com.docker.compose.project":"composedrill-run1"}}}]'
  exit 0
fi
if [ "$1" = "rm" ]; then exit 99; fi
exit 64
`)
	if err := CleanupRun(context.Background(), "run1", "composedrill-run1"); err == nil || !strings.Contains(err.Error(), "failed ownership verification") {
		t.Fatalf("expected ownership refusal, got %v", err)
	}
}

func TestTransformRefusesHostCapableVolumeAndNetworkDrivers(t *testing.T) {
	cases := []string{
		`{"services":{"app":{"image":"x"}},"volumes":{"data":{"driver":"local","driver_opts":{"type":"none","o":"bind","device":"/srv/prod"}}}}`,
		`{"services":{"app":{"image":"x"}},"volumes":{"data":{"driver":"nfs"}}}`,
		`{"services":{"app":{"image":"x"}},"networks":{"default":{"driver":"macvlan"}}}`,
		`{"services":{"app":{"image":"x"}},"networks":{"default":{"driver":"bridge","driver_opts":{"com.docker.network.bridge.name":"br-prod"}}}}`,
	}
	for _, raw := range cases {
		if _, err := transformCompose([]byte(raw), "r", "composedrill-r", t.TempDir(), t.TempDir(), nil); err == nil {
			t.Fatalf("expected driver configuration refusal for %s", raw)
		}
	}
}

func TestTransformRefusesUnknownMountTypes(t *testing.T) {
	raw := []byte(`{"services":{"app":{"image":"x","volumes":[{"type":"npipe","source":"//./pipe/docker_engine","target":"/pipe"}]}}}`)
	if _, err := transformCompose(raw, "r", "composedrill-r", t.TempDir(), t.TempDir(), nil); err == nil {
		t.Fatal("expected non-bind/volume/tmpfs mount refusal")
	}
}

func TestTransformDisablesDaemonSideServiceLogging(t *testing.T) {
	raw := []byte(`{"services":{"app":{"image":"x","logging":{"driver":"syslog","options":{"syslog-address":"tcp://prod.example:514"}}}}}`)
	tr, err := transformCompose(raw, "r", "composedrill-r", t.TempDir(), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(tr.JSON, &doc); err != nil {
		t.Fatal(err)
	}
	services := doc["services"].(map[string]any)
	app := services["app"].(map[string]any)
	logging := app["logging"].(map[string]any)
	if logging["driver"] != "none" || len(logging) != 1 {
		t.Fatalf("recovery logging driver was not neutralized: %#v", logging)
	}
}

func TestTransformRefusesUnsafeServiceIdentifier(t *testing.T) {
	if _, err := transformCompose([]byte(`{"services":{"--help":{"image":"x"}}}`), "r", "composedrill-r", t.TempDir(), t.TempDir(), nil); err == nil {
		t.Fatal("expected CLI-unsafe service name refusal")
	}
}
