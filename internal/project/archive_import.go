package project

import (
	"bytes"
	"errors"
	"fmt"
	"go.yaml.in/yaml/v3"
	"os"
	"sort"
	"strings"

	"github.com/luoyu/rpskill/internal/media"
	"github.com/luoyu/rpskill/internal/storage"
	"github.com/luoyu/rpskill/internal/timeline"
)

func archiveShape(kind string, files map[string][]byte) ([]string, error) {
	ids := map[string]bool{}
	for name := range files {
		parts := strings.Split(name, "/")
		switch kind {
		case "project", "chats":
			if name == "rp.yaml" {
				continue
			}
			if len(parts) < 3 || (parts[0] != "characters" && parts[0] != "worldbook" && parts[0] != "chats") {
				return nil, fmt.Errorf("unexpected archive path %s", name)
			}
			if parts[0] == "chats" {
				if !timeline.ValidChatID(parts[1]) {
					return nil, errors.New("invalid timeline path")
				}
				ids[parts[1]] = true
				if !(len(parts) == 3 && (parts[2] == "history.jsonl" || parts[2] == "settings.yaml")) && !(len(parts) >= 4 && parts[2] == "media") && !(len(parts) == 4 && parts[2] == "characters" && strings.HasSuffix(parts[3], ".md")) {
					return nil, errors.New("unexpected timeline archive file")
				}
			}
		case "characters":
			if len(parts) < 3 || parts[0] != "characters" {
				return nil, errors.New("character ZIP must contain characters/<id>/ files")
			}
			ids[parts[1]] = true
		case "worldbook":
			if len(parts) != 3 || parts[0] != "worldbook" || !strings.HasSuffix(parts[2], ".md") {
				return nil, errors.New("worldbook ZIP requires strict two-level Markdown")
			}
			ids[parts[1]] = true
		default:
			return nil, errors.New("unknown import kind")
		}
	}
	if (kind == "characters" || kind == "chats") && len(ids) != 1 {
		return nil, errors.New("archive must contain exactly one character/timeline")
	}
	if kind == "project" || kind == "chats" {
		if _, ok := files["rp.yaml"]; !ok {
			return nil, errors.New("archive requires rp.yaml")
		}
		if len(ids) == 0 {
			return nil, errors.New("archive requires timeline")
		}
	}
	result := []string{}
	for id := range ids {
		result = append(result, id)
	}
	sort.Strings(result)
	return result, nil
}
func candidate(files map[string][]byte) (func(), *Store, error) {
	dir, err := os.MkdirTemp("", "rps-import-")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	fs, err := storage.OpenFS(dir)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	done := func() { fs.Close(); cleanup() }
	for _, dir := range []string{"characters", "worldbook", "chats"} {
		if err := fs.MkdirAll(dir); err != nil {
			done()
			return nil, nil, err
		}
	}
	for name, data := range files {
		if err := fs.AtomicWrite(name, data); err != nil {
			done()
			return nil, nil, err
		}
	}
	staged := &Store{fs: fs}
	report := staged.validate()
	if !report.Valid {
		done()
		return nil, nil, fmt.Errorf("archive validation: %s: %s", report.Issues[0].Path, report.Issues[0].Message)
	}
	if _, err := staged.mediaIndex(); err != nil {
		done()
		return nil, nil, err
	}
	return done, staged, nil
}
func (s *Store) ImportArchive(kind string, data []byte) (ArchiveResult, error) {
	files, err := media.DecodeZIP(data)
	if err != nil {
		return ArchiveResult{}, err
	}
	ids, err := archiveShape(kind, files)
	if err != nil {
		return ArchiveResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(true); err != nil {
		return ArchiveResult{}, err
	}
	existing, err := s.archiveFiles()
	if err != nil {
		return ArchiveResult{}, err
	}
	merged := map[string][]byte{}
	for name, data := range existing {
		merged[name] = data
	}
	writes := []storage.Write{}
	if kind == "project" {
		metas, err := s.timelines()
		if err != nil {
			return ArchiveResult{}, err
		}
		if len(metas) != 1 || metas[0].ID != "main" || len(existing) != 3 || len(existing["chats/main/history.jsonl"]) != 0 {
			return ArchiveResult{}, fmt.Errorf("%w: project import requires pristine initialized Workspace", media.ErrConflict)
		}
		merged = files
		if _, ok := files["chats/main/history.jsonl"]; !ok {
			w, err := s.fs.PrepareTreeRemoval("chats/main")
			if err != nil {
				return ArchiveResult{}, err
			}
			writes = append(writes, w)
		}
	} else {
		if kind == "chats" {
			_, err := ParseConfig(files["rp.yaml"])
			if err != nil {
				return ArchiveResult{}, err
			}
			current, err := s.config()
			if err != nil {
				return ArchiveResult{}, err
			}
			for file := range files {
				parts := strings.Split(file, "/")
				if len(parts) >= 3 && parts[0] == "chats" {
					if _, exists := current.Chats[parts[1]]; exists {
						return ArchiveResult{}, media.ErrConflict
					}
				}
			}
			raw, err := yaml.Marshal(current)
			if err != nil {
				return ArchiveResult{}, err
			}
			merged["rp.yaml"] = raw
		}

		for name, body := range files {
			if kind == "chats" && name == "rp.yaml" {
				continue
			}
			if old, exists := existing[name]; exists && !bytes.Equal(old, body) {
				return ArchiveResult{}, fmt.Errorf("%w: existing file %s differs", media.ErrConflict, name)
			}
			// Timeline identity may never silently collide, including identical empty histories.
			if strings.HasPrefix(name, "chats/") {
				if _, exists := existing[name]; exists {
					return ArchiveResult{}, fmt.Errorf("%w: timeline already exists", media.ErrConflict)
				}
			}
			merged[name] = body
		}
	}
	portableNames := map[string]string{}
	for name := range merged {
		folded := strings.ToLower(name)
		if prior, ok := portableNames[folded]; ok && prior != name {
			return ArchiveResult{}, fmt.Errorf("%w: nonportable case collision %s / %s", media.ErrConflict, prior, name)
		}
		portableNames[folded] = name
	}
	cleanup, _, err := candidate(merged)
	if err != nil {
		return ArchiveResult{}, err
	}
	defer cleanup()
	names := make([]string, 0, len(merged))
	for name := range merged {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		body := merged[name]
		if old, exists := existing[name]; exists && bytes.Equal(old, body) {
			continue
		}
		w, err := s.fs.PrepareWrite(name, body)
		if err != nil {
			return ArchiveResult{}, err
		}
		writes = append(writes, w)
	}
	if err := storage.Commit(s.fs, s.runtime, writes); err != nil {
		return ArchiveResult{}, err
	}
	return ArchiveResult{Kind: kind, IDs: ids, Files: len(files)}, nil
}
