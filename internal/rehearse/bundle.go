package rehearse

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/maliroq/composedrill/internal/model"
	"github.com/maliroq/composedrill/internal/safefs"
)

type stagedBundle struct {
	Root        string
	Manifest    model.BackupManifest
	ManifestSHA string
}

func stageBundle(path, workspace string) (stagedBundle, error) {
	st := stagedBundle{}
	info, err := os.Stat(path)
	if err != nil {
		return st, err
	}
	root := path
	if !info.IsDir() {
		root = filepath.Join(workspace, "bundle")
		f, err := os.Open(path)
		if err != nil {
			return st, err
		}
		defer f.Close()
		gz := strings.HasSuffix(strings.ToLower(path), ".gz") || strings.HasSuffix(strings.ToLower(path), ".tgz")
		if err := safefs.ExtractTar(f, root, gz); err != nil {
			return st, fmt.Errorf("extract backup bundle: %w", err)
		}
	}
	mf := filepath.Join(root, "composedrill-backup.json")
	mfi, err := os.Lstat(mf)
	if err != nil {
		return st, fmt.Errorf("backup must contain composedrill-backup.json: %w", err)
	}
	if mfi.Mode()&os.ModeSymlink != 0 || !mfi.Mode().IsRegular() {
		return st, fmt.Errorf("composedrill-backup.json must be a regular non-symlink file")
	}
	b, err := os.ReadFile(mf)
	if err != nil {
		return st, fmt.Errorf("backup must contain composedrill-backup.json: %w", err)
	}
	sum := sha256.Sum256(b)
	if err := json.Unmarshal(b, &st.Manifest); err != nil {
		return st, fmt.Errorf("parse backup manifest: %w", err)
	}
	if st.Manifest.Format != "composedrill-recovery-bundle" || st.Manifest.Version != 1 {
		return st, fmt.Errorf("unsupported backup bundle format/version")
	}
	if err := validateBackupManifest(st.Manifest); err != nil {
		return st, err
	}
	for _, e := range st.Manifest.Entries {
		p, err := safeJoin(root, e.Artifact)
		if err != nil {
			return st, err
		}
		got, err := hashPath(p)
		if err != nil {
			return st, fmt.Errorf("artifact %s: %w", e.Artifact, err)
		}
		if !strings.EqualFold(got, e.SHA256) {
			return st, fmt.Errorf("checksum mismatch for %s: expected %s got %s", e.Artifact, e.SHA256, got)
		}
	}
	st.Root = root
	st.ManifestSHA = hex.EncodeToString(sum[:])
	return st, nil
}
func validateBackupManifest(m model.BackupManifest) error {
	known := map[string]bool{"compose": true, "bind": true, "volume": true, "database": true, "env": true, "secret": true, "config": true, "checks": true}
	composeCount := 0
	mappingKeys := map[string]bool{}
	for i, e := range m.Entries {
		if !known[e.Kind] {
			return fmt.Errorf("backup manifest entry %d has unsupported kind %q", i, e.Kind)
		}
		if strings.TrimSpace(e.Artifact) == "" {
			return fmt.Errorf("backup manifest entry %d has no artifact", i)
		}
		if len(e.SHA256) != 64 {
			return fmt.Errorf("backup manifest entry %s has invalid SHA-256", e.Artifact)
		}
		if _, err := hex.DecodeString(e.SHA256); err != nil {
			return fmt.Errorf("backup manifest entry %s has invalid SHA-256", e.Artifact)
		}
		var key string
		switch e.Kind {
		case "compose":
			composeCount++
			key = "compose"
		case "bind":
			if e.Service == "" || e.Target == "" {
				return fmt.Errorf("bind entry %s requires service and target", e.Artifact)
			}
			key = "bind\x00" + e.Service + "\x00" + e.Target
		case "volume":
			if e.Service != "" && e.Target != "" {
				key = "volume-target\x00" + e.Service + "\x00" + e.Target
			} else if e.Volume != "" {
				key = "volume-name\x00" + e.Volume
			} else {
				return fmt.Errorf("volume entry %s requires service+target or volume", e.Artifact)
			}
		case "database":
			if e.Service == "" {
				return fmt.Errorf("database entry %s requires service", e.Artifact)
			}
			key = "database\x00" + e.Service
		case "checks":
			key = "checks\x00" + filepath.ToSlash(filepath.Clean(e.Artifact))
		}
		if key != "" {
			if mappingKeys[key] {
				return fmt.Errorf("backup manifest has duplicate recovery mapping for %s", e.Kind)
			}
			mappingKeys[key] = true
		}
	}
	if composeCount != 1 {
		return fmt.Errorf("backup manifest must contain exactly one compose entry, got %d", composeCount)
	}
	return nil
}

