package runbook

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A runbook is addressed by its ref: a slash-separated path under the library root, up to
// MaxDepth segments deep ("name", "folder/name", "gcp/disks/1-find"). A directory holding a
// runbook.md is a runbook; any other directory is a folder.
//
// Layout of one runbook directory:
//
//	<root>/<ref>/runbook.md           source of truth
//	<root>/<name>/runs.log            audit log, JSONL, pruned to Retention
//	<root>/<name>/versions/vN.md      explicit snapshots
//	<root>/<name>/.runbook/state.json step ids + last output per step
//	<root>/<name>/.gitignore          ignores runs.log and .runbook/
const (
	docFile    = "runbook.md"
	logFile    = "runs.log"
	stateDir   = ".runbook"
	stateFile  = "state.json"
	versionDir = "versions"
	gitignore  = "runs.log\n.runbook/\n"

	// Retention is how long runs.log keeps entries.
	Retention = 30 * 24 * time.Hour
)

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ErrExists is returned when creating, renaming or moving onto a name that's taken.
var ErrExists = errors.New("that name is already taken in this folder")

// Store is the runbook library rooted at Root (normally ~/runbooks).
type Store struct {
	Root     string
	TrashDir string     // where Trash moves runbooks (normally ~/.Trash)
	mu       sync.Mutex // serialises writes to state.json and runs.log
}

// ValidName reports whether name is usable as a runbook directory name.
func ValidName(name string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("name %q: use letters, digits, '.', '_' or '-' (max 64), starting with a letter or digit", name)
	}
	return nil
}

// MaxDepth is how many path segments a ref may have: a runbook can sit at most
// MaxDepth-1 folders deep.
const MaxDepth = 5

// ValidRef checks a runbook or folder ref: slash-separated names, at most MaxDepth deep.
func ValidRef(ref string) error {
	parts := strings.Split(ref, "/")
	if len(parts) > MaxDepth {
		return fmt.Errorf("%q: at most %d levels of folders", ref, MaxDepth-1)
	}
	for _, p := range parts {
		if err := ValidName(p); err != nil {
			return err
		}
	}
	return nil
}

// Depth is how many segments a ref has (0 for the library root).
func Depth(ref string) int {
	if ref == "" {
		return 0
	}
	return strings.Count(ref, "/") + 1
}

// Inside reports whether ref is the folder itself or sits anywhere beneath it. It is what
// stops a folder being dragged into its own descendant.
func Inside(ref, folder string) bool {
	if folder == "" {
		return true // everything is inside the library root
	}
	return ref == folder || strings.HasPrefix(ref, folder+"/")
}

// SplitRef returns a ref's folder ("" at the top level) and name.
func SplitRef(ref string) (folder, name string) {
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		return ref[:i], ref[i+1:]
	}
	return "", ref
}

// JoinRef builds a ref from a folder ("" for the top level) and a name.
func JoinRef(folder, name string) string {
	if folder == "" {
		return name
	}
	return folder + "/" + name
}

func (s *Store) dir(ref string) string { return filepath.Join(s.Root, filepath.FromSlash(ref)) }

func (s *Store) isRunbook(ref string) bool {
	_, err := os.Stat(filepath.Join(s.dir(ref), docFile))
	return err == nil
}

// isFolder reports whether ref is an existing folder: a directory that is not a runbook.
// The library root ("") is always a folder.
func (s *Store) isFolder(ref string) bool {
	if ref == "" {
		return true
	}
	fi, err := os.Stat(s.dir(ref))
	return err == nil && fi.IsDir() && !s.isRunbook(ref)
}

// --- library ---

// Summary is one row in the library.
type Summary struct {
	Name     string    `json:"name"`   // the ref: "name" or "folder/name"
	Folder   string    `json:"folder"` // "" at the top level
	Steps    int       `json:"steps"`
	Modified time.Time `json:"modified"`
	LastRun  time.Time `json:"lastRun"`
}

