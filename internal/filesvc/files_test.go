package filesvc

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafePath(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := SafePath(root, "docs")
	if err != nil || got != filepath.Join(root, "docs") {
		t.Fatalf("got %s err %v", got, err)
	}
	if _, err := SafePath(root, "../outside"); err == nil {
		t.Fatal("escape was allowed")
	}
	if _, err := SafePath(root, `docs\..\..\Windows`); err == nil {
		t.Fatal("windows escape was allowed")
	}
	home, err := SafePath(root, "")
	if err != nil || home != root {
		t.Fatalf("root got %s %v", home, err)
	}
}
