package rehearse

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/maliroq/composedrill/internal/redact"
)

type inspectLabels struct {
	Name   string            `json:"Name"`
	Labels map[string]string `json:"Labels"`
	Config *struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config,omitempty"`
	NetworkSettings any `json:"NetworkSettings,omitempty"`
}

func CleanupRun(ctx context.Context, runID, project string) error {
	if runID == "" || project == "" || !strings.HasPrefix(project, "composedrill-") {
		return fmt.Errorf("refusing cleanup without a valid ComposeDrill run identity")
	}
	ids, err := dockerList(ctx, []string{"ps", "-aq", "--filter", "label=io.composedrill.run=" + runID})
	if err != nil {
		return err
	}
	for _, id := range ids {
		c := exec.CommandContext(ctx, "docker", "inspect", id)
		c.Env = safeDockerEnv()
		b, err := c.Output()
		if err != nil {
			return err
		}
		var rows []map[string]any
		if err := json.Unmarshal(b, &rows); err != nil || len(rows) == 0 {
			return fmt.Errorf("cannot verify container %s ownership", id)
		}
		cfg, _ := rows[0]["Config"].(map[string]any)
		labels := stringLabels(cfg["Labels"])
		if labels["io.composedrill.run"] != runID || labels["io.composedrill.managed"] != "true" || labels["com.docker.compose.project"] != project {
			return fmt.Errorf("refusing cleanup: container %s failed ownership verification", id)
		}
	}
	if len(ids) > 0 {
		args := append([]string{"rm", "-f"}, ids...)
		c := exec.CommandContext(ctx, "docker", args...)
		c.Env = safeDockerEnv()
		if b, err := c.CombinedOutput(); err != nil {
			return fmt.Errorf("remove recovery containers: %s", redact.Line(strings.TrimSpace(string(b))))
		}
	}
	vols, err := dockerList(ctx, []string{"volume", "ls", "-q", "--filter", "label=io.composedrill.run=" + runID})
	if err != nil {
		return err
	}
	for _, v := range vols {
		if !strings.HasPrefix(v, project+"_") {
			return fmt.Errorf("refusing cleanup: volume %s is outside project namespace", v)
		}
		if err := verifyObjectLabels(ctx, "volume", v, runID); err != nil {
			return err
		}
	}
	if len(vols) > 0 {
		args := append([]string{"volume", "rm"}, vols...)
		c := exec.CommandContext(ctx, "docker", args...)
		c.Env = safeDockerEnv()
		if b, err := c.CombinedOutput(); err != nil {
			return fmt.Errorf("remove recovery volumes: %s", redact.Line(strings.TrimSpace(string(b))))
		}
	}
	nets, err := dockerList(ctx, []string{"network", "ls", "-q", "--filter", "label=io.composedrill.run=" + runID})
	if err != nil {
		return err
	}
	for _, n := range nets {
		c := exec.CommandContext(ctx, "docker", "network", "inspect", n)
		c.Env = safeDockerEnv()
		b, err := c.Output()
		if err != nil {
			return err
		}
		var rows []map[string]any
		if json.Unmarshal(b, &rows) != nil || len(rows) == 0 {
			return fmt.Errorf("cannot verify network %s", n)
		}
		name := fmt.Sprint(rows[0]["Name"])
		if !strings.HasPrefix(name, project+"_") {
			return fmt.Errorf("refusing cleanup: network %s is outside project namespace", name)
		}
		labels := stringLabels(rows[0]["Labels"])
		if labels["io.composedrill.run"] != runID || labels["io.composedrill.managed"] != "true" {
			return fmt.Errorf("refusing cleanup: network %s failed ownership verification", name)
		}
	}
	if len(nets) > 0 {
		args := append([]string{"network", "rm"}, nets...)
		c := exec.CommandContext(ctx, "docker", args...)
		c.Env = safeDockerEnv()
		if b, err := c.CombinedOutput(); err != nil {
			return fmt.Errorf("remove recovery networks: %s", redact.Line(strings.TrimSpace(string(b))))
		}
	}
	return nil
}
func dockerList(ctx context.Context, args []string) ([]string, error) {
	c := exec.CommandContext(ctx, "docker", args...)
	c.Env = safeDockerEnv()
	b, err := c.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker %s: %s", strings.Join(args, " "), redact.Line(strings.TrimSpace(string(b))))
	}
	out := []string{}
	for _, s := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out, nil
}
func verifyObjectLabels(ctx context.Context, kind, name, runID string) error {
	c := exec.CommandContext(ctx, "docker", kind, "inspect", name)
	c.Env = safeDockerEnv()
	b, err := c.Output()
	if err != nil {
		return err
	}
	var rows []map[string]any
	if json.Unmarshal(b, &rows) != nil || len(rows) == 0 {
		return fmt.Errorf("cannot verify %s %s", kind, name)
	}
	labels := stringLabels(rows[0]["Labels"])
	if labels["io.composedrill.run"] != runID || labels["io.composedrill.managed"] != "true" {
		return fmt.Errorf("refusing cleanup: %s %s failed ownership verification", kind, name)
	}
	return nil
}
func stringLabels(v any) map[string]string {
	out := map[string]string{}
	switch m := v.(type) {
	case map[string]any:
		for k, x := range m {
			out[k] = fmt.Sprint(x)
		}
	case map[string]string:
		return m
	}
	return out
}

func CleanupWorkspace(root, runID string) error {
	if runID == "" || strings.Contains(runID, string(os.PathSeparator)) || strings.Contains(runID, "..") {
		return fmt.Errorf("refusing workspace cleanup without a valid ComposeDrill run ID")
	}
	base, err := filepath.Abs(filepath.Join(root, ".composedrill", "runs"))
	if err != nil {
		return err
	}
	target, err := filepath.Abs(filepath.Join(base, runID))
	if err != nil {
		return err
	}
	if target == base || !strings.HasPrefix(target, base+string(os.PathSeparator)) {
		return fmt.Errorf("refusing workspace cleanup outside ComposeDrill run directory")
	}
	return os.RemoveAll(target)
}

func ProjectNameForRun(runID string) string {
	p := "composedrill-" + strings.ToLower(strings.ReplaceAll(runID, "T", "-"))
	return strings.ReplaceAll(p, "Z", "")
}
