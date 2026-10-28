package rehearse

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/maliroq/composedrill/internal/model"
)

type transformResult struct {
	JSON            []byte
	VolumeNames     map[string]string
	BindTargets     map[string]string
	Services        []string
	Health          map[string]bool
	Persistent      map[string][]persistentMount
	Profiled        map[string]bool
	ReferencedFiles []string
}
type persistentMount struct{ Type, Source, Target string }

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

func transformCompose(raw []byte, runID, project, workspace, bundleRoot string, checks []model.Check) (transformResult, error) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return transformResult{}, err
	}
	res := transformResult{VolumeNames: map[string]string{}, BindTargets: map[string]string{}, Health: map[string]bool{}, Persistent: map[string][]persistentMount{}, Profiled: map[string]bool{}}
	services, ok := doc["services"].(map[string]any)
	if !ok || len(services) == 0 {
		return res, fmt.Errorf("resolved compose has no services")
	}
	vols := mapStringAny(doc["volumes"])
	for name, v := range vols {
		vm := mapStringAny(v)
		driver := strings.TrimSpace(fmt.Sprint(vm["driver"]))
		if driver == "<nil>" {
			driver = ""
		}
		if driver != "" && driver != "local" {
			return res, fmt.Errorf("volume %s uses driver %q; automatic rehearsal refused", name, driver)
		}
		if opts := mapStringAny(vm["driver_opts"]); len(opts) > 0 {
			return res, fmt.Errorf("volume %s uses driver_opts; automatic rehearsal refused", name)
		}
	}
	if nets := mapStringAny(doc["networks"]); len(nets) > 0 {
		for name, v := range nets {
			nm := mapStringAny(v)
			driver := strings.TrimSpace(fmt.Sprint(nm["driver"]))
			if driver == "<nil>" {
				driver = ""
			}
			if driver != "" && driver != "bridge" {
				return res, fmt.Errorf("network %s uses driver %q; automatic rehearsal refused", name, driver)
			}
			if opts := mapStringAny(nm["driver_opts"]); len(opts) > 0 {
				return res, fmt.Errorf("network %s uses driver_opts; automatic rehearsal refused", name)
			}
		}
	}
	for name, sv := range services {
		if !safeComposeIdentifier(name) {
			return res, fmt.Errorf("service name %q is not safe for automatic Docker CLI use", name)
		}
		sm, ok := sv.(map[string]any)
		if !ok {
			continue
		}
		res.Services = append(res.Services, name)
		if unsafeRuntime(sm) {
			return res, fmt.Errorf("service %s uses host/privileged runtime features; automatic rehearsal refused", name)
		}
		delete(sm, "container_name")
		// Docker logging drivers run daemon-side and can bypass the container's
		// internal network. Disable service logging in the recovery project rather
		// than allowing syslog/fluentd/etc. to contact production infrastructure.
		sm["logging"] = map[string]any{"driver": "none"}
		labels := mapStringAny(sm["labels"])
		labels["io.composedrill.run"] = runID
		labels["io.composedrill.managed"] = "true"
		sm["labels"] = labels
		res.Health[name] = hasActiveHealthcheck(sm["healthcheck"])
		if profiles, ok := sm["profiles"].([]any); ok && len(profiles) > 0 {
			res.Profiled[name] = true
		}
		refs, err := validateServiceFilesUnderBundle(sm, name, bundleRoot)
		if err != nil {
			return res, err
		}
		res.ReferencedFiles = append(res.ReferencedFiles, refs...)
		// Recovery services never publish host ports. Docker Engine 29.x can
		// silently drop publications for containers attached only to internal
		// networks, and making those networks non-internal would weaken the
		// recovery isolation boundary. HTTP/TCP checks therefore run from a
		// short-lived tool-owned probe container on the same internal network.
		delete(sm, "ports")
		if mounts, ok := sm["volumes"].([]any); ok {
			for _, mv := range mounts {
				mm, ok := mv.(map[string]any)
				if !ok {
					continue
				}
				typ := fmt.Sprint(mm["type"])
				target := fmt.Sprint(mm["target"])
				src := fmt.Sprint(mm["source"])
				if typ == "bind" {
					newp := filepath.Join(workspace, "data", "binds", safe(name), safeTarget(target))
					if err := os.MkdirAll(newp, 0o700); err != nil {
						return res, err
					}
					mm["source"] = newp
					res.BindTargets[name+"|"+target] = newp
					res.Persistent[name] = append(res.Persistent[name], persistentMount{Type: "bind", Source: src, Target: target})
				} else if typ == "volume" {
					if src == "" || src == "<nil>" {
						src = "anon_" + safe(name) + "_" + safeTarget(target)
						mm["source"] = src
						if _, exists := vols[src]; !exists {
							vols[src] = map[string]any{}
						}
					}
					res.Persistent[name] = append(res.Persistent[name], persistentMount{Type: "volume", Source: src, Target: target})
				} else if typ != "tmpfs" {
					return res, fmt.Errorf("service %s uses unsupported mount type %q; automatic rehearsal refused", name, typ)
				}
			}
		}
	}
	for k, v := range vols {
		vm, ok := v.(map[string]any)
		if !ok {
			vm = map[string]any{}
			vols[k] = vm
		}
		n := project + "_" + safe(k)
		vm["name"] = n
		vm["external"] = false
		vl := mapStringAny(vm["labels"])
		vl["io.composedrill.run"] = runID
		vl["io.composedrill.managed"] = "true"
		vm["labels"] = vl
		res.VolumeNames[k] = n
	}
	doc["volumes"] = vols
	nets := mapStringAny(doc["networks"])
	if len(nets) == 0 {
		nets["default"] = map[string]any{}
	}
	for k, v := range nets {
		nm, ok := v.(map[string]any)
		if !ok {
			nm = map[string]any{}
			nets[k] = nm
		}
		nm["name"] = project + "_" + safe(k)
		nm["external"] = false
		nm["internal"] = true
		nl := mapStringAny(nm["labels"])
		nl["io.composedrill.run"] = runID
		nl["io.composedrill.managed"] = "true"
		nm["labels"] = nl
	}
	doc["networks"] = nets
	refs, err := validateFilesUnderBundle(doc, "secrets", bundleRoot)
	if err != nil {
		return res, err
	}
	res.ReferencedFiles = append(res.ReferencedFiles, refs...)
	refs, err = validateFilesUnderBundle(doc, "configs", bundleRoot)
	if err != nil {
		return res, err
	}
	res.ReferencedFiles = append(res.ReferencedFiles, refs...)
	doc["name"] = project
	sortStrings(res.Services)
	out, err := json.MarshalIndent(doc, "", "  ")
	res.JSON = out
	return res, err
}
func mapStringAny(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}
func safeComposeIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		if i > 0 && (r == '_' || r == '.' || r == '-') {
			continue
		}
		return false
	}
	return true
}

