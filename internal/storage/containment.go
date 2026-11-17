package storage

import (
	"errors"
	"path/filepath"
	"strings"
)

// WithinDirectory compares absolute paths whose existing parents have already
// been resolved or checked for symlinks by the caller. Different Windows volumes
// are outside one another, not a failure to establish the boundary.
func WithinDirectory(parent, target string) (bool, error) {
	if !filepath.IsAbs(parent) || !filepath.IsAbs(target) {
		return false, errors.New("containment requires absolute paths")
	}
	if !strings.EqualFold(filepath.VolumeName(parent), filepath.VolumeName(target)) {
		return false, nil
	}
	rel, err := filepath.Rel(parent, target)
	if err != nil {
		return false, err
	}
	return rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)), nil
}
