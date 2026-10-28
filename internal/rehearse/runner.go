package rehearse

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/maliroq/composedrill/internal/model"
	"github.com/maliroq/composedrill/internal/redact"
)

type Options struct {
	Root, Backup, ChecksFile string
	DryRun, Keep             bool
	Version                  string
}

func Run(ctx context.Context, opt Options) (model.RecoveryReport, error) {
	start := time.Now().UTC()
	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return model.RecoveryReport{}, fmt.Errorf("generate recovery run identity: %w", err)
	}
	runID := fmt.Sprintf("%s-%x", start.Format("20060102T150405Z"), suffix[:])
	project := ProjectNameForRun(runID)
	root, err := filepath.Abs(opt.Root)
	if err != nil {
		return model.RecoveryReport{}, err
	}
	stateRoot := filepath.Join(root, ".composedrill")
	workspace := filepath.Join(stateRoot, "runs", runID)
	evidenceDir := filepath.Join(stateRoot, "evidence")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		return model.RecoveryReport{}, err
	}
	if err := os.MkdirAll(evidenceDir, 0o700); err != nil {
		return model.RecoveryReport{}, err
	}
	if !opt.Keep {
		defer os.RemoveAll(workspace)
	}
	rep := model.RecoveryReport{RunID: runID, StartedAt: start, ProjectName: project, BackupPath: filepath.Base(opt.Backup), CleanupStatus: "NOT RUN", ToolVersion: opt.Version}
	bundle, err := stageBundle(opt.Backup, workspace)
	if err != nil {
		return rep, err
	}
	rep.BackupManifest = bundle.ManifestSHA
	ce := composeEntry(bundle.Manifest)
	if ce == nil {
		return rep, fmt.Errorf("recovery bundle does not contain a compose entry")
	}
	composeFile, err := safeJoin(bundle.Root, ce.Artifact)
	if err != nil {
		return rep, err
	}
	cb, err := os.ReadFile(composeFile)
	if err != nil {
		return rep, err
	}
	if err := preflightComposeSource(cb); err != nil {
		return rep, err
	}
	cs := sha256.Sum256(cb)
	rep.ComposeSHA256 = hex.EncodeToString(cs[:])
	if bundle.Manifest.ComposeSHA256 != "" && !strings.EqualFold(rep.ComposeSHA256, bundle.Manifest.ComposeSHA256) {
		return rep, fmt.Errorf("compose checksum does not match backup manifest")
	}
	checks, err := loadChecks(opt.ChecksFile, bundle.Root, bundle.Manifest)
	if err != nil {
		return rep, err
	}
	envFile, err := interpolationEnvFile(bundle.Root, filepath.Dir(composeFile), bundle.Manifest)
	if err != nil {
		return rep, err
	}
	if err := dockerAvailable(ctx); err != nil {
		return rep, err
	}
	resolved, err := resolveCompose(ctx, composeFile, envFile)
	if err != nil {
		return rep, err
	}
	tr, err := transformCompose(resolved, runID, project, workspace, bundle.Root, checks)
	if err != nil {
		return rep, err
	}
	if err := verifyDeclaredRecoveryFiles(bundle.Root, bundle.Manifest, tr.ReferencedFiles); err != nil {
		return rep, err
	}
	genFile := filepath.Join(workspace, "recovery.compose.json")
	if err := os.WriteFile(genFile, tr.JSON, 0o600); err != nil {
		return rep, err
	}
	d := dockerCLI{composeFile: genFile, project: project, runID: runID, envFile: envFile, env: safeDockerEnv()}
	if !opt.DryRun && needsUtilityImage(checks, bundle.Manifest) {
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		err := d.ensureUtilityImage(probeCtx)
		cancel()
		if err != nil {
			return rep, err
		}
	}
	cleanupDone := opt.DryRun || opt.Keep
	if !cleanupDone {
		defer func() {
			if cleanupDone {
				return
			}
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = CleanupRun(cleanupCtx, runID, project)
		}()
	}
	serviceReports := map[string]*model.ServiceRecovery{}
	for _, s := range tr.Services {
		serviceReports[s] = &model.ServiceRecovery{Project: project, Service: s, Configuration: "PASS", DataRestore: "PASS", Startup: "NOT RUN", Health: "NOT RUN"}
		if tr.Profiled[s] {
			serviceReports[s].Startup = "MANUAL CHECK REQUIRED"
			serviceReports[s].Health = "MANUAL CHECK REQUIRED"
			serviceReports[s].Reason = appendReason(serviceReports[s].Reason, "service is profile-gated; v0.1 does not activate Compose profiles automatically")
		}
	}
	// Persistent mounts require an explicit artifact mapping or a logical DB restore.
	for svc, mounts := range tr.Persistent {
		db := findEntry(bundle.Manifest, "database", svc, "", "")
		for _, m := range mounts {
			if m.Type == "bind" {
				e := findEntry(bundle.Manifest, "bind", svc, m.Target, "")
				if e == nil {
					serviceReports[svc].DataRestore = "MANUAL CHECK REQUIRED"
					serviceReports[svc].Reason = appendReason(serviceReports[svc].Reason, "no backup artifact mapped for bind target "+m.Target)
					continue
				}
				if !opt.DryRun {
					dest := tr.BindTargets[svc+"|"+m.Target]
					if err := restoreBind(bundle.Root, e, dest); err != nil {
						serviceReports[svc].DataRestore = "FAIL"
						serviceReports[svc].Reason = appendReason(serviceReports[svc].Reason, err.Error())
					}
				}
			} else if m.Type == "volume" {
				if db != nil {
					continue
				}
				e := findEntry(bundle.Manifest, "volume", svc, m.Target, "")
				if e == nil {
					e = findEntry(bundle.Manifest, "volume", "", "", m.Source)
				}
				if e == nil {
					serviceReports[svc].DataRestore = "MANUAL CHECK REQUIRED"
					serviceReports[svc].Reason = appendReason(serviceReports[svc].Reason, "no backup artifact mapped for volume target "+m.Target+" (source "+m.Source+")")
					continue
				}
				if opt.DryRun {
					continue
				}
				if _, err := d.run(ctx, "create", "--no-build"); err != nil {
					serviceReports[svc].DataRestore = "FAIL"
					serviceReports[svc].Reason = appendReason(serviceReports[svc].Reason, err.Error())
					continue
				}
				vol := tr.VolumeNames[m.Source]
				if vol == "" {
					serviceReports[svc].DataRestore = "FAIL"
					serviceReports[svc].Reason = appendReason(serviceReports[svc].Reason, "could not resolve disposable volume for "+m.Source)
					continue
				}
				if err := restoreVolume(ctx, runID, vol, bundle.Root, e); err != nil {
					serviceReports[svc].DataRestore = "FAIL"
					serviceReports[svc].Reason = appendReason(serviceReports[svc].Reason, err.Error())
				}
			}
		}
	}
	if opt.DryRun {
		for _, s := range tr.Services {
			serviceReports[s].Startup = "DRY RUN"
			serviceReports[s].Health = "DRY RUN"
		}
		rep.CleanupStatus = "SKIPPED (DRY RUN)"
		return finalize(rep, serviceReports, evidenceDir)
	}
	// Always attempt cleanup on return. The generated project name and run labels are unique and tool-owned.
	// Bring up database services first and restore logical dumps before application services start.
	for _, e := range bundle.Manifest.Entries {
		if e.Kind != "database" {
			continue
		}
		sr := serviceReports[e.Service]
		if sr == nil {
			rep.Findings = append(rep.Findings, model.Finding{Severity: "HIGH", Code: "database-service-missing", Message: "database artifact references unknown service " + e.Service})
			continue
		}
		if tr.Profiled[e.Service] {
			sr.DataRestore = "MANUAL CHECK REQUIRED"
			sr.Reason = appendReason(sr.Reason, "database restore skipped because the service is profile-gated")
			continue
		}
		if _, err := d.run(ctx, "up", "-d", "--no-build", e.Service); err != nil {
			sr.Startup = "FAIL"
			sr.DataRestore = "FAIL"
			sr.Reason = appendReason(sr.Reason, err.Error())
			continue
		}
		st, why := d.waitService(ctx, e.Service, 90*time.Second)
		if st != "PASS" {
			sr.Startup = "FAIL"
			sr.DataRestore = "FAIL"
			sr.Reason = appendReason(sr.Reason, "database did not become ready: "+why)
			continue
		}
		if err := restoreDatabase(ctx, d, bundle.Root, &e); err != nil {
			sr.DataRestore = "FAIL"
			sr.Reason = appendReason(sr.Reason, err.Error())
		}
	}
	if _, err := d.run(ctx, "up", "-d", "--no-build"); err != nil {
		rep.Findings = append(rep.Findings, model.Finding{Severity: "HIGH", Code: "compose-up-failed", Message: redact.Line(err.Error())})
	}
	for _, svc := range tr.Services {
		sr := serviceReports[svc]
		if tr.Profiled[svc] {
			continue
		}
		st, why := d.waitStarted(ctx, svc, 90*time.Second)
		sr.Startup = st
		if st != "PASS" {
			sr.Health = "FAIL"
			sr.Reason = appendReason(sr.Reason, why)
			continue
		}
		if tr.Health[svc] {
			hs, hwhy := d.waitHealthy(ctx, svc, 90*time.Second)
			sr.Health = hs
			if hs != "PASS" {
				sr.Reason = appendReason(sr.Reason, hwhy)
			}
		} else {
			sr.Health = "MANUAL CHECK REQUIRED"
			sr.Reason = appendReason(sr.Reason, "container is running but service has no active Docker healthcheck")
		}
	}
	for _, c := range checks {
		sr := serviceReports[c.Service]
		if sr == nil {
			rep.Findings = append(rep.Findings, model.Finding{Severity: "HIGH", Code: "check-service-missing", Message: "check " + c.Name + " references unknown service " + c.Service})
			continue
		}
		if tr.Profiled[c.Service] {
			sr.Checks = append(sr.Checks, model.CheckResult{Name: c.Name, Service: c.Service, Type: c.Type, Status: "MANUAL CHECK REQUIRED", Reason: "profile is not activated automatically"})
			continue
		}
		cr := d.runCheck(ctx, c)
		sr.Checks = append(sr.Checks, cr)
		if cr.Status == "FAIL" {
			sr.Health = "FAIL"
			sr.Reason = appendReason(sr.Reason, c.Name+": "+cr.Reason)
		} else if sr.Health == "MANUAL CHECK REQUIRED" {
			sr.Health = "PASS"
		}
	}
	if opt.Keep {
		rep.CleanupStatus = "SKIPPED (--keep)"
	} else {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := CleanupRun(cleanupCtx, runID, project); err != nil {
			rep.CleanupStatus = "FAIL: " + redact.Line(err.Error())
		} else {
			rep.CleanupStatus = "PASS"
			cleanupDone = true
		}
		cancel()
	}
	_ = os.Remove(genFile)
	return finalize(rep, serviceReports, evidenceDir)
}

