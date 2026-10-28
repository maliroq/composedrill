package rehearse

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/maliroq/composedrill/internal/model"
	"github.com/maliroq/composedrill/internal/safefs"
)

func restoreBind(bundleRoot string, e *model.BackupEntry, dest string) error {
	src, err := safeJoin(bundleRoot, e.Artifact)
	if err != nil {
		return err
	}
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	if st.IsDir() {
		return copyTree(src, dest)
	}
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	low := strings.ToLower(src)
	if strings.HasSuffix(low, ".tar.gz") || strings.HasSuffix(low, ".tgz") {
		return safefs.ExtractTar(f, dest, true)
	}
	if strings.HasSuffix(low, ".tar") {
		return safefs.ExtractTar(f, dest, false)
	}
	return fmt.Errorf("bind artifact must be a directory, .tar, or .tar.gz: %s", e.Artifact)
}
func copyTree(src, dest string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		out := filepath.Join(dest, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink refused in recovery artifact: %s", rel)
		}
		if d.IsDir() {
			return os.MkdirAll(out, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("non-regular file refused in recovery artifact: %s", rel)
		}
		if err := os.MkdirAll(filepath.Dir(out), 0700); err != nil {
			return err
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		f, err := os.OpenFile(out, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			in.Close()
			return err
		}
		_, cpErr := io.Copy(f, in)
		inCloseErr := in.Close()
		outCloseErr := f.Close()
		if cpErr != nil {
			return cpErr
		}
		if inCloseErr != nil {
			return inCloseErr
		}
		return outCloseErr
	})
}
func restoreVolume(ctx context.Context, runID, volume, bundleRoot string, e *model.BackupEntry) error {
	if !strings.HasPrefix(volume, "composedrill-") {
		return fmt.Errorf("refusing to restore into non-ComposeDrill volume %s", volume)
	}
	artifact, err := safeJoin(bundleRoot, e.Artifact)
	if err != nil {
		return err
	}
	st, err := os.Stat(artifact)
	if err != nil {
		return err
	}
	if st.IsDir() {
		return fmt.Errorf("named-volume artifacts must be tar archives in v0.1")
	}
	low := strings.ToLower(artifact)
	gz := strings.HasSuffix(low, ".tar.gz") || strings.HasSuffix(low, ".tgz")
	if !gz && !strings.HasSuffix(low, ".tar") {
		return fmt.Errorf("named-volume artifact must be .tar, .tar.gz, or .tgz in v0.1")
	}
	f, err := os.Open(artifact)
	if err != nil {
		return err
	}
	validateErr := safefs.ValidateTar(f, gz)
	closeErr := f.Close()
	if validateErr != nil {
		return fmt.Errorf("unsafe named-volume archive: %w", validateErr)
	}
	if closeErr != nil {
		return closeErr
	}

	tarArgs := "-xf"
	if gz {
		tarArgs = "-xzf"
	}
	project := ProjectNameForRun(runID)
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "--pull=never", "--network", "none", "--read-only", "--cap-drop=ALL",
		"--security-opt", "no-new-privileges=true",
		"--label", "io.composedrill.run="+runID,
		"--label", "io.composedrill.managed=true",
		"--label", "com.docker.compose.project="+project,
		"-v", volume+":/restore", "-v", artifact+":/backup/archive:ro", utilityImage,
		"sh", "-ceu", "cd /restore && tar "+tarArgs+" /backup/archive")
	cmd.Env = safeDockerEnv()
	b, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(b))
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("restore volume %s: %s", volume, detail)
	}
	return nil
}
func shellSafe(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

func databaseRestoreArgs(e *model.BackupEntry) ([]string, error) {
	strategy := e.Strategy
	if strategy == "" {
		switch strings.ToLower(e.Engine) {
		case "postgres", "postgresql":
			strategy = "postgres-sql"
		case "mysql", "mariadb":
			strategy = "mysql-sql"
		}
	}
	switch strategy {
	case "postgres-sql":
		return []string{"exec", "-T", e.Service, "sh", "-ceu", `exec psql -v ON_ERROR_STOP=1 -U "${POSTGRES_USER:-postgres}" -d "${POSTGRES_DB:-postgres}"`}, nil
	case "postgres-custom":
		return []string{"exec", "-T", e.Service, "sh", "-ceu", `exec pg_restore --exit-on-error --clean --if-exists -U "${POSTGRES_USER:-postgres}" -d "${POSTGRES_DB:-postgres}" -`}, nil
	case "mysql-sql":
		script := `pw="${MYSQL_ROOT_PASSWORD:-${MARIADB_ROOT_PASSWORD:-}}"; [ -n "$pw" ] || { echo 'root password unavailable' >&2; exit 2; }; if command -v mariadb >/dev/null 2>&1; then client=mariadb; elif command -v mysql >/dev/null 2>&1; then client=mysql; else echo 'mysql/mariadb client unavailable' >&2; exit 2; fi; db="${MYSQL_DATABASE:-${MARIADB_DATABASE:-}}"; if [ -n "$db" ]; then exec "$client" -uroot -p"$pw" "$db"; else exec "$client" -uroot -p"$pw"; fi`
		return []string{"exec", "-T", e.Service, "sh", "-ceu", script}, nil
	default:
		return nil, fmt.Errorf("unsupported database restore strategy %q", strategy)
	}
}

func restoreDatabase(ctx context.Context, d dockerCLI, bundleRoot string, e *model.BackupEntry) error {
	artifact, err := safeJoin(bundleRoot, e.Artifact)
	if err != nil {
		return err
	}
	f, err := os.Open(artifact)
	if err != nil {
		return err
	}
	defer f.Close()
	args, err := databaseRestoreArgs(e)
	if err != nil {
		return err
	}
	cmd := d.cmd(ctx, args...)
	cmd.Stdin = f
	var er strings.Builder
	cmd.Stderr = &er
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("database restore for %s failed: %s", e.Service, strings.TrimSpace(er.String()))
	}
	return nil
}
