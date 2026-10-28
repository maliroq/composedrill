package safefs

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func ValidateTar(r io.Reader, gz bool) error {
	tr, closeFn, err := tarReader(r, gz)
	if err != nil {
		return err
	}
	if closeFn != nil {
		defer closeFn()
	}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := safeArchiveName(h.Name); err != nil {
			return err
		}
		if err := supportedArchiveType(h); err != nil {
			return err
		}
	}
}

func tarReader(r io.Reader, gz bool) (*tar.Reader, func() error, error) {
	if !gz {
		return tar.NewReader(r), nil, nil
	}
	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, nil, err
	}
	return tar.NewReader(gr), gr.Close, nil
}

func safeArchiveName(name string) (string, error) {
	clean := filepath.Clean(name)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	return clean, nil
}

func supportedArchiveType(h *tar.Header) error {
	switch h.Typeflag {
	case tar.TypeDir, tar.TypeReg, tar.TypeRegA:
		return nil
	case tar.TypeSymlink, tar.TypeLink:
		return fmt.Errorf("links are not extracted in recovery bundles: %q", h.Name)
	default:
		return fmt.Errorf("unsupported archive entry type %d for %q", h.Typeflag, h.Name)
	}
}

func ExtractTar(r io.Reader, dest string, gz bool) error {
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return err
	}
	tr, closeFn, err := tarReader(r, gz)
	if err != nil {
		return err
	}
	if closeFn != nil {
		defer closeFn()
	}
	base, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name, err := safeArchiveName(h.Name)
		if err != nil {
			return err
		}
		if err := supportedArchiveType(h); err != nil {
			return err
		}
		out := filepath.Join(base, name)
		abs, err := filepath.Abs(out)
		if err != nil {
			return err
		}
		if abs != base && !strings.HasPrefix(abs, base+string(os.PathSeparator)) {
			return fmt.Errorf("archive escapes destination: %q", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(abs, os.FileMode(h.Mode)&0o777); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
				return err
			}
			f, err := os.OpenFile(abs, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(h.Mode)&0o777)
			if err != nil {
				return err
			}
			_, cpErr := io.Copy(f, tr)
			closeErr := f.Close()
			if cpErr != nil {
				return cpErr
			}
			if closeErr != nil {
				return closeErr
			}
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("links are not extracted in recovery bundles: %q", h.Name)
		default:
			return fmt.Errorf("unsupported archive entry type %d for %q", h.Typeflag, h.Name)
		}
	}
	return nil
}
