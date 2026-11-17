package project

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/flock"
	"github.com/luoyu/rpskill/internal/storage"
	"github.com/luoyu/rpskill/internal/timeline"
	"go.yaml.in/yaml/v3"
)

var ErrReadOnly = errors.New("store is read-only")
var ErrBusy = errors.New("workspace is already opened by a writer or validator")
var ErrClosed = errors.New("store is closed")
var ErrRecoveryRequired = errors.New("pending transaction requires reopen/recovery")

type OpenOptions struct {
	ReadOnly   bool
	RepairTail bool
	RuntimeDir string
}
type InitOptions struct {
	Name       string
	RuntimeDir string
}
type TimelineOptions struct {
	ID        string
	Name      string
	Overrides timeline.SettingsPatch
}
type Issue struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}
type ValidationReport struct {
	Valid     bool    `json:"valid"`
	Project   string  `json:"project"`
	Issues    []Issue `json:"issues"`
	Timelines int     `json:"chats"`
	Events    int     `json:"events"`
}
type Recovery struct {
	File       string `json:"file"`
	Bytes      int    `json:"bytes"`
	Quarantine string `json:"quarantine"`
}

type Store struct {
	lockFS                *storage.FS
	mu                    sync.Mutex
	fs                    *storage.FS
	runtime               *storage.FS
	lock                  *flock.Flock
	readOnly              bool
	closed                bool
	recoveries            []Recovery
	ignoreUncommittedTail bool
}

func (s *Store) Root() string { return s.fs.Root() }
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return errors.Join(s.fs.Close(), s.lock.Close(), s.runtime.Close(), s.lockFS.Close())
}
func (s *Store) ready(write bool) error {
	if s.closed {
		return ErrClosed
	}
	if write && s.readOnly {
		return ErrReadOnly
	}
	// A failed multi-file commit may already have applied a subset. Reads must
	// not expose that uncommitted intermediate view while awaiting recovery.
	if pending, err := storage.Pending(s.runtime); err != nil {
		return err
	} else if pending {
		return ErrRecoveryRequired
	}

	return nil
}
func Open(directory string, opts OpenOptions) (*Store, error) {
	return openStore(directory, opts, true)
}
func openStore(directory string, opts OpenOptions, validate bool) (*Store, error) {
	if opts.ReadOnly && opts.RepairTail {
		return nil, errors.New("RepairTail requires a writable Store")
	}
	fs, err := storage.OpenFS(directory)
	if err != nil {
		return nil, err
	}
	rt, err := storage.OpenRuntimeFS(fs.Root(), opts.RuntimeDir)
	if err != nil {
		fs.Close()
		return nil, err
	}
	// The writer lock namespace is independent of both OpenOptions.RuntimeDir
	// and RPS_RUNTIME_DIR; changing session/cache locations must not create a
	// second writer for the same canonical Workspace.
	configRoot, err := os.UserConfigDir()
	if err != nil {
		fs.Close()
		rt.Close()
		return nil, err
	}
	lockFS, err := storage.OpenRuntimeFS(fs.Root(), filepath.Join(configRoot, "rpskill", "writer-locks"))
	if err != nil {
		fs.Close()
		rt.Close()
		return nil, err
	}
	if _, err := lockFS.ReadFile("store.lock"); err != nil && !errors.Is(err, os.ErrNotExist) {
		fs.Close()
		rt.Close()
		lockFS.Close()
		return nil, err
	}
	lock := flock.New(filepath.Join(lockFS.Root(), "store.lock"), flock.SetPermissions(0600))
	var ok bool
	if opts.ReadOnly {
		ok, err = lock.TryRLock()
	} else {
		ok, err = lock.TryLock()
	}
	if err != nil || !ok {
		fs.Close()
		rt.Close()
		lock.Close()
		lockFS.Close()
		if err == nil {
			err = ErrBusy
		}
		return nil, err
	}
	s := &Store{fs: fs, runtime: rt, lock: lock, lockFS: lockFS, readOnly: opts.ReadOnly}
	fail := func(err error) (*Store, error) { s.Close(); return nil, err }
	if opts.ReadOnly {
		if pending, e := storage.Pending(rt); e != nil {
			return fail(e)
		} else if pending {
			return fail(errors.New("pending transaction requires writable recovery"))
		}
	} else if err := storage.Recover(fs, rt); err != nil {
		return fail(err)
	}
	if opts.RepairTail {
		if err := s.repairTails(); err != nil {
			return fail(err)
		}
	}
	report := ValidationReport{Valid: true}
	if validate {
		report = s.validate()
	}
	if !report.Valid {
		return fail(fmt.Errorf("workspace validation failed: %s: %s", report.Issues[0].Path, report.Issues[0].Message))
	}
	return s, nil
}