func safe(s string) string {
	s = unsafeName.ReplaceAllString(s, "_")
	if s == "" {
		return "x"
	}
	return s
}
func safeTarget(s string) string {
	s = strings.Trim(s, "/")
	return safe(strings.ReplaceAll(s, "/", "_"))
}
func unsafeRuntime(sm map[string]any) bool {
	if b, ok := sm["privileged"].(bool); ok && b {
		return true
	}
	// Namespace-sharing and host-routing features can bypass the disposable
	// recovery network or couple the rehearsal to another container/host.
	for _, k := range []string{"pid", "ipc", "network_mode", "uts", "userns_mode", "cgroup", "cgroup_parent", "runtime", "isolation", "credential_spec"} {
		if v, ok := sm[k]; ok && strings.TrimSpace(fmt.Sprint(v)) != "" && fmt.Sprint(v) != "<nil>" {
			return true
		}
	}
	for _, k := range []string{"devices", "device_cgroup_rules", "cap_add", "volumes_from", "external_links", "extra_hosts", "gpus"} {
		if len(asAnyList(sm[k])) > 0 {
			return true
		}
	}
	if len(asAnyList(sm["security_opt"])) > 0 {
		return true
	}
	return false
}

func asAnyList(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case nil:
		return nil
	default:
		return []any{x}
	}
}

func hasActiveHealthcheck(v any) bool {
	m, ok := v.(map[string]any)
	if !ok || len(m) == 0 {
		return false
	}
	if disabled, _ := m["disable"].(bool); disabled {
		return false
	}
	test := asAnyList(m["test"])
	if len(test) > 0 && strings.EqualFold(strings.TrimSpace(fmt.Sprint(test[0])), "NONE") {
		return false
	}
	return len(test) > 0
}
func validateServiceFilesUnderBundle(sm map[string]any, service, bundle string) ([]string, error) {
	refs := []string{}
	check := func(path, kind string) error {
		if path == "" {
			return nil
		}
		if err := validatePathWithinBundle(bundle, path); err != nil {
			return fmt.Errorf("service %s %s: %w", service, kind, err)
		}
		refs = append(refs, path)
		return nil
	}
	switch v := sm["env_file"].(type) {
	case string:
		if err := check(v, "env_file"); err != nil {
			return nil, err
		}
	case []any:
		for _, item := range v {
			switch x := item.(type) {
			case string:
				if err := check(x, "env_file"); err != nil {
					return nil, err
				}
			case map[string]any:
				if p, ok := x["path"].(string); ok {
					if err := check(p, "env_file"); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	return refs, nil
}

func validateFilesUnderBundle(doc map[string]any, key, bundle string) ([]string, error) {
	refs := []string{}
	items := mapStringAny(doc[key])
	for name, v := range items {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if f, ok := m["file"].(string); ok && f != "" {
			if err := validatePathWithinBundle(bundle, f); err != nil {
				return nil, fmt.Errorf("%s %s: %w", key, name, err)
			}
			refs = append(refs, f)
		}
		if ex, ok := m["external"].(bool); ok && ex {
			return nil, fmt.Errorf("external %s %s cannot be proven from the recovery bundle", key, name)
		}
	}
	return refs, nil
}

func validatePathWithinBundle(bundle, path string) error {
	base, err := filepath.EvalSymlinks(bundle)
	if err != nil {
		return fmt.Errorf("cannot resolve recovery bundle: %w", err)
	}
	base, err = filepath.Abs(base)
	if err != nil {
		return err
	}
	p := path
	if !filepath.IsAbs(p) {
		p = filepath.Join(bundle, p)
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return fmt.Errorf("referenced file is missing or cannot be resolved")
	}
	real, err = filepath.Abs(real)
	if err != nil {
		return err
	}
	if real != base && !strings.HasPrefix(real, base+string(os.PathSeparator)) {
		return fmt.Errorf("referenced file resolves outside recovery bundle; automatic rehearsal refused")
	}
	st, err := os.Stat(real)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("referenced path is not a regular file")
	}
	return nil
}
