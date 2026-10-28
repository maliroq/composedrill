package scan

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRun(t *testing.T) {
	d := t.TempDir()
	data := `services:
  web:
    image: nginx:latest
    volumes:
      - ./data:/data
  db:
    image: postgres:17
    healthcheck:
      test: ["CMD", "pg_isready"]
volumes:
  dbdata:
`
	if err := os.WriteFile(filepath.Join(d, "compose.yaml"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(d, "data"), 0700); err != nil {
		t.Fatal(err)
	}
	r, err := Run(d)
	if err != nil {
		t.Fatal(err)
	}
	if r.ServiceCount != 2 || r.DatabaseCount != 1 || r.UnpinnedImages != 2 {
		t.Fatalf("bad counts: %+v", r)
	}
}
