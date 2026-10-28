package rehearse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/maliroq/composedrill/internal/model"
	"github.com/maliroq/composedrill/internal/redact"
)

type dockerCLI struct {
	composeFile, project string
	runID                string
	envFile              string
	env                  []string
}

func (d dockerCLI) cmd(ctx context.Context, args ...string) *exec.Cmd {
	base := []string{"compose"}
	if d.envFile != "" {
		base = append(base, "--env-file", d.envFile)
	}
	base = append(base, "-f", d.composeFile, "-p", d.project)
	base = append(base, args...)
	c := exec.CommandContext(ctx, "docker", base...)
	c.Env = d.env
	return c
}
func (d dockerCLI) run(ctx context.Context, args ...string) (string, error) {
	c := d.cmd(ctx, args...)
	var out, er bytes.Buffer
	c.Stdout = &out
	c.Stderr = &er
	err := c.Run()
	if err != nil {
		action := "command"
		if len(args) > 0 {
			action = args[0]
		}
		detail := strings.TrimSpace(er.String())
		if detail == "" {
			detail = err.Error()
		}
		return out.String(), fmt.Errorf("docker compose %s failed: %s", action, redact.Line(detail))
	}
	return out.String(), nil
}
func safeDockerEnv() []string {
	keys := []string{"PATH", "HOME", "DOCKER_HOST", "DOCKER_CONTEXT", "XDG_RUNTIME_DIR", "TMPDIR"}
	out := []string{}
	for _, k := range keys {
		if v, ok := os.LookupEnv(k); ok {
			out = append(out, k+"="+v)
		}
	}
	return out
}
func resolveCompose(ctx context.Context, file, envFile string) ([]byte, error) {
	args := []string{"compose"}
	if envFile != "" {
		args = append(args, "--env-file", envFile)
	}
	args = append(args, "-f", file, "config", "--no-interpolate", "--no-env-resolution", "--format", "json")
	c := exec.CommandContext(ctx, "docker", args...)
	c.Env = safeDockerEnv()
	var out, er bytes.Buffer
	c.Stdout = &out
	c.Stderr = &er
	if err := c.Run(); err != nil {
		detail := strings.TrimSpace(er.String())
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("docker compose config failed: %s", redact.Line(detail))
	}
	return out.Bytes(), nil
}
func dockerAvailable(ctx context.Context) error {
	c := exec.CommandContext(ctx, "docker", "compose", "version")
	c.Env = safeDockerEnv()
	if b, err := c.CombinedOutput(); err != nil {
		detail := strings.TrimSpace(string(b))
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("Docker Compose v2 is required for rehearsal: %s", redact.Line(detail))
	}
	return nil
}
func (d dockerCLI) waitService(ctx context.Context, service string, timeout time.Duration) (string, string) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := d.run(ctx, "ps", "-a", "--format", "json", service)
		if err == nil {
			state, health := parsePS(out)
			if state == "running" {
				if health == "" || health == "healthy" {
					return "PASS", health
				}
				if health == "unhealthy" {
					return "FAIL", health
				}
			}
			if state == "exited" || state == "dead" {
				return "FAIL", state
			}
		}
		select {
		case <-ctx.Done():
			return "FAIL", "interrupted"
		case <-time.After(1 * time.Second):
		}
	}
	return "FAIL", "startup timeout"
}

func (d dockerCLI) waitStarted(ctx context.Context, service string, timeout time.Duration) (string, string) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := d.run(ctx, "ps", "-a", "--format", "json", service)
		if err == nil {
			state := parsePSState(out)
			switch state {
			case "running":
				return "PASS", ""
			case "exited", "dead":
				return "FAIL", state
			}
		}
		select {
		case <-ctx.Done():
			return "FAIL", "interrupted"
		case <-time.After(1 * time.Second):
		}
	}
	return "FAIL", "startup timeout"
}

