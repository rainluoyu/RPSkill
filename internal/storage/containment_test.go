package storage

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestWithinDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	for _, tc := range []struct {
		target string
		want   bool
	}{
		{root, true}, {filepath.Join(root, "child"), true},
		{root + "-sibling", false}, {filepath.Dir(root), false},
	} {
		got, err := WithinDirectory(root, tc.target)
		if err != nil || got != tc.want {
			t.Fatal(tc, got, err)
		}
	}
	if _, err := WithinDirectory("relative", root); err == nil {
		t.Fatal("relative boundary accepted")
	}
	if runtime.GOOS == "windows" {
		for _, target := range []string{`D:\\Temp`, `\\server\share\Temp`} {
			if within, err := WithinDirectory(`C:\\workspace`, target); err != nil || within {
				t.Fatal(target, within, err)
			}
		}
	}
}
