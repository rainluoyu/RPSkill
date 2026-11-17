// Package rpskill exposes the bundled RP instructions for installation.
// The installer supplies the native CLI separately from os.Executable.
package rpskill

import (
	"embed"
	"io/fs"
)

// Do not embed rp or rp/bin: a bundled executable would recursively enlarge
// subsequent builds and could install a stale or wrong-platform CLI.
//
//go:embed skill/rp/SKILL.md skill/rp/builtins/*.md skill/rp/references/*.md
var texts embed.FS

// SkillFiles returns read-only Skill text with SKILL.md at the filesystem root.
// It includes builtins and references, but no binary, tests or phase records.
func SkillFiles() fs.FS {
	files, err := fs.Sub(texts, "skill/rp")
	if err != nil {
		panic(err) // The literal subdirectory is guaranteed by the embed patterns.
	}
	return files
}
