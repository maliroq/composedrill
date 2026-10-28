package rehearse

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyTreeAppliesSourceRootPermissions(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "index.html"), []byte("recovered\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "dest")
	if err := os.Mkdir(dest, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := copyTree(src, dest); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}

	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("destination mode = %o, want 755", got)
	}
}
