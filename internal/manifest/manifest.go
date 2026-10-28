package manifest

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/maliroq/composedrill/internal/model"
)

type RecoveryManifest struct {
	Version              int             `json:"version"`
	GeneratedAt          time.Time       `json:"generated_at"`
	ToolVersion          string          `json:"tool_version"`
	Projects             []model.Project `json:"projects"`
	ExpectedChecks       []ExpectedCheck `json:"expected_checks"`
	RecoveryInstructions []string        `json:"recovery_instructions"`
	Limitations          []string        `json:"limitations"`
}
type ExpectedCheck struct {
	Project string `json:"project"`
	Service string `json:"service"`
	Type    string `json:"type"`
	Status  string `json:"status"`
}

func Build(r model.ScanResult, version string) RecoveryManifest {
	projects := make([]model.Project, 0, len(r.Projects))
	for _, p := range r.Projects {
		cp := p
		cp.Dir = portablePath(r.Root, p.Dir)
		cp.ComposeFile = portablePath(r.Root, p.ComposeFile)
		cp.BindMounts = append([]model.Mount(nil), p.BindMounts...)
		for i := range cp.BindMounts {
			cp.BindMounts[i].Source = portablePath(r.Root, cp.BindMounts[i].Source)
		}
		cp.Services = append([]model.Service(nil), p.Services...)
		for i := range cp.Services {
			cp.Services[i].Mounts = append([]model.Mount(nil), p.Services[i].Mounts...)
			for j := range cp.Services[i].Mounts {
				if cp.Services[i].Mounts[j].Type == "bind" {
					cp.Services[i].Mounts[j].Source = portablePath(r.Root, cp.Services[i].Mounts[j].Source)
				}
			}
			cp.Services[i].EnvFiles = append([]string(nil), p.Services[i].EnvFiles...)
			for j := range cp.Services[i].EnvFiles {
				cp.Services[i].EnvFiles[j] = portablePath(r.Root, cp.Services[i].EnvFiles[j])
			}
		}
		projects = append(projects, cp)
	}
	m := RecoveryManifest{
		Version:     1,
		GeneratedAt: time.Now().UTC(),
		ToolVersion: version,
		Projects:    projects,
		RecoveryInstructions: []string{
			"Stage a recovery bundle containing the checksummed Compose file and every required persistent-state artifact.",
			"Run composedrill rehearse --backup <bundle> --dry-run <root> and resolve every refusal or MANUAL CHECK REQUIRED state.",
			"Run the real rehearsal on a Docker host; require startup plus Docker healthchecks or meaningful explicit checks before accepting PASS.",
			"Preserve the evidence JSON and investigate any cleanup failure before the next drill.",
		},
		Limitations: []string{
			"Image digests are recorded only when already present in Compose; runtime digest discovery requires Docker.",
			"Backup coverage is UNKNOWN until a recovery bundle maps persistent state to checksummed artifacts.",
			"A service without a Docker healthcheck or user-defined check cannot be reported as recovered automatically.",
			"Profile-gated services are not activated automatically in v0.1 and remain manual.",
		},
	}
	for _, p := range projects {
		for _, s := range p.Services {
			st := "MANUAL CHECK REQUIRED"
			typ := "manual"
			if len(s.Profiles) > 0 {
				typ = "compose-profile"
			} else if s.Healthcheck {
				st = "EXPECTED"
				typ = "docker-healthcheck"
			}
			m.ExpectedChecks = append(m.ExpectedChecks, ExpectedCheck{Project: p.Name, Service: s.Name, Type: typ, Status: st})
		}
	}
	return m
}

func portablePath(root, p string) string {
	if p == "" || !filepath.IsAbs(p) {
		return p
	}
	rel, err := filepath.Rel(root, p)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		if rel == "." {
			return "."
		}
		return filepath.ToSlash(rel)
	}
	return p
}