// List returns every runbook anywhere in the library, most recently modified first.
// Folders (including empty ones) are listed by Folders.
func (s *Store) List() ([]Summary, error) {
	var out []Summary
	err := s.walk("", func(ref string, isRunbook bool) {
		if isRunbook {
			if sum, ok := s.summary(ref); ok {
				out = append(out, sum)
			}
		}
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	return out, nil
}

// walk visits every runbook and folder under ref, depth first, without descending into
// runbooks or past MaxDepth.
func (s *Store) walk(ref string, visit func(ref string, isRunbook bool)) error {
	entries, err := os.ReadDir(s.dir(ref))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		if ref == "" {
			return err
		}
		return nil // an unreadable folder shouldn't hide the rest of the library
	}
	for _, e := range entries {
		if !e.IsDir() || ValidName(e.Name()) != nil {
			continue
		}
		child := JoinRef(ref, e.Name())
		if s.isRunbook(child) {
			visit(child, true)
			continue
		}
		visit(child, false)
		if Depth(child) < MaxDepth-1 {
			s.walk(child, visit)
		}
	}
	return nil
}

func (s *Store) summary(ref string) (Summary, bool) {
	path := filepath.Join(s.dir(ref), docFile)
	fi, err := os.Stat(path)
	if err != nil {
		return Summary{}, false
	}
	folder, _ := SplitRef(ref)
	sum := Summary{Name: ref, Folder: folder, Modified: fi.ModTime()}
	if b, err := os.ReadFile(path); err == nil {
		if d, err := Parse(b); err == nil {
			sum.Steps = len(d.Steps)
		}
	}
	sum.LastRun = s.readState(ref).LastRun
	return sum, true
}

// Folders lists every folder ref in the library, including empty ones, parents before
// children and alphabetical within a level.
func (s *Store) Folders() ([]string, error) {
	var out []string
	if err := s.walk("", func(ref string, isRunbook bool) {
		if !isRunbook {
			out = append(out, ref)
		}
	}); err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// CreateFolder makes an empty folder at ref; its parent folder must already exist.
func (s *Store) CreateFolder(ref string) error {
	if err := ValidRef(ref); err != nil {
		return err
	}
	parent, _ := SplitRef(ref)
	if !s.isFolder(parent) {
		return fmt.Errorf("folder %q not found", parent)
	}
	if Depth(ref) > MaxDepth-1 {
		return fmt.Errorf("at most %d levels of folders", MaxDepth-1)
	}
	if err := os.MkdirAll(s.Root, 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(s.dir(ref), 0o755); errors.Is(err, os.ErrExist) {
		return ErrExists
	} else {
		return err
	}
}

// MoveFolder moves a folder (with everything in it) to newRef, which is also how folders are
// renamed. It refuses to move a folder into itself or one of its own descendants, and
// refuses moves that would push its contents past MaxDepth.
func (s *Store) MoveFolder(ref, newRef string) error {
	if err := ValidRef(ref); err != nil {
		return err
	}
	if err := ValidRef(newRef); err != nil {
		return err
	}
	if !s.isFolder(ref) || ref == "" {
		return fmt.Errorf("folder %q not found", ref)
	}
	if Inside(newRef, ref) {
		return fmt.Errorf("can't move %q into itself", ref)
	}
	parent, _ := SplitRef(newRef)
	if !s.isFolder(parent) {
		return fmt.Errorf("folder %q not found", parent)
	}
	deepest, err := s.deepestUnder(ref)
	if err != nil {
		return err
	}
	if Depth(newRef)+deepest > MaxDepth {
		return fmt.Errorf("that would nest deeper than %d levels", MaxDepth-1)
	}
	if _, err := os.Stat(s.dir(newRef)); err == nil {
		return ErrExists
	}
	return os.Rename(s.dir(ref), s.dir(newRef))
}

// deepestUnder returns how many levels the deepest entry sits below ref (1 = ref itself).
func (s *Store) deepestUnder(ref string) (int, error) {
	deepest := 1
	err := s.walk(ref, func(child string, _ bool) {
		if d := Depth(child) - Depth(ref) + 1; d > deepest {
			deepest = d
		}
	})
	return deepest, err
}

// TrashFolder moves a folder and everything in it to TrashDir. It returns the new path.
func (s *Store) TrashFolder(ref string) (string, error) {
	if err := ValidRef(ref); err != nil {
		return "", err
	}
	if !s.isFolder(ref) || ref == "" {
		return "", fmt.Errorf("folder %q not found", ref)
	}
	return s.moveToTrash(ref)
}

// --- load / save ---

// Load reads a runbook and gives every step a stable id (see assignIDs).
func (s *Store) Load(name string) (Doc, error) {
	if err := ValidRef(name); err != nil {
		return Doc{}, err
	}
	b, err := os.ReadFile(filepath.Join(s.dir(name), docFile))
	if err != nil {
		return Doc{}, err
	}
	d, err := Parse(b)
	if err != nil {
		return Doc{}, fmt.Errorf("%s/%s: %w", name, docFile, err)
	}
	if d.Name == "" {
		_, d.Name = SplitRef(name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.readState(name)
	assignIDs(&d, st.Steps)
	st.Steps = refs(d)
	s.ensureGitignore(name)
	return d, s.writeState(name, st)
}

// Save writes runbook.md atomically. dirName is the runbook's directory.
func (s *Store) Save(dirName string, d Doc) error {
	if err := ValidRef(dirName); err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(s.dir(dirName), docFile), Serialize(d)); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.readState(dirName)
	st.Steps = refs(d)
	for id := range st.Outputs {
		if _, _, ok := d.StepByID(id); !ok {
			delete(st.Outputs, id)
		}
	}
	for id := range st.StepArgs {
		if _, _, ok := d.StepByID(id); !ok {
			delete(st.StepArgs, id)
		}
	}
	return s.writeState(dirName, st)
}

// Create makes a new runbook at ref ("name" or "folder/name"; the folder must exist).
// d.Name is set to the ref's name. It fails if the name is taken.
func (s *Store) Create(ref string, d Doc) error {
	if err := ValidRef(ref); err != nil {
		return err
	}
	folder, name := SplitRef(ref)
	if !s.isFolder(folder) {
		return fmt.Errorf("folder %q not found", folder)
	}
	if err := os.MkdirAll(s.Root, 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(s.dir(ref), 0o755); errors.Is(err, os.ErrExist) {
		return ErrExists
	} else if err != nil {
		return err
	}
	d.Name = name
	for i := range d.Steps {
		if d.Steps[i].ID == "" {
			d.Steps[i].ID = newID()
		}
	}
	s.ensureGitignore(ref)
	return s.Save(ref, d)
}

// Rename moves a runbook to newRef: a new name, a different folder, or both. Everything in
// its directory (versions, cache, runs.log) moves with it. d.Name follows the new name.
func (s *Store) Rename(old string, d Doc, newRef string) (Doc, error) {
	if err := ValidRef(newRef); err != nil {
		return d, err
	}
	folder, name := SplitRef(newRef)
	if !s.isFolder(folder) {
		return d, fmt.Errorf("folder %q not found", folder)
	}
	if _, err := os.Stat(s.dir(newRef)); err == nil {
		return d, ErrExists
	}
	if err := os.Rename(s.dir(old), s.dir(newRef)); err != nil {
		return d, err
	}
	d.Name = name
	return d, s.Save(newRef, d)
}

// Trash moves a runbook's directory (runbook.md, versions, cache and runs.log) into
// TrashDir, so a mistaken delete can be undone from the Finder. It returns the new path.
func (s *Store) Trash(name string) (string, error) {
	if err := ValidRef(name); err != nil {
		return "", err
	}
	if !s.isRunbook(name) {
		return "", fmt.Errorf("runbook %q not found", name)
	}
	return s.moveToTrash(name)
}

// moveToTrash moves the directory at ref into TrashDir under its base name, adding " 2",
// " 3"… if the Trash already holds that name.
func (s *Store) moveToTrash(ref string) (string, error) {
	if s.TrashDir == "" {
		return "", errors.New("no trash directory configured")
	}
	src := s.dir(ref)
	_, base := SplitRef(ref)
	dst := filepath.Join(s.TrashDir, base)
	for i := 2; ; i++ {
		if _, err := os.Lstat(dst); errors.Is(err, os.ErrNotExist) {
			break
		}
		dst = filepath.Join(s.TrashDir, fmt.Sprintf("%s %d", base, i))
	}
	if err := os.MkdirAll(s.TrashDir, 0o700); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Rename(src, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// seededMarker records that the first-launch demo was offered, so deleting every runbook
// later leaves an empty library instead of bringing the demo back.
const seededMarker = ".seeded"

// FirstLaunch reports whether the library has never been used, and marks it used.
func (s *Store) FirstLaunch(empty bool) bool {
	marker := filepath.Join(s.Root, seededMarker)
	if _, err := os.Stat(marker); err == nil {
		return false
	}
	if err := os.MkdirAll(s.Root, 0o755); err != nil {
		return false
	}
	os.WriteFile(marker, nil, 0o644)
	return empty
}

// --- versions ---

// Version is one saved snapshot.
type Version struct {
	N      int       `json:"n"`
	Saved  time.Time `json:"saved"`
	Parent string    `json:"parent"`
}

// Versions lists versions/vN.md, newest first.
func (s *Store) Versions(name string) ([]Version, error) {
	entries, err := os.ReadDir(filepath.Join(s.dir(name), versionDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Version
	for _, e := range entries {
		n, ok := versionNumber(e.Name())
		if !ok {
			continue
		}
		v := Version{N: n}
		if fi, err := e.Info(); err == nil {
			v.Saved = fi.ModTime()
		}
		if b, err := os.ReadFile(filepath.Join(s.dir(name), versionDir, e.Name())); err == nil {
			if d, err := Parse(b); err == nil {
				v.Parent = d.Parent
			}
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].N > out[j].N })
	return out, nil
}

// SaveVersion stamps the next version number into d, saves it, and snapshots it to
// versions/vN.md. Versions are only ever created by this call (never on autosave).
func (s *Store) SaveVersion(name string, d Doc) (Doc, error) {
	vs, err := s.Versions(name)
	if err != nil {
		return d, err
	}
	next := d.Version + 1
	if len(vs) > 0 && vs[0].N >= next {
		next = vs[0].N + 1
	}
	d.Version = next
	if err := s.Save(name, d); err != nil {
		return d, err
	}
	if err := os.MkdirAll(filepath.Join(s.dir(name), versionDir), 0o755); err != nil {
		return d, err
	}
	return d, writeAtomic(filepath.Join(s.dir(name), versionDir, fmt.Sprintf("v%d.md", next)), Serialize(d))
}

func versionNumber(file string) (int, bool) {
	if !strings.HasPrefix(file, "v") || !strings.HasSuffix(file, ".md") {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(file, "v"), ".md"))
	return n, err == nil && n > 0
}

// --- fork (Generate from selected, screen 2c) ---

// Fork creates newName from the given steps of src, optionally carrying their last
// outputs and a parent link.
func (s *Store) Fork(srcName string, src Doc, stepIDs []string, newName string, keepOutputs, linkParent bool) (Doc, error) {
	_, leaf := SplitRef(newName)
	d := NewDoc(leaf, src.Cwd)
	d.Defaults, d.Running = src.Defaults, src.Running
	if linkParent {
		d.Parent = srcName
	}
	want := map[string]bool{}
	for _, id := range stepIDs {
		want[id] = true
	}
	for _, st := range src.Steps {
		if want[st.ID] {
			d.Steps = append(d.Steps, st)
		}
	}
	if len(d.Steps) == 0 {
		return Doc{}, errors.New("no steps selected")
	}
	if err := s.Create(newName, d); err != nil {
		return Doc{}, err
	}
	d.Name = leaf
	if keepOutputs {
		s.mu.Lock()
		from := s.readState(srcName)
		st := s.readState(newName)
		for _, step := range d.Steps {
			if o, ok := from.Outputs[step.ID]; ok {
				st.Outputs[step.ID] = o
			}
		}
		err := s.writeState(newName, st)
		s.mu.Unlock()
		if err != nil {
			return d, err
		}
	}
	return d, nil
}

// --- state: step ids + last output per step ---

// StepRef remembers which id a step had, to re-match after edits outside the app. The
// body is stored as a hash so the cache never holds a second copy of command text.
type StepRef struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	BodyHash string `json:"bodyHash"`
}

func bodyHash(st Step) string {
	body := st.Command + st.Note
	if body == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// Output is the retained last run of one step (SPEC.md §2 "Output"), already redacted.
type Output struct {
	Status     string    `json:"status"` // passed | failed
	Output     string    `json:"output"`
	ExitCode   int       `json:"exit"`
	DurationMs int64     `json:"ms"`
	StartedAt  time.Time `json:"startedAt"`
	Reason     string    `json:"reason"`
	Command    string    `json:"command"`
	Cwd        string    `json:"cwd"`
}

type state struct {
	Steps   []StepRef         `json:"steps"`
	Outputs map[string]Output `json:"outputs"`
	LastRun time.Time         `json:"lastRun"`
	// StepArgs are the last-used {{argument}} values, per step id.
	StepArgs map[string]map[string]string `json:"stepArgs,omitempty"`
}

func (s *Store) statePath(name string) string {
	return filepath.Join(s.dir(name), stateDir, stateFile)
}

func (s *Store) readState(name string) state {
	st := state{Outputs: map[string]Output{}}
	if b, err := os.ReadFile(s.statePath(name)); err == nil {
		json.Unmarshal(b, &st)
	}
	if st.Outputs == nil {
		st.Outputs = map[string]Output{}
	}
	return st
}

func (s *Store) writeState(name string, st state) error {
	if err := os.MkdirAll(filepath.Dir(s.statePath(name)), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(s.statePath(name), b)
}

// Outputs returns the retained last output per step id.
func (s *Store) Outputs(name string) map[string]Output {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readState(name).Outputs
}

// StepArgs returns the last-used argument values, per step id.
func (s *Store) StepArgs(name string) map[string]map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.readState(name).StepArgs
	if a == nil {
		a = map[string]map[string]string{}
	}
	return a
}

// SetStepArgs remembers one step's argument values (replacing its previous set).
func (s *Store) SetStepArgs(name, stepID string, args map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.readState(name)
	if st.StepArgs == nil {
		st.StepArgs = map[string]map[string]string{}
	}
	if len(args) == 0 {
		delete(st.StepArgs, stepID)
	} else {
		st.StepArgs[stepID] = args
	}
	return s.writeState(name, st)
}

// SetOutput records a step's last output; o == nil clears it (recordOutput: false).
func (s *Store) SetOutput(name, stepID string, o *Output) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.readState(name)
	if o == nil {
		delete(st.Outputs, stepID)
	} else {
		st.Outputs[stepID] = *o
		st.LastRun = o.StartedAt
	}
	return s.writeState(name, st)
}

func refs(d Doc) []StepRef {
	out := make([]StepRef, len(d.Steps))
	for i, st := range d.Steps {
		out[i] = StepRef{ID: st.ID, Title: st.Title, BodyHash: bodyHash(st)}
	}
	return out
}

// assignIDs gives each step the id it had before: first by exact title+body, then by
// title, then by body; anything left gets a new id.
func assignIDs(d *Doc, known []StepRef) {
	used := map[string]bool{}
	take := func(match func(StepRef) bool) string {
		for _, r := range known {
			if !used[r.ID] && match(r) {
				used[r.ID] = true
				return r.ID
			}
		}
		return ""
	}
	for pass := 0; pass < 3; pass++ {
		for i := range d.Steps {
			st := &d.Steps[i]
			if st.ID != "" {
				continue
			}
			body := bodyHash(*st)
			switch pass {
			case 0:
				st.ID = take(func(r StepRef) bool { return r.Title == st.Title && r.BodyHash == body })
			case 1:
				st.ID = take(func(r StepRef) bool { return r.Title == st.Title })
			case 2:
				st.ID = take(func(r StepRef) bool { return r.BodyHash == body && body != "" })
			}
		}
	}
	for i := range d.Steps {
		if d.Steps[i].ID == "" {
			d.Steps[i].ID = newID()
		}
	}
}

func newID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return "s" + hex.EncodeToString(b)
}

// --- audit log (runs.log) ---

// StepText is a step's title and command/note, for step.edit before/after.
type StepText struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// AuditEntry is one line of runs.log. Event names follow SPEC.md §1, plus
// confirm.skip / confirm.stop (gate decisions), step.delete, runbook.rename,
// runbook.path, shell.reset and version.save.
type AuditEntry struct {
	Ts             time.Time `json:"ts"`
	Event          string    `json:"event"`
	RunID          string    `json:"runId,omitempty"`
	StepID         string    `json:"stepId,omitempty"`
	Step           int       `json:"step,omitempty"` // display number at the time
	Title          string    `json:"title,omitempty"`
	Command        string    `json:"command,omitempty"`
	Cwd            string    `json:"cwd,omitempty"`
	Exit           *int      `json:"exit,omitempty"`
	Ms             int64     `json:"ms,omitempty"`
	Reason         string    `json:"reason,omitempty"`
	Outcome        string    `json:"outcome,omitempty"`
	Output         string    `json:"output,omitempty"`
	OutputRecorded *bool     `json:"outputRecorded,omitempty"`
	Before         *StepText `json:"before,omitempty"`
	After          *StepText `json:"after,omitempty"`
	Message        string    `json:"message,omitempty"`
}

// LogPath is where a runbook's runs.log lives.
func (s *Store) LogPath(name string) (string, error) {
	if err := ValidRef(name); err != nil {
		return "", err
	}
	return filepath.Join(s.dir(name), logFile), nil
}

// Append adds entries to runs.log.
func (s *Store) Append(name string, entries ...AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(filepath.Join(s.dir(name), logFile), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	var buf bytes.Buffer
	for _, e := range entries {
		if e.Ts.IsZero() {
			e.Ts = time.Now().UTC()
		}
		b, err := json.Marshal(e)
		if err != nil {
			return err
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	_, err = f.Write(buf.Bytes())
	return err
}

// ReadLog returns the last limit entries, oldest first, without their output (the
// sidebar doesn't need it and outputs can be large).
func (s *Store) ReadLog(name string, limit int) ([]AuditEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.Open(filepath.Join(s.dir(name), logFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []AuditEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for sc.Scan() {
		var e AuditEntry
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		e.Output = ""
		out = append(out, e)
		if len(out) > limit {
			out = out[1:]
		}
	}
	return out, sc.Err()
}

// Prune drops entries older than cutoff. Entries are chronological, so the file is
// rewritten from the first entry inside the window (temp file + rename).
func (s *Store) Prune(name string, cutoff time.Time) (removed int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.dir(name), logFile)
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	offset := 0
	for offset < len(b) {
		end := bytes.IndexByte(b[offset:], '\n')
		if end < 0 {
			end = len(b) - offset
		}
		var head struct {
			Ts time.Time `json:"ts"`
		}
		if json.Unmarshal(b[offset:offset+end], &head) == nil && !head.Ts.Before(cutoff) {
			break
		}
		offset += end + 1
		removed++
	}
	if removed == 0 {
		return 0, nil
	}
	if offset > len(b) {
		offset = len(b)
	}
	return removed, writeAtomic(path, b[offset:])
}

// --- helpers ---

func (s *Store) ensureGitignore(name string) {
	path := filepath.Join(s.dir(name), ".gitignore")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		os.WriteFile(path, []byte(gitignore), 0o644)
	}
}

// writeAtomic writes via a temp file in the same directory and renames it into place.
func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