var restrictedComposeSourceRE = regexp.MustCompile(`(?i)(?:^|[\s,{])(?:include|extends|label_file)\s*:|"(?:include|extends|label_file)"\s*:`)

func preflightComposeSource(b []byte) error {
	// These Compose features can load additional host files while `docker compose
	// config` is canonicalizing the document. Refuse them from raw source before
	// invoking Compose, including flow-style YAML/JSON where the key is mid-line.
	for _, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if m := restrictedComposeSourceRE.FindString(line); m != "" {
			key := "restricted host-file-loading key"
			lower := strings.ToLower(m)
			for _, candidate := range []string{"include", "extends", "label_file"} {
				if strings.Contains(lower, candidate) {
					key = candidate
					break
				}
			}
			return fmt.Errorf("Compose %s is not automatically rehearsed in v0.1 because it may read files outside the recovery bundle", key)
		}
	}
	return nil
}

func interpolationEnvFile(bundleRoot, composeDir string, manifest model.BackupManifest) (string, error) {
	candidate := filepath.Join(composeDir, ".env")
	if _, err := os.Stat(candidate); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	rel, err := filepath.Rel(bundleRoot, candidate)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	if !artifactDeclared(manifest, rel) {
		return "", fmt.Errorf("Compose interpolation .env exists but is not declared/checksummed in composedrill-backup.json")
	}
	if err := validatePathWithinBundle(bundleRoot, candidate); err != nil {
		return "", err
	}
	return candidate, nil
}

