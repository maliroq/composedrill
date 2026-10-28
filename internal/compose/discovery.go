package compose

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/maliroq/composedrill/internal/model"
)

var composeNames = []string{"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml"}
var envRefRE = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)`)

func Discover(root string) ([]model.Project, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	byDir := map[string]string{}
	err = filepath.WalkDir(abs, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			base := filepath.Base(path)
			if base == ".git" || base == "node_modules" || base == ".composedrill" {
				return filepath.SkipDir
			}
			if path != abs {
				if _, err := os.Stat(filepath.Join(path, "composedrill-backup.json")); err == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		name := filepath.Base(path)
		pri := -1
		for i, n := range composeNames {
			if name == n {
				pri = i
				break
			}
		}
		if pri < 0 {
			return nil
		}
		dir := filepath.Dir(path)
		if old, ok := byDir[dir]; ok {
			op := 99
			for i, n := range composeNames {
				if filepath.Base(old) == n {
					op = i
				}
			}
			if op <= pri {
				return nil
			}
		}
		byDir[dir] = path
		return nil
	})
	if err != nil {
		return nil, err
	}
	dirs := make([]string, 0, len(byDir))
	for d := range byDir {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	projects := []model.Project{}
	for _, dir := range dirs {
		p, err := parseProject(byDir[dir])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", byDir[dir], err)
		}
		projects = append(projects, p)
	}
	return projects, nil
}

func parseProject(file string) (model.Project, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return model.Project{}, err
	}
	root, info, err := ParseYAMLSubset(data)
	if err != nil {
		return model.Project{}, err
	}
	h := sha256.Sum256(data)
	dir := filepath.Dir(file)
	name := filepath.Base(dir)
	if n := asString(root["name"]); n != "" {
		name = n
	}
	p := model.Project{Name: name, Dir: dir, ComposeFile: file, ComposeSHA256: hex.EncodeToString(h[:]), GitRevision: gitRevision(dir), UnsupportedYAML: info.Unsupported, TopLevelVolumeRisk: map[string]string{}}
	if vols := asMap(root["volumes"]); vols != nil {
		for k, v := range vols {
			p.NamedVolumes = append(p.NamedVolumes, k)
			vm := asMap(v)
			if vm != nil {
				if b, ok := vm["external"].(bool); ok && b {
					p.TopLevelVolumeRisk[k] = "external"
				}
				if n := asString(vm["name"]); n != "" {
					p.TopLevelVolumeRisk[k] = "explicit-name:" + n
				}
				if d := strings.TrimSpace(asString(vm["driver"])); d != "" && d != "local" {
					p.TopLevelVolumeRisk[k] = "driver:" + d
				}
				if opts := asMap(vm["driver_opts"]); len(opts) > 0 {
					p.TopLevelVolumeRisk[k] = "driver-opts"
				}
			}
		}
		sort.Strings(p.NamedVolumes)
	}
	if secs := asMap(root["secrets"]); secs != nil {
		for k := range secs {
			p.Secrets = append(p.Secrets, k)
		}
		sort.Strings(p.Secrets)
	}
	services := asMap(root["services"])
	if services == nil {
		return p, fmt.Errorf("no services mapping found")
	}
	names := make([]string, 0, len(services))
	for n := range services {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, sn := range names {
		sm := asMap(services[sn])
		if sm == nil {
			p.Warnings = append(p.Warnings, "service "+sn+" is not a mapping")
			continue
		}
		s := model.Service{Name: sn, Image: asString(sm["image"]), Build: sm["build"] != nil}
		s.Healthcheck = hasActiveHealthcheck(sm["healthcheck"])
		s.Profiles = stringList(sm["profiles"])
		s.EnvFiles = stringList(sm["env_file"])
		for i, e := range s.EnvFiles {
			if !filepath.IsAbs(e) {
				s.EnvFiles[i] = filepath.Join(dir, e)
			}
		}
		s.Secrets = extractSecretNames(sm["secrets"])
		s.DependsOn = extractDepends(sm["depends_on"])
		s.Ports = extractPorts(sm["ports"])
		s.EnvironmentRefs = extractEnvRefs(sm["environment"])
		s.Mounts = extractMounts(sm["volumes"], dir)
		for _, m := range s.Mounts {
			if m.Type == "bind" {
				p.BindMounts = append(p.BindMounts, m)
			}
		}
		s.Database, s.DatabaseStrategy = detectDatabase(s, dir)
		s.RiskFlags = runtimeRiskFlags(sm)
		p.Services = append(p.Services, s)
	}
	return p, nil
}

func hasActiveHealthcheck(v any) bool {
	m := asMap(v)
	if m == nil {
		return false
	}
	if disabled, ok := m["disable"].(bool); ok && disabled {
		return false
	}
	test := asList(m["test"])
	if len(test) > 0 && strings.EqualFold(asString(test[0]), "NONE") {
		return false
	}
	return len(test) > 0 || m["disable"] != nil || m["interval"] != nil || m["timeout"] != nil || m["retries"] != nil || m["start_period"] != nil
}

func runtimeRiskFlags(sm map[string]any) []string {
	flags := []string{}
	if b, ok := sm["privileged"].(bool); ok && b {
		flags = append(flags, "privileged")
	}
	for _, key := range []string{"pid", "ipc", "network_mode", "uts", "userns_mode", "cgroup", "cgroup_parent", "runtime", "isolation", "credential_spec"} {
		if strings.TrimSpace(asString(sm[key])) != "" {
			flags = append(flags, key)
		}
	}
	for _, key := range []string{"devices", "device_cgroup_rules", "cap_add", "volumes_from", "external_links", "extra_hosts", "gpus"} {
		if v, ok := sm[key]; ok && v != nil {
			if len(asList(v)) > 0 || strings.TrimSpace(asString(v)) != "" || asMap(v) != nil {
				flags = append(flags, key)
			}
		}
	}
	if len(stringList(sm["security_opt"])) > 0 {
		flags = append(flags, "security_opt")
	}
	sort.Strings(flags)
	return flags
}

func extractMounts(v any, dir string) []model.Mount {
	out := []model.Mount{}
	for _, x := range asList(v) {
		switch t := x.(type) {
		case string:
			parts := strings.Split(t, ":")
			if len(parts) == 1 {
				out = append(out, model.Mount{Type: "volume", Target: parts[0]})
				continue
			}
			src, target := parts[0], parts[1]
			typ := "volume"
			if strings.HasPrefix(src, ".") || strings.HasPrefix(src, "/") || strings.HasPrefix(src, "~") {
				typ = "bind"
				if !filepath.IsAbs(src) {
					src = filepath.Clean(filepath.Join(dir, src))
				}
			}
			ro := len(parts) > 2 && strings.Contains(parts[2], "ro")
			out = append(out, model.Mount{Type: typ, Source: src, Target: target, ReadOnly: ro})
		case map[string]any:
			typ := asString(t["type"])
			if typ == "" {
				typ = "volume"
			}
			src := asString(t["source"])
			if typ == "bind" && !filepath.IsAbs(src) {
				src = filepath.Clean(filepath.Join(dir, src))
			}
			ro, _ := t["read_only"].(bool)
			out = append(out, model.Mount{Type: typ, Source: src, Target: asString(t["target"]), ReadOnly: ro})
		}
	}
	return out
}
func extractPorts(v any) []string {
	out := []string{}
	for _, x := range asList(v) {
		switch t := x.(type) {
		case string:
			out = append(out, t)
		case int64:
			out = append(out, fmt.Sprint(t))
		case map[string]any:
			target := asString(t["target"])
			pub := asString(t["published"])
			if pub != "" {
				out = append(out, pub+":"+target)
			} else {
				out = append(out, target)
			}
		}
	}
	return out
}
func extractDepends(v any) []string {
	if m := asMap(v); m != nil {
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	return stringList(v)
}
func extractSecretNames(v any) []string {
	out := []string{}
	for _, x := range asList(v) {
		switch t := x.(type) {
		case string:
			out = append(out, t)
		case map[string]any:
			if s := asString(t["source"]); s != "" {
				out = append(out, s)
			}
		}
	}
	sort.Strings(out)
	return out
}
func extractEnvRefs(v any) []string {
	set := map[string]bool{}
	addPlaceholders := func(s string) {
		for _, m := range envRefRE.FindAllStringSubmatch(s, -1) {
			set[m[1]] = true
		}
	}
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			set[k] = true
			if s, ok := val.(string); ok {
				addPlaceholders(s)
			}
		}
	case []any:
		for _, item := range t {
			if s, ok := item.(string); ok {
				name := s
				if i := strings.Index(name, "="); i >= 0 {
					addPlaceholders(name[i+1:])
					name = name[:i]
				} else {
					addPlaceholders(name)
				}
				name = strings.TrimSpace(name)
				if name != "" && !strings.Contains(name, "${") {
					set[name] = true
				}
			}
		}
	case string:
		addPlaceholders(t)
	}
	out := []string{}
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
func detectDatabase(s model.Service, dir string) (string, string) {
	i := strings.ToLower(s.Image)
	switch {
	case strings.Contains(i, "postgres"):
		return "postgresql", "pg_dump/pg_restore or pg_dumpall logical dump"
	case strings.Contains(i, "mariadb"):
		return "mariadb", "mariadb-dump logical dump"
	case strings.Contains(i, "mysql"):
		return "mysql", "mysqldump logical dump"
	}
	for _, m := range s.Mounts {
		if m.Type == "bind" {
			if hasSQLiteFile(m.Source) {
				return "sqlite", "SQLite online backup API / sqlite3 .backup"
			}
		}
	}
	return "", ""
}
func hasSQLiteFile(path string) bool {
	st, err := os.Stat(path)
	if err != nil {
		return false
	}
	if !st.IsDir() {
		l := strings.ToLower(path)
		return strings.HasSuffix(l, ".db") || strings.HasSuffix(l, ".sqlite") || strings.HasSuffix(l, ".sqlite3")
	}
	count := 0
	found := false
	_ = filepath.WalkDir(path, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return filepath.SkipDir
		}
		count++
		if count > 500 {
			return filepath.SkipAll
		}
		if !d.IsDir() {
			l := strings.ToLower(d.Name())
			if strings.HasSuffix(l, ".db") || strings.HasSuffix(l, ".sqlite") || strings.HasSuffix(l, ".sqlite3") {
				found = true
				return filepath.SkipAll
			}
		}
		return nil
	})
	return found
}
func gitRevision(dir string) string {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "HEAD")
	b, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
func IsPinnedImage(image string) bool {
	// Tags, including version-looking tags, are mutable registry references.
	// Only a digest identifies immutable image content for recovery proof.
	return strings.Contains(image, "@sha256:")
}
