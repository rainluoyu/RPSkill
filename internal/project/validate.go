package project

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"time"

	"github.com/luoyu/rpskill/internal/character"
	"github.com/luoyu/rpskill/internal/storage"
	"github.com/luoyu/rpskill/internal/timeline"
	"github.com/luoyu/rpskill/internal/worldbook"
)

func (s *Store) Validate() (ValidationReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(false); err != nil {
		return ValidationReport{}, err
	}
	report := s.validate()
	return report, nil
}
func Validate(directory string) (ValidationReport, error) {
	s, err := openStore(directory, OpenOptions{ReadOnly: true}, false)
	if err != nil {
		return ValidationReport{Project: directory, Issues: []Issue{{Path: ".", Code: "open_error", Message: err.Error()}}}, err
	}
	defer s.Close()
	return s.Validate()
}
func (s *Store) validate() ValidationReport {
	report := ValidationReport{Valid: true, Project: s.Root(), Issues: []Issue{}}
	add := func(file, code string, err error) {
		if err != nil {
			report.Valid = false
			report.Issues = append(report.Issues, Issue{file, code, err.Error()})
		}
	}
	config, err := s.config()
	add("rp.yaml", "invalid_config", err)
	// Reject symlinks and special files throughout the portable project, even if
	// a malicious attachment isn't referenced by a card/event yet.
	var walk func(string) error
	walk = func(dir string) error {
		entries, err := s.fs.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			name := entry.Name()
			if dir != "." {
				name = path.Join(dir, name)
			}
			if err := storage.ValidPath(name); err != nil {
				return err
			}
			if entry.IsDir() {
				if err := walk(name); err != nil {
					return err
				}
			} else {
				info, err := entry.Info()
				if err != nil {
					return err
				}
				if !info.Mode().IsRegular() {
					return fmt.Errorf("non-regular file %s", name)
				}
			}
		}
		return nil
	}
	add(".", "unsafe_path", walk("."))
	for _, dir := range []string{"worldbook", "characters", "chats"} {
		_, err := s.fs.ReadDir(dir)
		add(dir, "missing_directory", err)
	}
	cards, err := character.Scan(s.Root())
	add("characters", "invalid_character", err)
	index, err := worldbook.Scan(s.Root())
	add("worldbook", "invalid_worldbook", err)
	_ = index
	cardIDs := map[string]bool{}
	for _, card := range cards {
		cardIDs[card.Metadata.ID] = true
	}
	metas, err := s.timelines()
	add("chats", "invalid_timeline", err)
	if err != nil {
		return report
	}
	if len(metas) == 0 {
		add("chats", "missing_timeline", errors.New("workspace requires at least one timeline"))
	}
	report.Timelines = len(metas)
	mediaIndex, mediaErr := s.mediaIndex()
	add(".", "invalid_media_index", mediaErr)
	ids := map[string]string{}
	for _, meta := range metas {
		file := timelinePath(meta.ID, "history.jsonl")
		own, err := s.ownHistory(meta.ID)
		add(file, "invalid_jsonl", err)
		if err != nil {
			continue
		}
		report.Events += len(own)
		for _, e := range own {
			if prior, ok := ids[e.ID]; ok {
				add(file, "duplicate_event_id", fmt.Errorf("%s also exists in %s", e.ID, prior))
			}
			ids[e.ID] = meta.ID
		}
		events, _, err := s.view(meta.ID, "")
		add(file, "invalid_history", err)
		if err != nil {
			continue
		}
		priorMedia := map[string]timeline.MediaReference{}
		for _, e := range events {
			if e.Type == timeline.Input && timeline.OrdinaryCharacter(e.Speaker) && !cardIDs[e.Speaker] {
				add(file, "missing_character", fmt.Errorf("event %s refers to missing character %s", e.ID, e.Speaker))
			}
			for _, name := range e.Characters {
				if !cardIDs[name] {
					add(file, "missing_character", fmt.Errorf("output refers to missing character %s", name))
				}
			}
			for _, m := range e.Media {
				if mediaErr == nil {
					add(file, "invalid_media_binding", validateMediaBinding(m, meta.ID, mediaIndex))
				}
				if err := validateMediaPath(m); err != nil {
					add(file, "invalid_media", err)
					continue
				}
				prior, seen := priorMedia[m.ID]
				if seen && (prior.Path != m.Path || prior.MIME != m.MIME) {
					add(file, "invalid_media", fmt.Errorf("media id %s has conflicting references", m.ID))
					continue
				}
				priorMedia[m.ID] = m
				_, err := s.fs.ReadFile(timelinePath(meta.ID, m.Path))
				add(m.Path, "missing_media", err)
			}
		}
	}
	// Settings are shared project defaults, not historical snapshots.
	_ = config
	return report
}

func (s *Store) repairTails() error {
	metas, err := s.timelines()
	if err != nil {
		return err
	}
	type repair struct {
		id  string
		log storage.JSONL
	}
	var work []repair
	// Validate every complete physical event before touching any damaged tail.
	for _, m := range metas {
		log, err := s.fs.ReadJSONL(timelinePath(m.ID, "history.jsonl"))
		if err != nil {
			return err
		}
		lines := make([][]byte, 0, len(log.Lines)+1)
		for _, line := range log.Lines {
			lines = append(lines, line.Data)
		}
		if _, err := timeline.ParseMessages(lines, m.ID); err != nil {
			return fmt.Errorf("cannot repair: %w", err)
		}
		if len(log.Tail) > 0 {
			if json.Valid(log.Tail) {
				if _, err := timeline.ParseMessages(append(lines, log.Tail), m.ID); err != nil {
					return fmt.Errorf("complete but invalid tail must not be repaired: %w", err)
				}
			}
			work = append(work, repair{m.ID, log})
		}
	}
	// Check complete history semantics, ancestry, configuration snapshots and
	// media before any quarantine/truncate. Failed Open must not mutate data.
	s.ignoreUncommittedTail = true
	report := s.validate()
	s.ignoreUncommittedTail = false
	if !report.Valid {
		return fmt.Errorf("cannot repair invalid committed prefix: %s: %s", report.Issues[0].Path, report.Issues[0].Message)
	}
	for _, r := range work {
		unique, err := timeline.NewID("tail")
		if err != nil {
			return err
		}
		quarantine := path.Join("recovery", time.Now().UTC().Format("20060102T150405Z")+"-"+unique+".bin")
		if err := s.runtime.AtomicWrite(quarantine, r.log.Tail); err != nil {
			return err
		}
		file := timelinePath(r.id, "history.jsonl")
		if err := s.fs.TruncateJSONL(file, r.log.TailOffset); err != nil {
			return err
		}
		s.recoveries = append(s.recoveries, Recovery{File: file, Bytes: len(r.log.Tail), Quarantine: path.Join(s.runtime.Root(), quarantine)})
	}
	return nil
}

// CopyTo migrates an entire validated portable Workspace with no runtime data.
// It writes a new destination only, never silently overwrites an existing tree.
func (s *Store) CopyTo(destination string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(false); err != nil {
		return err
	}
	report := s.validate()
	if !report.Valid {
		return errors.New("refusing to migrate invalid workspace")
	}
	return copyWorkspace(s.fs, destination)
}
