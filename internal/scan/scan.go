package scan

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/maliroq/composedrill/internal/compose"
	"github.com/maliroq/composedrill/internal/model"
)

func Run(root string) (model.ScanResult, error) {
	projects, err := compose.Discover(root)
	if err != nil {
		return model.ScanResult{}, err
	}
	abs, _ := filepath.Abs(root)
	r := model.ScanResult{Root: abs, GeneratedAt: time.Now().UTC(), Projects: projects}
	pinned, totalImages, health := 0, 0, 0
	for _, p := range projects {
		for _, u := range p.UnsupportedYAML {
			r.Findings = append(r.Findings, model.Finding{Severity: "WARN", Project: p.Name, Code: "yaml-advanced", Message: u, Remedy: "Run on a host with Docker Compose so `docker compose config` can normalize advanced YAML before rehearsal."})
		}
		for v, risk := range p.TopLevelVolumeRisk {
			r.Findings = append(r.Findings, model.Finding{Severity: "HIGH", Project: p.Name, Code: "volume-isolation-risk", Message: fmt.Sprintf("volume %s uses %s", v, risk), Remedy: "ComposeDrill rewrites this to a disposable volume during rehearsal; never restore into the production volume."})
		}
		for _, s := range p.Services {
			r.ServiceCount++
			if s.Healthcheck {
				health++
			} else {
				r.Findings = append(r.Findings, model.Finding{Severity: "WARN", Project: p.Name, Service: s.Name, Code: "no-healthcheck", Message: "service has no Docker healthcheck", Remedy: "Add a Docker healthcheck or a ComposeDrill HTTP/TCP/command/file check."})
			}
			if s.Image != "" {
				totalImages++
				if compose.IsPinnedImage(s.Image) {
					pinned++
				} else {
					r.UnpinnedImages++
					r.Findings = append(r.Findings, model.Finding{Severity: "WARN", Project: p.Name, Service: s.Name, Code: "image-unpinned", Message: "image is not digest-pinned: " + s.Image, Remedy: "Pin the image by digest so a future restore cannot silently test different image bytes."})
				}
			}
			if s.Database != "" {
				r.DatabaseCount++
				r.Findings = append(r.Findings, model.Finding{Severity: "INFO", Project: p.Name, Service: s.Name, Code: "database-detected", Message: fmt.Sprintf("detected %s; preferred recovery strategy: %s", s.Database, s.DatabaseStrategy)})
			}
			for _, m := range s.Mounts {
				if m.Type == "volume" && m.Source == "" {
					r.Findings = append(r.Findings, model.Finding{Severity: "WARN", Project: p.Name, Service: s.Name, Code: "anonymous-volume", Message: "anonymous volume mounted at " + m.Target, Remedy: "Prefer a named volume or map the recovery artifact explicitly by service and target."})
				}
				if m.Type == "bind" && m.Source != "" {
					if _, err := os.Stat(m.Source); err != nil {
						r.Findings = append(r.Findings, model.Finding{Severity: "HIGH", Project: p.Name, Service: s.Name, Code: "bind-source-missing", Message: "bind source is missing: " + safePath(m.Source, p.Dir), Remedy: "Recover or document this path; a missing production bind is a recovery dependency."})
					}
				}
			}
			for _, ef := range s.EnvFiles {
				if _, err := os.Stat(ef); err != nil {
					r.Findings = append(r.Findings, model.Finding{Severity: "HIGH", Project: p.Name, Service: s.Name, Code: "env-file-missing", Message: "referenced env_file is missing: " + safePath(ef, p.Dir), Remedy: "Include the environment file or an explicit secret-recovery procedure in the recovery bundle."})
				}
			}
			for _, rf := range s.RiskFlags {
				r.Findings = append(r.Findings, model.Finding{Severity: "HIGH", Project: p.Name, Service: s.Name, Code: "unsafe-runtime-mode", Message: "service uses " + rf, Remedy: "Automatic rehearsal will refuse host-level/privileged runtime modes unless they can be safely neutralized."})
			}
		}
		r.NamedVolumeCount += len(p.NamedVolumes)
		r.BindMountCount += len(p.BindMounts)
	}
	r.HealthcheckCount = health
	// Readiness is intentionally not a restore-success score. It measures whether the static state is recoverable enough to attempt a drill.
	score := 30
	if len(projects) > 0 {
		score += 10
	}
	if r.ServiceCount > 0 {
		score += int(20 * float64(health) / float64(r.ServiceCount))
	}
	if totalImages > 0 {
		score += int(15 * float64(pinned) / float64(totalImages))
	} else {
		score += 5
	}
	if r.DatabaseCount == 0 {
		score += 10
	} else {
		known := 0
		for _, p := range projects {
			for _, s := range p.Services {
				if s.Database != "" && s.DatabaseStrategy != "" {
					known++
				}
			}
		}
		score += int(10 * float64(known) / float64(r.DatabaseCount))
	}
	high := 0
	for _, f := range r.Findings {
		if f.Severity == "HIGH" {
			high++
		}
	}
	score -= min(high*5, 25)
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	r.ReadinessScore = score
	sort.SliceStable(r.Findings, func(i, j int) bool {
		return severityRank(r.Findings[i].Severity) > severityRank(r.Findings[j].Severity)
	})
	return r, nil
}
func severityRank(s string) int {
	switch s {
	case "CRITICAL":
		return 4
	case "HIGH":
		return 3
	case "WARN":
		return 2
	case "INFO":
		return 1
	}
	return 0
}
func safePath(p, base string) string {
	rel, err := filepath.Rel(base, p)
	if err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return filepath.Base(p)
}