func (d dockerCLI) waitHealthy(ctx context.Context, service string, timeout time.Duration) (string, string) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := d.run(ctx, "ps", "-a", "--format", "json", service)
		if err == nil {
			state, health := parsePS(out)
			if state == "exited" || state == "dead" {
				return "FAIL", state
			}
			if health == "healthy" {
				return "PASS", ""
			}
			if health == "unhealthy" {
				return "FAIL", "unhealthy"
			}
		}
		select {
		case <-ctx.Done():
			return "FAIL", "interrupted"
		case <-time.After(1 * time.Second):
		}
	}
	return "FAIL", "health timeout"
}

func parsePSState(s string) string {
	rows := parsePSRows(s)
	if len(rows) == 0 {
		return ""
	}
	allRunning := true
	for _, row := range rows {
		state := strings.ToLower(fmt.Sprint(row["State"]))
		if state == "exited" || state == "dead" {
			return state
		}
		if state != "running" {
			allRunning = false
		}
	}
	if allRunning {
		return "running"
	}
	return "starting"
}
func parsePSRows(s string) []map[string]any {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var rows []map[string]any
	if strings.HasPrefix(s, "[") {
		_ = json.Unmarshal([]byte(s), &rows)
		return rows
	}
	for _, line := range strings.Split(s, "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil {
			rows = append(rows, m)
		}
	}
	return rows
}

func parsePS(s string) (string, string) {
	rows := parsePSRows(s)
	if len(rows) == 0 {
		return "", ""
	}
	allRunning := true
	anyStarting := false
	for _, row := range rows {
		state := strings.ToLower(fmt.Sprint(row["State"]))
		health := strings.ToLower(fmt.Sprint(row["Health"]))
		if health == "<nil>" {
			health = ""
		}
		if state == "exited" || state == "dead" {
			return state, health
		}
		if health == "unhealthy" {
			return state, health
		}
		if state != "running" {
			allRunning = false
		}
		if health != "" && health != "healthy" {
			anyStarting = true
		}
	}
	if allRunning && !anyStarting {
		return "running", "healthy"
	}
	if allRunning {
		return "running", "starting"
	}
	return "starting", ""
}

const utilityImage = "busybox@sha256:fd7dc98638c8e305f4dc34e979f1c0fdfdcaeb0fbf8fcff77ae834b6da3d7e6e"

var httpStatusRE = regexp.MustCompile(`HTTP/[0-9.]+[[:space:]]+([0-9]{3})`)

func needsNetworkProbe(checks []model.Check) bool {
	for _, c := range checks {
		if c.Type == "http" || c.Type == "tcp" {
			return true
		}
	}
	return false
}

func needsUtilityImage(checks []model.Check, manifest model.BackupManifest) bool {
	if needsNetworkProbe(checks) {
		return true
	}
	for _, e := range manifest.Entries {
		if e.Kind == "volume" {
			return true
		}
	}
	return false
}

func (d dockerCLI) ensureUtilityImage(ctx context.Context) error {
	if _, err := d.runDocker(ctx, "image", "inspect", utilityImage); err == nil {
		return nil
	}
	if _, err := d.runDocker(ctx, "pull", utilityImage); err != nil {
		return fmt.Errorf("required network-probe image is unavailable: %w", err)
	}
	return nil
}

