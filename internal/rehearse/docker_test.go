package rehearse

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maliroq/composedrill/internal/model"
)

func TestParseProbeHTTPStatusUsesFinalResponse(t *testing.T) {
	out := "  HTTP/1.1 301 Moved Permanently\n  Location: /ready\n  HTTP/1.1 204 No Content\n"
	got, err := parseProbeHTTPStatus(out)
	if err != nil {
		t.Fatal(err)
	}
	if got != 204 {
		t.Fatalf("expected final HTTP status 204, got %d", got)
	}
}

func TestParseProbeHTTPStatusRejectsMalformedOutput(t *testing.T) {
	if _, err := parseProbeHTTPStatus("not an HTTP response"); err == nil {
		t.Fatal("expected malformed probe output to fail")
	}
}

func TestHTTPCheckUsesToolOwnedInternalProbeNetwork(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "docker.log")
	writeFakeDocker(t, dir, `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$LOG"
if [ "$1" = "compose" ]; then
  shift
  while [ "$#" -gt 0 ]; do
    case "$1" in
      -f|-p|--env-file) shift 2 ;;
      *) break ;;
    esac
  done
  [ "$1" = "ps" ] && [ "$2" = "-q" ] && printf '%s\n' 'container-id' && exit 0
  exit 64
fi
if [ "$1" = "inspect" ]; then
  printf '%s\n' '[{"NetworkSettings":{"Networks":{"composedrill-run_default":{"IPAddress":"172.31.0.2","GlobalIPv6Address":""}}}}]'
  exit 0
fi
if [ "$1" = "network" ] && [ "$2" = "inspect" ]; then
  printf '%s\n' '[{"Name":"composedrill-run_default","Internal":true,"Labels":{"io.composedrill.run":"run1","io.composedrill.managed":"true"}}]'
  exit 0
fi
if [ "$1" = "run" ]; then
  printf '%s\n' '  HTTP/1.1 200 OK'
  exit 0
fi
exit 64
`)
	t.Setenv("LOG", log)
	d := testDockerCLI(dir)
	r := d.runCheck(context.Background(), model.Check{Name: "homepage", Service: "web", Type: "http", Port: 80, Path: "/", ExpectedStatus: 200, TimeoutSeconds: 2})
	if r.Status != "PASS" {
		t.Fatalf("expected PASS, got %+v", r)
	}
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	calls := string(b)
	if strings.Contains(calls, "compose port") || strings.Contains(calls, "--publish") {
		t.Fatalf("HTTP check attempted host port publication/resolution:\n%s", calls)
	}
	if !strings.Contains(calls, "run --rm --pull=never --read-only --cap-drop=ALL") || !strings.Contains(calls, "--network composedrill-run_default") {
		t.Fatalf("HTTP check did not use constrained internal-network probe container:\n%s", calls)
	}
	if !strings.Contains(calls, "io.composedrill.run=run1") || !strings.Contains(calls, "io.composedrill.managed=true") || !strings.Contains(calls, "com.docker.compose.project=composedrill-run") {
		t.Fatalf("probe container is missing cleanup ownership labels:\n%s", calls)
	}
	if !strings.Contains(calls, "http://172.31.0.2:80/") {
		t.Fatalf("probe did not target the recovery container address:\n%s", calls)
	}
}

func TestTCPCheckUsesInternalProbeNetwork(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "docker.log")
	writeFakeDocker(t, dir, `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$LOG"
if [ "$1" = "compose" ]; then
  shift
  while [ "$#" -gt 0 ]; do
    case "$1" in -f|-p|--env-file) shift 2 ;; *) break ;; esac
  done
  [ "$1" = "ps" ] && [ "$2" = "-q" ] && printf '%s\n' 'container-id' && exit 0
  exit 64
fi
if [ "$1" = "inspect" ]; then
  printf '%s\n' '[{"NetworkSettings":{"Networks":{"composedrill-run_default":{"IPAddress":"172.31.0.3"}}}}]'
  exit 0
fi
if [ "$1" = "network" ] && [ "$2" = "inspect" ]; then
  printf '%s\n' '[{"Name":"composedrill-run_default","Internal":true,"Labels":{"io.composedrill.run":"run1","io.composedrill.managed":"true"}}]'
  exit 0
fi
[ "$1" = "run" ] && exit 0
exit 64
`)
	t.Setenv("LOG", log)
	d := testDockerCLI(dir)
	r := d.runCheck(context.Background(), model.Check{Name: "socket", Service: "web", Type: "tcp", Port: 8080, TimeoutSeconds: 2})
	if r.Status != "PASS" {
		t.Fatalf("expected PASS, got %+v", r)
	}
	b, _ := os.ReadFile(log)
	if !strings.Contains(string(b), "nc -z -w 2 172.31.0.3 8080") {
		t.Fatalf("TCP check did not execute in-network probe: %s", string(b))
	}
}

