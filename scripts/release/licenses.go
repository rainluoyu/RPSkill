package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Include original licensing texts instead of treating the project's MIT
// license as a replacement for the toolchain or bundled dependencies' terms.
func dependencyLicenses(goBin, npmBin string) ([]entry, error) {
	var files []entry
	var notices strings.Builder
	notices.WriteString("# Third-party notices\n\nRPSkill's root LICENSE applies to RPSkill's own code. The following original licensing texts apply to bundled third-party components. Frontend entries conservatively include the installed production dependency graph (including build-time/optional peers); listing does not claim every component is shipped in the browser bundle. No dependency source, node_modules or host credentials are distributed here.\n\n")
	add := func(kind, name, version, directory string) error {
		if !validArchivePath(name) || strings.ContainsAny(version, "/\\\x00\r\n") {
			return errors.New("unsafe dependency identity")
		}
		entries, err := os.ReadDir(directory)
		if err != nil {
			return err
		}
		count := 0
		fmt.Fprintf(&notices, "## %s: %s %s\n\n", kind, name, version)
		for _, item := range entries {
			lower := strings.ToLower(item.Name())
			if !strings.HasPrefix(lower, "license") && !strings.HasPrefix(lower, "licence") && !strings.HasPrefix(lower, "copying") && !strings.HasPrefix(lower, "notice") && !strings.HasPrefix(lower, "patents") && !strings.HasPrefix(lower, "thirdparty") {
				continue
			}
			if item.IsDir() {
				continue
			}
			data, err := readRegular(filepath.Join(directory, item.Name()))
			if err != nil {
				return err
			}
			if len(data) == 0 {
				continue
			}
			name := filepath.ToSlash(filepath.Join("licenses", kind, name+"@"+version, item.Name()))
			files = append(files, entry{name, 0644, data})
			fmt.Fprintf(&notices, "- [%s](%s)\n", item.Name(), name)
			count++
		}
		if count == 0 {
			return fmt.Errorf("no original license found for %s %s in %s", name, version, directory)
		}
		notices.WriteByte('\n')
		return nil
	}
	type module struct {
		Path, Version, Dir string
		Main               bool
	}
	modules := map[string]module{}
	// Resolve actual compile dependencies for all six targets. go list -m all
	// would unnecessarily fetch test-only module graphs on offline builds.
	for _, t := range platforms {
		cmd := exec.Command(goBin, "list", "-mod=readonly", "-deps", "-json", "./cmd/rps")
		cmd.Env = platformEnvironment(t)
		data, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("resolve %s Go licenses: %w", t.key(), err)
		}
		dec := json.NewDecoder(bytes.NewReader(data))
		for {
			var p struct{ Module *module }
			err := dec.Decode(&p)
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			if p.Module != nil && !p.Module.Main {
				modules[p.Module.Path] = *p.Module
			}
		}
	}
	var names []string
	for name := range modules {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		m := modules[name]
		if err := add("go", m.Path, m.Version, m.Dir); err != nil {
			return nil, err
		}
	}
	if err := add("toolchain", "go", runtime.Version(), runtime.GOROOT()); err != nil {
		return nil, err
	}
	cmd := exec.Command(npmBin, "ls", "--omit=dev", "--parseable", "--all")
	cmd.Dir = "web"
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("resolve production frontend licenses (run npm ci first): %w", err)
	}
	var packages []struct{ name, version, directory string }
	seen := map[string]bool{}
	web, err := filepath.Abs("web")
	if err != nil {
		return nil, err
	}
	for _, directory := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if directory == web || directory == "" {
			continue
		}
		rel, err := filepath.Rel(filepath.Join(web, "node_modules"), directory)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, errors.New("npm dependency resolved outside node_modules")
		}
		raw, err := readRegular(filepath.Join(directory, "package.json"))
		if err != nil {
			return nil, err
		}
		var p struct{ Name, Version string }
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		if p.Name == "" || p.Version == "" {
			return nil, errors.New("npm dependency missing identity")
		}
		key := p.Name + "@" + p.Version
		if seen[key] {
			continue
		}
		seen[key] = true
		packages = append(packages, struct{ name, version, directory string }{p.Name, p.Version, directory})
	}
	sort.Slice(packages, func(i, j int) bool {
		return packages[i].name+packages[i].version < packages[j].name+packages[j].version
	})
	for _, p := range packages {
		if err := add("npm", p.name, p.version, p.directory); err != nil {
			return nil, err
		}
	}
	files = append(files, entry{"THIRD_PARTY_NOTICES.md", 0644, []byte(notices.String())})
	return files, nil
}
