package safefs

import (
	"archive/tar"
	"bytes"
	"testing"
)

func TestExtractRejectsTraversal(t *testing.T) {
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	_ = tw.WriteHeader(&tar.Header{Name: "../escape", Mode: 0600, Size: 1})
	_, _ = tw.Write([]byte("x"))
	_ = tw.Close()
	if err := ExtractTar(bytes.NewReader(b.Bytes()), t.TempDir(), false); err == nil {
		t.Fatal("expected traversal rejection")
	}
}

func TestValidateTarRejectsLinksAndSpecialFiles(t *testing.T) {
	cases := []*tar.Header{
		{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"},
		{Name: "hard", Typeflag: tar.TypeLink, Linkname: "target"},
		{Name: "fifo", Typeflag: tar.TypeFifo},
	}
	for _, h := range cases {
		var b bytes.Buffer
		tw := tar.NewWriter(&b)
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := ValidateTar(bytes.NewReader(b.Bytes()), false); err == nil {
			t.Fatalf("expected validation refusal for tar type %d", h.Typeflag)
		}
	}
}