func (d dockerCLI) runCheck(ctx context.Context, c model.Check) model.CheckResult {
	start := time.Now()
	r := model.CheckResult{Name: c.Name, Service: c.Service, Type: c.Type, Status: "FAIL"}
	timeout := time.Duration(c.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	cc, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := validateCheck(c); err != nil {
		r.DurationMS = time.Since(start).Milliseconds()
		r.Reason = redact.Line(err.Error())
		return r
	}
	ids, err := d.serviceContainerIDs(cc, c.Service)
	if err != nil {
		r.DurationMS = time.Since(start).Milliseconds()
		r.Reason = redact.Line(err.Error())
		return r
	}
	for _, id := range ids {
		if err := d.runCheckOnContainer(cc, c, id, timeout); err != nil {
			r.DurationMS = time.Since(start).Milliseconds()
			r.Reason = redact.Line(fmt.Sprintf("replica %s: %v", shortID(id), err))
			return r
		}
	}
	r.DurationMS = time.Since(start).Milliseconds()
	r.Status = "PASS"
	return r
}

func validateCheck(c model.Check) error {
	switch c.Type {
	case "http", "tcp":
		if c.Port <= 0 || c.Port > 65535 {
			return fmt.Errorf("invalid target port %d", c.Port)
		}
	case "command":
		if len(c.Command) == 0 {
			return fmt.Errorf("empty command")
		}
	case "file-exists":
		if c.File == "" {
			return fmt.Errorf("missing file path")
		}
	case "dns":
		if c.Host == "" {
			return fmt.Errorf("missing host")
		}
	default:
		return fmt.Errorf("unsupported check type %s", c.Type)
	}
	return nil
}

func (d dockerCLI) serviceContainerIDs(ctx context.Context, service string) ([]string, error) {
	out, err := d.run(ctx, "ps", "-q", service)
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(out)
	if len(ids) == 0 {
		return nil, fmt.Errorf("service %s has no running recovery container", service)
	}
	sort.Strings(ids)
	return ids, nil
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func (d dockerCLI) runCheckOnContainer(ctx context.Context, c model.Check, containerID string, timeout time.Duration) error {
	switch c.Type {
	case "http":
		networkName, targetIP, err := d.probeTargetForContainer(ctx, c.Service, containerID)
		if err != nil {
			return err
		}
		path := c.Path
		if path == "" {
			path = "/"
		}
		url := "http://" + net.JoinHostPort(targetIP, strconv.Itoa(c.Port)) + path
		probeTimeout := int(timeout / time.Second)
		if probeTimeout < 1 {
			probeTimeout = 1
		}
		out, probeErr := d.runProbe(ctx, networkName, "wget", "-S", "-O", "/dev/null", "-T", strconv.Itoa(probeTimeout), "-Y", "off", url)
		status, statusErr := parseProbeHTTPStatus(string(out))
		if statusErr != nil {
			if probeErr != nil {
				return fmt.Errorf("HTTP probe failed before a response status was observed: %w", probeErr)
			}
			return statusErr
		}
		want := c.ExpectedStatus
		if want == 0 {
			want = 200
		}
		if status != want {
			return fmt.Errorf("HTTP status %d, expected %d", status, want)
		}
		return nil
	case "tcp":
		networkName, targetIP, err := d.probeTargetForContainer(ctx, c.Service, containerID)
		if err != nil {
			return err
		}
		probeTimeout := int(timeout / time.Second)
		if probeTimeout < 1 {
			probeTimeout = 1
		}
		_, err = d.runProbe(ctx, networkName, "nc", "-z", "-w", strconv.Itoa(probeTimeout), targetIP, strconv.Itoa(c.Port))
		return err
	case "command":
		args := append([]string{"exec", containerID}, c.Command...)
		out, err := d.runDocker(ctx, args...)
		if err == nil && c.ExpectContains != "" && !strings.Contains(out, c.ExpectContains) {
			return fmt.Errorf("command output did not contain expected marker")
		}
		return err
	case "file-exists":
		_, err := d.runDocker(ctx, "exec", containerID, "test", "-e", c.File)
		return err
	case "dns":
		_, err := d.runDocker(ctx, "exec", containerID, "getent", "hosts", c.Host)
		return err
	default:
		return fmt.Errorf("unsupported check type %s", c.Type)
	}
}

func (d dockerCLI) runDocker(ctx context.Context, args ...string) (string, error) {
	c := exec.CommandContext(ctx, "docker", args...)
	c.Env = d.env
	var out, er bytes.Buffer
	c.Stdout = &out
	c.Stderr = &er
	if err := c.Run(); err != nil {
		action := "command"
		if len(args) > 0 {
			action = args[0]
		}
		detail := strings.TrimSpace(er.String())
		if detail == "" {
			detail = err.Error()
		}
		return out.String(), fmt.Errorf("docker %s failed: %s", action, redact.Line(detail))
	}
	return out.String(), nil
}

type probeContainerInspect struct {
	NetworkSettings struct {
		Networks map[string]struct {
			IPAddress         string `json:"IPAddress"`
			GlobalIPv6Address string `json:"GlobalIPv6Address"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
}

type probeNetworkInspect struct {
	Name     string            `json:"Name"`
	Internal bool              `json:"Internal"`
	Labels   map[string]string `json:"Labels"`
}

func (d dockerCLI) probeTarget(ctx context.Context, service string) (containerID, networkName, targetIP string, err error) {
	ids, err := d.serviceContainerIDs(ctx, service)
	if err != nil {
		return "", "", "", err
	}
	containerID = ids[0]
	networkName, targetIP, err = d.probeTargetForContainer(ctx, service, containerID)
	return containerID, networkName, targetIP, err
}

func (d dockerCLI) probeTargetForContainer(ctx context.Context, service, containerID string) (networkName, targetIP string, err error) {
	out, err := d.runDocker(ctx, "inspect", "--type", "container", containerID)
	if err != nil {
		return "", "", err
	}
	var rows []probeContainerInspect
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &rows); err != nil || len(rows) != 1 {
		if err == nil {
			err = fmt.Errorf("unexpected container inspect result count %d", len(rows))
		}
		return "", "", fmt.Errorf("cannot inspect recovery network for service %s: %w", service, err)
	}
	names := make([]string, 0, len(rows[0].NetworkSettings.Networks))
	for name := range rows[0].NetworkSettings.Networks {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		n := rows[0].NetworkSettings.Networks[name]
		ip := strings.TrimSpace(n.IPAddress)
		if ip == "" {
			ip = strings.TrimSpace(n.GlobalIPv6Address)
		}
		if net.ParseIP(ip) == nil {
			continue
		}
		if err := d.verifyRecoveryNetwork(ctx, name); err != nil {
			return "", "", err
		}
		return name, ip, nil
	}
	return "", "", fmt.Errorf("service %s has no usable recovery-network address", service)
}

func (d dockerCLI) verifyRecoveryNetwork(ctx context.Context, name string) error {
	if d.runID == "" || d.project == "" {
		return fmt.Errorf("refusing network probe without a valid ComposeDrill run identity")
	}
	if !strings.HasPrefix(name, d.project+"_") {
		return fmt.Errorf("refusing probe on network %q outside recovery project namespace", name)
	}
	out, err := d.runDocker(ctx, "network", "inspect", name)
	if err != nil {
		return err
	}
	var rows []probeNetworkInspect
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &rows); err != nil || len(rows) != 1 {
		if err == nil {
			err = fmt.Errorf("unexpected network inspect result count %d", len(rows))
		}
		return fmt.Errorf("cannot verify recovery network %s: %w", name, err)
	}
	n := rows[0]
	if n.Name != name || !n.Internal || n.Labels["io.composedrill.run"] != d.runID || n.Labels["io.composedrill.managed"] != "true" {
		return fmt.Errorf("refusing probe: network %s failed recovery ownership/isolation verification", name)
	}
	return nil
}

func (d dockerCLI) runProbe(ctx context.Context, networkName string, args ...string) ([]byte, error) {
	cmdArgs := []string{
		"run", "--rm", "--pull=never", "--read-only", "--cap-drop=ALL",
		"--security-opt", "no-new-privileges=true",
		"--network", networkName,
		"--label", "io.composedrill.run=" + d.runID,
		"--label", "io.composedrill.managed=true",
		"--label", "com.docker.compose.project=" + d.project,
		utilityImage,
	}
	cmdArgs = append(cmdArgs, args...)
	c := exec.CommandContext(ctx, "docker", cmdArgs...)
	c.Env = d.env
	out, err := c.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("network probe container failed: %w", err)
	}
	return out, nil
}

func parseProbeHTTPStatus(s string) (int, error) {
	matches := httpStatusRE.FindAllStringSubmatch(s, -1)
	if len(matches) == 0 {
		return 0, fmt.Errorf("HTTP probe returned no parseable response status")
	}
	last := matches[len(matches)-1]
	code, err := strconv.Atoi(last[1])
	if err != nil || code < 100 || code > 599 {
		return 0, fmt.Errorf("HTTP probe returned invalid response status")
	}
	return code, nil
}