func TestProbeRefusesNonInternalNetwork(t *testing.T) {
	dir := t.TempDir()
	writeFakeDocker(t, dir, `#!/bin/sh
set -eu
if [ "$1" = "compose" ]; then
  shift
  while [ "$#" -gt 0 ]; do case "$1" in -f|-p|--env-file) shift 2 ;; *) break ;; esac; done
  [ "$1" = "ps" ] && [ "$2" = "-q" ] && printf '%s\n' 'container-id' && exit 0
  exit 64
fi
if [ "$1" = "inspect" ]; then
  printf '%s\n' '[{"NetworkSettings":{"Networks":{"composedrill-run_default":{"IPAddress":"172.31.0.2"}}}}]'
  exit 0
fi
if [ "$1" = "network" ] && [ "$2" = "inspect" ]; then
  printf '%s\n' '[{"Name":"composedrill-run_default","Internal":false,"Labels":{"io.composedrill.run":"run1","io.composedrill.managed":"true"}}]'
  exit 0
fi
exit 64
`)
	d := testDockerCLI(dir)
	r := d.runCheck(context.Background(), model.Check{Name: "homepage", Service: "web", Type: "http", Port: 80, TimeoutSeconds: 2})
	if r.Status != "FAIL" || !strings.Contains(r.Reason, "failed recovery ownership/isolation verification") {
		t.Fatalf("expected isolation refusal, got %+v", r)
	}
}

func TestProbeRefusesNetworkOutsideRecoveryNamespace(t *testing.T) {
	dir := t.TempDir()
	writeFakeDocker(t, dir, `#!/bin/sh
set -eu
if [ "$1" = "compose" ]; then
  shift
  while [ "$#" -gt 0 ]; do case "$1" in -f|-p|--env-file) shift 2 ;; *) break ;; esac; done
  [ "$1" = "ps" ] && [ "$2" = "-q" ] && printf '%s\n' 'container-id' && exit 0
  exit 64
fi
if [ "$1" = "inspect" ]; then
  printf '%s\n' '[{"NetworkSettings":{"Networks":{"production_default":{"IPAddress":"172.20.0.2"}}}}]'
  exit 0
fi
exit 64
`)
	d := testDockerCLI(dir)
	r := d.runCheck(context.Background(), model.Check{Name: "homepage", Service: "web", Type: "http", Port: 80, TimeoutSeconds: 2})
	if r.Status != "FAIL" || !strings.Contains(r.Reason, "outside recovery project namespace") {
		t.Fatalf("expected production-network refusal, got %+v", r)
	}
}

func TestProbeRejectsMalformedContainerInspect(t *testing.T) {
	dir := t.TempDir()
	writeFakeDocker(t, dir, `#!/bin/sh
set -eu
if [ "$1" = "compose" ]; then
  shift
  while [ "$#" -gt 0 ]; do case "$1" in -f|-p|--env-file) shift 2 ;; *) break ;; esac; done
  [ "$1" = "ps" ] && [ "$2" = "-q" ] && printf '%s\n' 'container-id' && exit 0
  exit 64
fi
[ "$1" = "inspect" ] && printf '%s\n' '{not-json' && exit 0
exit 64
`)
	d := testDockerCLI(dir)
	r := d.runCheck(context.Background(), model.Check{Name: "homepage", Service: "web", Type: "http", Port: 80, TimeoutSeconds: 2})
	if r.Status != "FAIL" || !strings.Contains(r.Reason, "cannot inspect recovery network") {
		t.Fatalf("expected malformed inspect failure, got %+v", r)
	}
}

func TestHTTPCheckRequiresExpectedStatus(t *testing.T) {
	dir := t.TempDir()
	writeFakeDocker(t, dir, `#!/bin/sh
set -eu
if [ "$1" = "compose" ]; then
  shift
  while [ "$#" -gt 0 ]; do case "$1" in -f|-p|--env-file) shift 2 ;; *) break ;; esac; done
  [ "$1" = "ps" ] && [ "$2" = "-q" ] && printf '%s\n' 'container-id' && exit 0
  exit 64
fi
if [ "$1" = "inspect" ]; then printf '%s\n' '[{"NetworkSettings":{"Networks":{"composedrill-run_default":{"IPAddress":"172.31.0.2"}}}}]'; exit 0; fi
if [ "$1" = "network" ] && [ "$2" = "inspect" ]; then printf '%s\n' '[{"Name":"composedrill-run_default","Internal":true,"Labels":{"io.composedrill.run":"run1","io.composedrill.managed":"true"}}]'; exit 0; fi
[ "$1" = "run" ] && printf '%s\n' '  HTTP/1.1 503 Service Unavailable' && exit 1
exit 64
`)
	d := testDockerCLI(dir)
	r := d.runCheck(context.Background(), model.Check{Name: "homepage", Service: "web", Type: "http", Port: 80, ExpectedStatus: 200, TimeoutSeconds: 2})
	if r.Status != "FAIL" || !strings.Contains(r.Reason, "HTTP status 503, expected 200") {
		t.Fatalf("expected semantic HTTP failure, got %+v", r)
	}
}