func Init(directory string, opts InitOptions) (*Store, error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return nil, err
	}
	target := filepath.Join(parent, filepath.Base(absolute))
	existing, err := os.Lstat(target)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if !existing.IsDir() || existing.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("init target must be a real empty directory or absent")
		}
		entries, err := os.ReadDir(target)
		if err != nil {
			return nil, err
		}
		if len(entries) != 0 {
			return nil, errors.New("init refuses a nonempty directory")
		}
	}
	// Check the runtime boundary before publishing a newly created Workspace.
	// An invalid --runtime location must not leave an apparently successful init.
	rt, err := storage.OpenRuntimeFS(target, opts.RuntimeDir)
	if err != nil {
		return nil, err
	}
	if err := rt.Close(); err != nil {
		return nil, err
	}
	name := opts.Name
	if name == "" {
		name = filepath.Base(target)
	}
	config := DefaultConfig(name)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	temp, err := os.MkdirTemp(parent, ".rps-init-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(temp)
	fs, err := storage.OpenFS(temp)
	if err != nil {
		return nil, err
	}
	for _, dir := range []string{"worldbook", "characters", "chats/main"} {
		if err := fs.MkdirAll(dir); err != nil {
			fs.Close()
			return nil, err
		}
	}
	configData, _ := yaml.Marshal(config)
	storyData, _ := yaml.Marshal(DefaultStorySettings())
	for name, data := range map[string][]byte{"rp.yaml": configData, "chats/main/history.jsonl": {}, "chats/main/settings.yaml": storyData} {
		if err := fs.AtomicWrite(name, data); err != nil {
			fs.Close()
			return nil, err
		}
	}
	fs.Close()
	if existing != nil {
		if err := os.Remove(target); err != nil {
			return nil, err
		}
	}
	if err := os.Rename(temp, target); err != nil {
		if existing != nil {
			_ = os.Mkdir(target, existing.Mode().Perm())
		}
		return nil, err
	}
	return Open(target, OpenOptions{RuntimeDir: opts.RuntimeDir})
}
func (s *Store) config() (Config, error) {
	data, err := s.fs.ReadFile("rp.yaml")
	if err != nil {
		return Config{}, err
	}
	c, err := ParseConfig(data)
	if err != nil {
		return c, err
	}
	c.Name = filepath.Base(s.Root())
	c.Version = 3
	c.Chats = map[string]ChatInfo{}
	entries, err := s.fs.ReadDir("chats")
	if err != nil {
		return c, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !timeline.ValidChatID(entry.Name()) {
			return c, errors.New("chats must be a flat list of story directories")
		}
		settings, err := s.storySettings(entry.Name())
		if err != nil {
			return c, err
		}
		c.Chats[entry.Name()] = ChatInfo{Name: entry.Name(), Archived: settings.Archived}
	}
	if _, ok := c.Chats[c.LastChat]; !ok {
		return c, errors.New("last_chat does not exist")
	}
	return c, nil
}
func (s *Store) Config() (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(false); err != nil {
		return Config{}, err
	}
	return s.config()
}
func timelinePath(id, file string) string { return path.Join("chats", id, file) }
func (s *Store) metadata(id string) (timeline.Config, error) {
	if !timeline.ValidChatID(id) {
		return timeline.Config{}, errors.New("invalid chat id")
	}
	settings, err := s.storySettings(id)
	if err != nil {
		return timeline.Config{}, err
	}
	return timeline.Config{ID: id, Name: id, Archived: settings.Archived, SettingsPatch: settings.Patch()}, nil
}
func (s *Store) timelines() ([]timeline.Config, error) {
	c, err := s.config()
	if err != nil {
		return nil, err
	}
	entries, err := s.fs.ReadDir("chats")
	if err != nil {
		return nil, err
	}
	result := []timeline.Config{}
	for _, entry := range entries {
		if !entry.IsDir() {
			return nil, errors.New("chats contains non-directory entry")
		}
		m, err := s.metadata(entry.Name())
		if err != nil {
			return nil, err
		}
		result = append(result, m)
		files, err := s.fs.ReadDir("chats/" + entry.Name())
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if ((f.Name() != "history.jsonl" && f.Name() != "settings.yaml") || f.IsDir()) && ((f.Name() != "media" && f.Name() != "characters") || !f.IsDir()) {
				return nil, fmt.Errorf("unexpected chat file %s; chats are flat", f.Name())
			}
		}
	}
	if len(result) != len(c.Chats) {
		return nil, errors.New("chat list and folders differ")
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}
func (s *Store) Timelines() ([]timeline.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(false); err != nil {
		return nil, err
	}
	return s.timelines()
}
func (s *Store) ownHistory(id string) ([]timeline.Event, error) {
	log, err := s.fs.ReadJSONL(timelinePath(id, "history.jsonl"))
	if err != nil {
		return nil, err
	}
	if len(log.Tail) > 0 && !s.ignoreUncommittedTail {
		return nil, fmt.Errorf("unterminated JSONL tail at %s line %d; open with explicit RepairTail to quarantine", id, len(log.Lines)+1)
	}
	lines := make([][]byte, 0, len(log.Lines))
	for _, line := range log.Lines {
		lines = append(lines, line.Data)
	}
	return timeline.ParseMessages(lines, id)
}
func (s *Store) view(id, until string) ([]timeline.Event, timeline.State, error) {
	settings, err := s.storySettings(id)
	if err != nil {
		return nil, timeline.State{}, err
	}
	state := timeline.NewState(settings.Effective())
	state.Characters, err = s.characterStates(id)
	if err != nil {
		return nil, state, err
	}
	own, err := s.ownHistory(id)
	if err != nil {
		return nil, state, err
	}
	result := []timeline.Event{}
	found := until == ""
	for _, e := range own {
		if err := state.Apply(e); err != nil {
			return nil, state, err
		}
		result = append(result, e)
		if until != "" && e.ID == until {
			found = true
			break
		}
	}
	if !found {
		return nil, state, errors.New("message not found in chat")
	}
	return result, state, nil
}
func (s *Store) History(id string) ([]timeline.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(false); err != nil {
		return nil, err
	}
	if _, err := s.metadata(id); err != nil {
		return nil, err
	}
	return s.ownHistory(id)
}
func (s *Store) State(id string) (timeline.State, error) { return s.StateAt(id, "") }
func (s *Store) StateAt(id, eventID string) (timeline.State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(false); err != nil {
		return timeline.State{}, err
	}
	_, state, err := s.view(id, eventID)
	return state, err
}
func (s *Store) CreateTimeline(opts TimelineOptions) (timeline.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(true); err != nil {
		return timeline.Config{}, err
	}
	return s.createTimeline(opts, nil, nil)
}