func verifyDeclaredRecoveryFiles(bundleRoot string, manifest model.BackupManifest, files []string) error {
	for _, f := range files {
		base, err := filepath.EvalSymlinks(bundleRoot)
		if err != nil {
			return err
		}
		real, err := filepath.EvalSymlinks(f)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(base, real)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(filepath.Clean(rel))
		if !artifactDeclared(manifest, rel) {
			return fmt.Errorf("Compose references %s but that file is not declared/checksummed in composedrill-backup.json", rel)
		}
	}
	return nil
}

func artifactDeclared(manifest model.BackupManifest, rel string) bool {
	rel = filepath.ToSlash(filepath.Clean(rel))
	for _, e := range manifest.Entries {
		if filepath.ToSlash(filepath.Clean(e.Artifact)) == rel {
			return true
		}
	}
	return false
}

func artifactDeclaredAs(manifest model.BackupManifest, rel, kind string) bool {
	rel = filepath.ToSlash(filepath.Clean(rel))
	for _, e := range manifest.Entries {
		if e.Kind == kind && filepath.ToSlash(filepath.Clean(e.Artifact)) == rel {
			return true
		}
	}
	return false
}

func loadChecks(path, bundleRoot string, manifest model.BackupManifest) ([]model.Check, error) {
	if path == "" {
		candidate := filepath.Join(bundleRoot, "composedrill-checks.json")
		if _, err := os.Stat(candidate); err == nil {
			path = candidate
		} else {
			return nil, nil
		}
	}
	if err := validatePathWithinBundle(bundleRoot, path); err != nil {
		return nil, fmt.Errorf("checks file: %w", err)
	}
	base, err := filepath.EvalSymlinks(bundleRoot)
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(base, real)
	if err != nil {
		return nil, err
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	if !artifactDeclaredAs(manifest, rel, "checks") {
		return nil, fmt.Errorf("checks file %s is not declared as a checks artifact in composedrill-backup.json", rel)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg model.UserConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	if cfg.Version != 1 {
		return nil, fmt.Errorf("unsupported checks config version")
	}
	for i := range cfg.Checks {
		if cfg.Checks[i].Name == "" {
			cfg.Checks[i].Name = fmt.Sprintf("check-%d", i+1)
		}
	}
	return cfg.Checks, nil
}
func finalize(rep model.RecoveryReport, srs map[string]*model.ServiceRecovery, evidenceDir string) (model.RecoveryReport, error) {
	names := make([]string, 0, len(srs))
	for n := range srs {
		names = append(names, n)
	}
	sortStrings(names)
	status := "PASS"
	for _, n := range names {
		sr := *srs[n]
		rep.Services = append(rep.Services, sr)
		for _, v := range []string{sr.Configuration, sr.DataRestore, sr.Startup, sr.Health} {
			if v == "FAIL" {
				status = "FAIL"
			} else if status == "PASS" && (strings.Contains(v, "MANUAL") || strings.Contains(v, "UNKNOWN") || v == "NOT RUN" || v == "DRY RUN") {
				status = "MANUAL CHECK REQUIRED"
			}
		}
	}
	for _, finding := range rep.Findings {
		switch strings.ToUpper(finding.Severity) {
		case "HIGH", "CRITICAL":
			status = "FAIL"
		}
	}
	if strings.HasPrefix(rep.CleanupStatus, "FAIL") {
		status = "FAIL"
	}
	rep.Status = status
	rep.FinishedAt = time.Now().UTC()
	path := filepath.Join(evidenceDir, rep.RunID+".json")
	rep.EvidencePath = path
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return rep, fmt.Errorf("marshal recovery evidence: %w", err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return rep, fmt.Errorf("write recovery evidence %s: %w", path, err)
	}
	return rep, nil
}
func appendReason(old, n string) string {
	if n == "" {
		return old
	}
	if old == "" {
		return redact.Line(n)
	}
	return old + "; " + redact.Line(n)
}