func TestEnsureProbeImagePullsPinnedImageWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "docker.log")
	writeFakeDocker(t, dir, `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$LOG"
if [ "$1" = "image" ] && [ "$2" = "inspect" ]; then exit 1; fi
if [ "$1" = "pull" ]; then exit 0; fi
exit 64
`)
	t.Setenv("LOG", log)
	d := testDockerCLI(dir)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := d.ensureUtilityImage(ctx); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(log)
	calls := string(b)
	if !strings.Contains(calls, "pull "+utilityImage) {
		t.Fatalf("expected pinned probe image pull, got %s", calls)
	}
}

func TestDockerRunIncludesExecutionErrorWhenStderrEmpty(t *testing.T) {
	dir := t.TempDir()
	writeFakeDocker(t, dir, "#!/bin/sh\nexit 64\n")
	d := testDockerCLI(dir)
	_, err := d.run(context.Background(), "ps", "-q", "web")
	if err == nil || !strings.Contains(err.Error(), "exit status 64") {
		t.Fatalf("expected execution error when stderr is empty, got %v", err)
	}
}

func writeFakeDocker(t *testing.T, dir, script string) {
	t.Helper()
	docker := filepath.Join(dir, "docker")
	if err := os.WriteFile(docker, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func testDockerCLI(dir string) dockerCLI {
	path := dir + string(os.PathListSeparator) + os.Getenv("PATH")
	env := safeDockerEnv()
	found := false
	for i := range env {
		if strings.HasPrefix(env[i], "PATH=") {
			env[i] = "PATH=" + path
			found = true
		}
	}
	if !found {
		env = append(env, "PATH="+path)
	}
	if v := os.Getenv("LOG"); v != "" {
		env = append(env, "LOG="+v)
	}
	return dockerCLI{composeFile: "recovery.compose.json", project: "composedrill-run", runID: "run1", env: env}
}

func TestVolumeRestoreRequiresUtilityImage(t *testing.T) {
	if !needsUtilityImage(nil, model.BackupManifest{Entries: []model.BackupEntry{{Kind: "volume"}}}) {
		t.Fatal("volume restore must ensure the pinned utility image")
	}
	if needsUtilityImage(nil, model.BackupManifest{}) {
		t.Fatal("utility image should not be required without network probes or volume restores")
	}
}

func TestParsePSStateSeparatesRunningFromUnhealthy(t *testing.T) {
	rows := `[{"State":"running","Health":"unhealthy"},{"State":"running","Health":"healthy"}]`
	if got := parsePSState(rows); got != "running" {
		t.Fatalf("unhealthy health must not rewrite running state, got %q", got)
	}
	state, health := parsePS(rows)
	if state != "running" || health != "unhealthy" {
		t.Fatalf("health aggregation changed unexpectedly: state=%q health=%q", state, health)
	}
}

func TestCommandCheckRequiresEveryRunningReplica(t *testing.T) {
	dir := t.TempDir()
	writeFakeDocker(t, dir, `#!/bin/sh
set -eu
if [ "$1" = "compose" ]; then
  shift
  while [ "$#" -gt 0 ]; do case "$1" in -f|-p|--env-file) shift 2 ;; *) break ;; esac; done
  [ "$1" = "ps" ] && [ "$2" = "-q" ] && printf '%s\n' 'bbbbbbbbbbbb2222' 'aaaaaaaaaaaa1111' && exit 0
  exit 64
fi
if [ "$1" = "exec" ] && [ "$2" = "aaaaaaaaaaaa1111" ]; then printf '%s\n' 'marker'; exit 0; fi
if [ "$1" = "exec" ] && [ "$2" = "bbbbbbbbbbbb2222" ]; then printf '%s\n' 'wrong'; exit 0; fi
exit 64
`)
	d := testDockerCLI(dir)
	r := d.runCheck(context.Background(), model.Check{Name: "semantic", Service: "web", Type: "command", Command: []string{"printf", "marker"}, ExpectContains: "marker", TimeoutSeconds: 2})
	if r.Status != "FAIL" || !strings.Contains(r.Reason, "bbbbbbbbbbbb") {
		t.Fatalf("expected second replica semantic failure, got %+v", r)
	}
}

func TestDockerAvailableIncludesExecutionErrorWhenOutputEmpty(t *testing.T) {
	dir := t.TempDir()
	writeFakeDocker(t, dir, "#!/bin/sh\nexit 64\n")
	if err := dockerAvailable(context.Background()); err == nil || !strings.Contains(err.Error(), "exit status 64") {
		t.Fatalf("expected execution detail, got %v", err)
	}
}