// createTimeline is called only with the Store write lock held.
func (s *Store) createTimeline(opts TimelineOptions, initial []timeline.Event, extra []storage.Write) (timeline.Config, error) {
	meta := timeline.Config{ID: opts.ID, Name: opts.ID}
	if !timeline.ValidChatID(opts.ID) {
		return meta, errors.New("chat requires valid ID and name")
	}
	c, err := s.config()
	if err != nil {
		return meta, err
	}
	for existing := range c.Chats {
		if strings.EqualFold(existing, opts.ID) {
			return meta, errors.New("chat already exists")
		}
	}
	if _, err := s.fs.ReadDir("chats/" + opts.ID); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			err = errors.New("chat directory already exists")
		}
		return meta, err
	}
	settings, err := DefaultStorySettings().apply(opts.Overrides)
	if err != nil {
		return meta, err
	}
	meta.SettingsPatch = settings.Patch()
	a, err := s.prepareStorySettings(opts.ID, settings)
	if err != nil {
		return meta, err
	}
	var history bytes.Buffer
	if err := timeline.ValidateHistory(initial); err != nil {
		return meta, err
	}
	for _, e := range initial {
		if e.Timeline != opts.ID {
			return meta, errors.New("cross-chat initial history")
		}
		line, err := timeline.EncodeMessage(e)
		if err != nil {
			return meta, err
		}
		if len(line) > storage.MaxEventSize {
			return meta, errors.New("history message exceeds size limit")
		}
		history.Write(line)
		history.WriteByte('\n')
	}
	b, err := s.fs.PrepareWrite(timelinePath(opts.ID, "history.jsonl"), history.Bytes())
	if err != nil {
		return meta, err
	}
	return meta, storage.Commit(s.fs, s.runtime, append(extra, a, b))
}
func (s *Store) Append(id string, event timeline.Event) (timeline.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(true); err != nil {
		return event, err
	}
	if event.Type != timeline.Input && event.Type != timeline.Output {
		return event, errors.New("history only accepts input/output")
	}
	history, state, err := s.view(id, "")
	if err != nil {
		return event, err
	}
	meta, err := s.metadata(id)
	if err != nil {
		return event, err
	}
	if meta.Archived {
		return event, errors.New("timeline is archived")
	}
	if event.Version == 0 {
		event.Version = 1
	}
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	if event.Timeline != "" && event.Timeline != id {
		return event, errors.New("cross-timeline append rejected")
	}
	event.Timeline = id
	// Only the newest input can receive output in a sequential history.
	event.ID = timeline.MessageID(id, len(history)+1)
	if event.Type == timeline.Input {
		event.InputID = event.ID
	}
	if event.Type == timeline.Output {
		input := ""
		for _, e := range history {
			if e.Type == timeline.Input {
				input = e.ID
			}
		}
		if input == "" || event.InputID != input {
			return event, errors.New("output must belong to the latest input; branch to continue older history")
		}
	}
	if err := timeline.ValidateHistory(append(history, event)); err != nil {
		return event, err
	}
	if err := s.validateAppendContent(id, state, event); err != nil {
		return event, err
	}
	data, err := timeline.EncodeMessage(event)
	if err != nil {
		return event, err
	}
	if err := s.fs.AppendJSONL(timelinePath(id, "history.jsonl"), data); err != nil {
		return event, err
	}
	return event, nil
}

func (s *Store) writeConfig(c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	raw, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return s.fs.AtomicWrite("rp.yaml", raw)
}
func (s *Store) UpdateConfig(c Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(true); err != nil {
		return err
	}
	old, err := s.config()
	if err != nil {
		return err
	}
	before, _ := json.Marshal(old.Chats)
	after, _ := json.Marshal(c.Chats)
	if !bytes.Equal(before, after) {
		return errors.New("use chat management to change the chat list")
	}
	if err := c.Validate(); err != nil {
		return err
	}
	if _, ok := old.Chats[c.LastChat]; !ok {
		return errors.New("last_chat does not exist")
	}
	if c.Name != old.Name || c.Version != old.Version {
		return errors.New("project metadata is derived from the workspace")
	}
	return s.writeConfig(c)
}

func (s *Store) Recoveries() []Recovery {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Recovery{}, s.recoveries...)
}
