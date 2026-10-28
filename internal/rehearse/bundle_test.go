package rehearse

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maliroq/composedrill/internal/model"
)

func TestHashPathDirectoryFramingDistinguishesBoundaries(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()

	if err := os.WriteFile(filepath.Join(a, "ab"), []byte("c"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b, "a"), []byte("bc"), 0o600); err != nil {
		t.Fatal(err)
	}

	ha, err := hashPath(a)
	if err != nil {
		t.Fatal(err)
	}
	hb, err := hashPath(b)
	if err != nil {
		t.Fatal(err)
	}
	if ha == hb {
		t.Fatalf("framed directory hashes must distinguish different path/content boundaries: %s", ha)
	}
}

func TestHashPathDirectoryDeterministic(t *testing.T) {
	d := t.TempDir()
	if err := os.MkdirAll(filepath.Join(d, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "z.txt"), []byte("z"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "nested", "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}

	h1, err := hashPath(d)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := hashPath(d)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatalf("directory hash must be deterministic: %s != %s", h1, h2)
	}
}

func TestHashPathDirectoryIncludesModeAndEmptyDirectory(t *testing.T) {
	d := t.TempDir()
	file := filepath.Join(d, "script")
	if err := os.WriteFile(file, []byte("echo ok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h1, err := hashPath(d)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0o700); err != nil {
		t.Fatal(err)
	}
	h2, err := hashPath(d)
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h2 {
		t.Fatal("file mode change must alter directory artifact hash")
	}
	if err := os.Mkdir(filepath.Join(d, "empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	h3, err := hashPath(d)
	if err != nil {
		t.Fatal(err)
	}
	if h2 == h3 {
		t.Fatal("empty directory addition must alter directory artifact hash")
	}
}

func TestStageBundleRejectsSymlinkManifest(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real.json")
	if err := os.WriteFile(target, []byte(`{"format":"composedrill-recovery-bundle","version":1,"entries":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.json", filepath.Join(root, "composedrill-backup.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := stageBundle(root, t.TempDir()); err == nil {
		t.Fatal("expected symlink manifest refusal")
	}
}

func TestValidateBackupManifestRejectsAmbiguityAndUnknownKinds(t *testing.T) {
	validHash := strings.Repeat("a", 64)
	cases := []model.BackupManifest{
		{Entries: []model.BackupEntry{{Kind: "compose", Artifact: "a", SHA256: validHash}, {Kind: "compose", Artifact: "b", SHA256: validHash}}},
		{Entries: []model.BackupEntry{{Kind: "compose", Artifact: "a", SHA256: validHash}, {Kind: "mystery", Artifact: "b", SHA256: validHash}}},
		{Entries: []model.BackupEntry{{Kind: "compose", Artifact: "a", SHA256: validHash}, {Kind: "bind", Service: "app", Target: "/data", Artifact: "b", SHA256: validHash}, {Kind: "bind", Service: "app", Target: "/data", Artifact: "c", SHA256: validHash}}},
	}
	for i, m := range cases {
		if err := validateBackupManifest(m); err == nil {
			t.Fatalf("case %d: expected manifest validation failure", i)
		}
	}
}