func safeJoin(root, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("artifact path must be relative: %s", rel)
	}
	base, _ := filepath.Abs(root)
	p, _ := filepath.Abs(filepath.Join(base, filepath.Clean(rel)))
	if p != base && !strings.HasPrefix(p, base+string(os.PathSeparator)) {
		return "", fmt.Errorf("artifact escapes bundle root: %s", rel)
	}
	return p, nil
}
func hashPath(path string) (string, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("symlink artifacts are refused: %s", filepath.Base(path))
	}
	if !st.IsDir() && !st.Mode().IsRegular() {
		return "", fmt.Errorf("special-file artifacts are refused: %s", filepath.Base(path))
	}
	h := sha256.New()
	if !st.IsDir() {
		f, err := os.Open(path)
		if err != nil {
			return "", err
		}
		defer f.Close()
		if _, err := io.Copy(h, f); err != nil {
			return "", err
		}
		return hex.EncodeToString(h.Sum(nil)), nil
	}
	entries := []string{}
	err = filepath.WalkDir(path, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if p == path {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink refused in directory artifact: %s", p)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !d.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("special file refused in directory artifact: %s", p)
		}
		r, _ := filepath.Rel(path, p)
		entries = append(entries, filepath.ToSlash(r))
		return nil
	})
	if err != nil {
		return "", err
	}
	sortStrings(entries)
	// Frame type/path/mode/length so directory restore metadata is covered and
	// path/content boundaries cannot collide. The format is versioned because
	// recovery manifests persist these hashes.
	if _, err := io.WriteString(h, "composedrill-directory-hash-v1\x00"); err != nil {
		return "", err
	}
	for _, r := range entries {
		p := filepath.Join(path, filepath.FromSlash(r))
		info, err := os.Lstat(p)
		if err != nil {
			return "", err
		}
		kind := byte('f')
		if info.IsDir() {
			kind = byte('d')
		}
		if _, err := h.Write([]byte{kind}); err != nil {
			return "", err
		}
		if err := binary.Write(h, binary.BigEndian, uint64(len(r))); err != nil {
			return "", err
		}
		if _, err := io.WriteString(h, r); err != nil {
			return "", err
		}
		if err := binary.Write(h, binary.BigEndian, uint32(info.Mode().Perm())); err != nil {
			return "", err
		}
		if info.IsDir() {
			continue
		}
		if err := binary.Write(h, binary.BigEndian, uint64(info.Size())); err != nil {
			return "", err
		}
		f, err := os.Open(p)
		if err != nil {
			return "", err
		}
		_, err = io.Copy(h, f)
		closeErr := f.Close()
		if err != nil {
			return "", err
		}
		if closeErr != nil {
			return "", closeErr
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
func findEntry(m model.BackupManifest, kind, service, target, volume string) *model.BackupEntry {
	for i := range m.Entries {
		e := &m.Entries[i]
		if e.Kind != kind {
			continue
		}
		if service != "" && e.Service != service {
			continue
		}
		if target != "" && e.Target != target {
			continue
		}
		if volume != "" && e.Volume != volume {
			continue
		}
		return e
	}
	return nil
}
func composeEntry(m model.BackupManifest) *model.BackupEntry {
	for i := range m.Entries {
		if m.Entries[i].Kind == "compose" {
			return &m.Entries[i]
		}
	}
	return nil
}
