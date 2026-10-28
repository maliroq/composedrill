package compose

import (
	"strings"
	"testing"
)

func TestParseYAMLSubset(t *testing.T) {
	in := []byte(`services:
  web:
    image: nginx:1.27
    ports:
      - "8080:80"
    volumes:
      - ./data:/data:ro
    environment:
      TOKEN: ${TOKEN}
  db:
    image: postgres:17
volumes:
  data:
`)
	m, _, err := ParseYAMLSubset(in)
	if err != nil {
		t.Fatal(err)
	}
	if asMap(m["services"])["web"] == nil {
		t.Fatal("web missing")
	}
}
func TestPinned(t *testing.T) {
	cases := map[string]bool{"nginx": false, "nginx:latest": false, "nginx:1.27": false, "registry:5000/x:2": false, "x@sha256:abc": true}
	for s, w := range cases {
		if got := IsPinnedImage(s); got != w {
			t.Fatalf("%s got %v", s, got)
		}
	}
}
func TestMalformedYAML(t *testing.T) {
	_, _, err := ParseYAMLSubset([]byte("services:\n  web\n    image: nginx\n"))
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestEnvironmentRefsDoNotExposeValues(t *testing.T) {
	got := extractEnvRefs(map[string]any{"PASSWORD": "super-secret", "API_URL": "${API_URL}"})
	joined := strings.Join(got, ",")
	if !strings.Contains(joined, "PASSWORD") || !strings.Contains(joined, "API_URL") {
		t.Fatalf("missing environment names: %v", got)
	}
	if strings.Contains(joined, "super-secret") {
		t.Fatalf("environment value leaked: %v", got)
	}
}

func TestStaticHealthcheckDisabledIsNotActive(t *testing.T) {
	for name, hc := range map[string]any{
		"disable": map[string]any{"disable": true},
		"none":    map[string]any{"test": []any{"NONE"}},
	} {
		if hasActiveHealthcheck(hc) {
			t.Fatalf("%s healthcheck should be inactive", name)
		}
	}
	if !hasActiveHealthcheck(map[string]any{"test": []any{"CMD", "true"}}) {
		t.Fatal("active healthcheck was not recognized")
	}
}

func TestRuntimeRiskFlagsMatchAutomaticRehearsalRefusals(t *testing.T) {
	flags := runtimeRiskFlags(map[string]any{
		"privileged":   true,
		"pid":          "host",
		"cap_add":      []any{"SYS_ADMIN"},
		"extra_hosts":  []any{"host.docker.internal:host-gateway"},
		"security_opt": []any{"seccomp=unconfined"},
	})
	joined := strings.Join(flags, ",")
	for _, want := range []string{"privileged", "pid", "cap_add", "extra_hosts", "security_opt"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing risk flag %q in %v", want, flags)
		}
	}
}
