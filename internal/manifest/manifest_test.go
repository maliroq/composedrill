package manifest

import (
	"path/filepath"
	"testing"

	"github.com/maliroq/composedrill/internal/model"
)

func TestManifestRelativizesPathsInsideRoot(t *testing.T) {
	root := t.TempDir()
	p := model.Project{
		Name:        "app",
		Dir:         filepath.Join(root, "app"),
		ComposeFile: filepath.Join(root, "app", "compose.yaml"),
		BindMounts:  []model.Mount{{Type: "bind", Source: filepath.Join(root, "app", "data"), Target: "/data"}},
		Services:    []model.Service{{Name: "web", Healthcheck: true}},
	}
	got := Build(model.ScanResult{Root: root, Projects: []model.Project{p}}, "test")
	if got.Projects[0].ComposeFile != "app/compose.yaml" {
		t.Fatalf("compose path not portable: %q", got.Projects[0].ComposeFile)
	}
	if got.Projects[0].BindMounts[0].Source != "app/data" {
		t.Fatalf("bind path not portable: %q", got.Projects[0].BindMounts[0].Source)
	}
}
