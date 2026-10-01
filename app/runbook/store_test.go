package runbook

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func newStore(t *testing.T) *Store {
	return &Store{Root: filepath.Join(t.TempDir(), "runbooks")}
}

func writeDoc(t *testing.T, s *Store, name, content string) {
	t.Helper()
	if err := os.MkdirAll(s.dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.dir(name), docFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadSaveUnchangedIsByteIdenticalOnDisk(t *testing.T) {
	s := newStore(t)
	writeDoc(t, s, "db-latency-triage", specExample)
	d, err := s.Load("db-latency-triage")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save("db-latency-triage", d); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(s.dir("db-latency-triage"), docFile))
	if string(got) != specExample {
		t.Errorf("file changed on save:\n%s", got)
	}
	if gi, _ := os.ReadFile(filepath.Join(s.dir("db-latency-triage"), ".gitignore")); string(gi) != gitignore {
		t.Errorf(".gitignore = %q", gi)
	}
}

func TestStepIDsAreStableAndSurviveOutsideEdits(t *testing.T) {
	s := newStore(t)
	writeDoc(t, s, "rb", specExample)
	first, _ := s.Load("rb")
	again, _ := s.Load("rb")
	for i := range first.Steps {
		if first.Steps[i].ID == "" || first.Steps[i].ID != again.Steps[i].ID {
			t.Fatalf("ids not stable: %s vs %s", first.Steps[i].ID, again.Steps[i].ID)
		}
	}

	// Someone reorders the file and edits a command in their editor.
	edited := strings.Replace(specExample, "grep -v Running", "grep -v Completed", 1)
	d := mustParse(t, edited)
	d.Steps[0], d.Steps[2] = d.Steps[2], d.Steps[0]
	writeDoc(t, s, "rb", string(Serialize(d)))
	after, _ := s.Load("rb")

	byTitle := map[string]string{}
	for _, st := range first.Steps {
		byTitle[st.Title] = st.ID
	}
	for _, st := range after.Steps {
		if st.ID != byTitle[st.Title] {
			t.Errorf("%q: id %s, want %s", st.Title, st.ID, byTitle[st.Title])
		}
	}
}

func TestCreateListRename(t *testing.T) {
	s := newStore(t)
	if list, err := s.List(); err != nil || len(list) != 0 {
		t.Fatalf("empty root: %v %v", list, err)
	}
	d := NewDoc("alpha", "~")
	d.Steps = []Step{{Title: "a", Kind: "command", Command: "true"}}
	if err := s.Create(d.Name, d); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(d.Name, d); !errors.Is(err, ErrExists) {
		t.Errorf("duplicate create: %v", err)
	}
	if err := s.Create("../escape", NewDoc("x", "~")); err == nil {
		t.Error("bad name accepted")
	}

	list, _ := s.List()
	if len(list) != 1 || list[0].Name != "alpha" || list[0].Steps != 1 {
		t.Errorf("list: %+v", list)
	}

	loaded, _ := s.Load("alpha")
	renamed, err := s.Rename("alpha", loaded, "beta")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Name != "beta" {
		t.Error("name not updated")
	}
	back, err := s.Load("beta")
	if err != nil || back.Name != "beta" || back.Steps[0].ID != loaded.Steps[0].ID {
		t.Errorf("after rename: %+v %v", back, err)
	}
	if _, err := os.Stat(s.dir("alpha")); !errors.Is(err, os.ErrNotExist) {
		t.Error("old dir still there")
	}
}

func TestVersionsAreExplicit(t *testing.T) {
	s := newStore(t)
	d := NewDoc("v", "~")
	d.Steps = []Step{{Title: "a", Kind: "command", Command: "echo 1"}}
	s.Create(d.Name, d)
	d, _ = s.Load("v")

	for i := 0; i < 3; i++ { // autosaves
		d.Steps[0].Command = fmt.Sprintf("echo %d", i)
		s.Save("v", d)
	}
	if vs, _ := s.Versions("v"); len(vs) != 0 {
		t.Fatalf("autosave created versions: %+v", vs)
	}

	d, err := s.SaveVersion("v", d)
	if err != nil || d.Version != 1 {
		t.Fatalf("v1: %v %d", err, d.Version)
	}
	d.Steps[0].Command = "echo changed"
	s.Save("v", d)
	d, _ = s.SaveVersion("v", d)

	vs, _ := s.Versions("v")
	if len(vs) != 2 || vs[0].N != 2 || vs[1].N != 1 {
		t.Fatalf("versions: %+v", vs)
	}
	v1, _ := os.ReadFile(filepath.Join(s.dir("v"), versionDir, "v1.md"))
	if !strings.Contains(string(v1), "echo 2") || !strings.Contains(string(v1), "version: 1") {
		t.Errorf("v1 snapshot:\n%s", v1)
	}
	cur, _ := os.ReadFile(filepath.Join(s.dir("v"), docFile))
	if !strings.Contains(string(cur), "version: 2") {
		t.Errorf("runbook.md not stamped:\n%s", cur)
	}
}

func TestForkCarriesSelectedStepsOutputsAndParent(t *testing.T) {
	s := newStore(t)
	writeDoc(t, s, "src", specExample)
	src, _ := s.Load("src")
	s.SetOutput("src", src.Steps[0].ID, &Output{Status: "passed", Output: "pod list\n", StartedAt: time.Now()})

	fork, err := s.Fork("src", src, []string{src.Steps[0].ID, src.Steps[2].ID}, "src-clean", true, true)
	if err != nil {
		t.Fatal(err)
	}
	if fork.Parent != "src" || len(fork.Steps) != 2 || fork.Steps[1].Kind != "note" {
		t.Errorf("fork: %+v", fork)
	}
	if o := s.Outputs("src-clean"); o[src.Steps[0].ID].Output != "pod list\n" {
		t.Errorf("outputs not carried: %+v", o)
	}
	if _, err := s.Fork("src", src, nil, "empty-fork", false, false); err == nil {
		t.Error("fork with no steps accepted")
	}
}

func TestOutputsAreDroppedWithTheirStep(t *testing.T) {
	s := newStore(t)
	writeDoc(t, s, "o", specExample)
	d, _ := s.Load("o")
	s.SetOutput("o", d.Steps[0].ID, &Output{Output: "x"})
	s.SetOutput("o", d.Steps[1].ID, &Output{Output: "y"})
	d.Steps = d.Steps[1:]
	s.Save("o", d)
	o := s.Outputs("o")
	if _, ok := o[d.Steps[0].ID]; !ok || len(o) != 1 {
		t.Errorf("outputs: %+v", o)
	}
	s.SetOutput("o", d.Steps[0].ID, nil)
	if len(s.Outputs("o")) != 0 {
		t.Error("clear failed")
	}
}

func TestAuditAppendReadPrune(t *testing.T) {
	s := newStore(t)
	s.Create("a", NewDoc("a", "~"))
	now := time.Now().UTC()
	zero := 0
	var entries []AuditEntry
	for _, days := range []int{45, 31, 20, 10, 0} {
		entries = append(entries, AuditEntry{Ts: now.Add(-time.Duration(days) * 24 * time.Hour), Event: "step.finish", Step: days, Exit: &zero, Output: "big output"})
	}
	if err := s.Append("a", entries...); err != nil {
		t.Fatal(err)
	}

	got, _ := s.ReadLog("a", 3)
	if len(got) != 3 || got[0].Step != 20 || got[2].Step != 0 || got[2].Output != "" {
		t.Errorf("read: %+v", got)
	}

	removed, err := s.Prune("a", now.Add(-Retention))
	if err != nil || removed != 2 {
		t.Fatalf("prune removed %d, %v", removed, err)
	}
	got, _ = s.ReadLog("a", 100)
	if len(got) != 3 || got[0].Step != 20 {
		t.Errorf("after prune: %+v", got)
	}
	if removed, _ := s.Prune("a", now.Add(-Retention)); removed != 0 {
		t.Error("second prune removed more")
	}
	raw, _ := os.ReadFile(filepath.Join(s.dir("a"), logFile))
	if !strings.Contains(string(raw), `"output":"big output"`) {
		t.Error("output not kept on disk")
	}
	if fi, _ := os.Stat(filepath.Join(s.dir("a"), logFile)); fi.Mode().Perm() != 0o600 {
		t.Errorf("runs.log mode %v", fi.Mode().Perm())
	}
}

func TestStateHoldsNoCommandText(t *testing.T) {
	s := newStore(t)
	d := NewDoc("sec", "~")
	d.Steps = []Step{{Title: "t", Kind: "command", Command: "export API_TOKEN=do-not-copy-me"}}
	s.Create(d.Name, d)
	s.Load("sec")
	b, _ := os.ReadFile(s.statePath("sec"))
	if strings.Contains(string(b), "do-not-copy-me") {
		t.Errorf("state.json holds command text:\n%s", b)
	}
}

func TestTrashMovesTheWholeRunbook(t *testing.T) {
	s := newStore(t)
	s.TrashDir = t.TempDir()
	for i := 0; i < 2; i++ {
		d := NewDoc("gone", "~")
		d.Steps = []Step{{Title: "a", Kind: "command", Command: "true"}}
		if err := s.Create(d.Name, d); err != nil {
			t.Fatal(err)
		}
		s.Append("gone", AuditEntry{Event: "run.start"})
		path, err := s.Trash("gone")
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(s.TrashDir, "gone")
		if i == 1 {
			want += " 2" // a same-named runbook trashed earlier is kept
		}
		if path != want {
			t.Errorf("trashed to %s, want %s", path, want)
		}
		for _, f := range []string{docFile, logFile, ".gitignore"} {
			if _, err := os.Stat(filepath.Join(path, f)); err != nil {
				t.Errorf("%s missing from trash: %v", f, err)
			}
		}
		if _, err := os.Stat(s.dir("gone")); !errors.Is(err, os.ErrNotExist) {
			t.Error("still in the library")
		}
	}
	if _, err := s.Trash("never-existed"); err == nil {
		t.Error("trashing a missing runbook succeeded")
	}
	if _, err := s.Trash("../escape"); err == nil {
		t.Error("bad name accepted")
	}
}

func TestFirstLaunchOnlyOnce(t *testing.T) {
	s := newStore(t)
	if !s.FirstLaunch(true) {
		t.Error("empty new library should be a first launch")
	}
	if s.FirstLaunch(true) {
		t.Error("second call should not be a first launch")
	}
	s2 := newStore(t)
	if s2.FirstLaunch(false) || s2.FirstLaunch(true) {
		t.Error("a library that already had runbooks is never a first launch")
	}
}

func TestStepArgsRememberedAndDroppedWithTheirStep(t *testing.T) {
	s := newStore(t)
	writeDoc(t, s, "a", specExample)
	d, _ := s.Load("a")
	s.SetStepArgs("a", d.Steps[0].ID, map[string]string{"disk": "pvc-1"})
	s.SetStepArgs("a", d.Steps[1].ID, map[string]string{"disk": "pvc-2"})
	if got := s.StepArgs("a"); got[d.Steps[0].ID]["disk"] != "pvc-1" || got[d.Steps[1].ID]["disk"] != "pvc-2" {
		t.Errorf("args %+v", got)
	}
	d.Steps = d.Steps[1:]
	s.Save("a", d)
	if got := s.StepArgs("a"); len(got) != 1 {
		t.Errorf("deleted step's args kept: %+v", got)
	}
}

func TestFolders(t *testing.T) {
	s := newStore(t)
	s.TrashDir = t.TempDir()
	mk := func(ref string) {
		t.Helper()
		d := NewDoc("ignored", "~")
		d.Steps = []Step{{Title: "a", Kind: "command", Command: "true"}}
		if err := s.Create(ref, d); err != nil {
			t.Fatalf("create %s: %v", ref, err)
		}
	}

	if err := s.CreateFolder("disk-cleanup"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateFolder("disk-cleanup"); !errors.Is(err, ErrExists) {
		t.Errorf("duplicate folder: %v", err)
	}
	s.CreateFolder("empty")
	mk("disk-cleanup/1-find")
	mk("disk-cleanup/2-delete")
	mk("loose")
	if err := s.Create("missing-folder/x", NewDoc("x", "~")); err == nil {
		t.Error("created a runbook in a folder that doesn't exist")
	}
	if err := s.Create("loose/inside-a-runbook", NewDoc("x", "~")); err == nil {
		t.Error("a runbook was treated as a folder")
	}
	for _, bad := range []string{"a/b/c/d/e/f", "../x", "a/../b", "/abs", "a/"} {
		if err := ValidRef(bad); err == nil {
			t.Errorf("ValidRef(%q) accepted", bad)
		}
	}

	folders, _ := s.Folders()
	if !reflect.DeepEqual(folders, []string{"disk-cleanup", "empty"}) {
		t.Errorf("folders %q", folders)
	}
	list, _ := s.List()
	got := map[string]string{}
	for _, r := range list {
		got[r.Name] = r.Folder
	}
	want := map[string]string{"disk-cleanup/1-find": "disk-cleanup", "disk-cleanup/2-delete": "disk-cleanup", "loose": ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("list %v", got)
	}

	d, err := s.Load("disk-cleanup/2-delete")
	if err != nil || d.Name != "2-delete" {
		t.Fatalf("load: %+v %v", d.Name, err)
	}
	s.Append("disk-cleanup/2-delete", AuditEntry{Event: "run.start"})
	s.SetStepArgs("disk-cleanup/2-delete", d.Steps[0].ID, map[string]string{"disk": "pvc-1"})

	// Move out to the top level, then into another folder; everything travels along.
	if _, err := s.Rename("disk-cleanup/2-delete", d, "loose"); !errors.Is(err, ErrExists) {
		t.Errorf("move onto a taken name: %v", err)
	}
	moved, err := s.Rename("disk-cleanup/2-delete", d, "2-delete")
	if err != nil || moved.Name != "2-delete" {
		t.Fatalf("move to top: %v", err)
	}
	if _, err := s.Rename("2-delete", moved, "empty/2-delete"); err != nil {
		t.Fatal(err)
	}
	if s.StepArgs("empty/2-delete")[d.Steps[0].ID]["disk"] != "pvc-1" {
		t.Error("remembered args didn't move with the runbook")
	}
	if p, _ := s.LogPath("empty/2-delete"); !fileExists(p) {
		t.Error("runs.log didn't move with the runbook")
	}

	// Deleting a folder moves it, with every runbook inside, to the Trash.
	path, err := s.TrashFolder("disk-cleanup")
	if err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(path, "1-find", docFile)) {
		t.Error("folder contents not in the Trash")
	}
	if _, err := s.TrashFolder("loose"); err == nil {
		t.Error("TrashFolder deleted a runbook")
	}
	if _, err := s.Trash("empty"); err == nil {
		t.Error("Trash deleted a folder")
	}
	folders, _ = s.Folders()
	if !reflect.DeepEqual(folders, []string{"empty"}) {
		t.Errorf("folders after delete %q", folders)
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// mkRunbook creates a runbook at ref with one step.
func mkRunbook(t *testing.T, s *Store, ref string) {
	t.Helper()
	d := NewDoc("ignored", "~")
	d.Steps = []Step{{Title: "a", Kind: "command", Command: "true"}}
	if err := s.Create(ref, d); err != nil {
		t.Fatalf("create %s: %v", ref, err)
	}
}

func TestNestedFolders(t *testing.T) {
	s := newStore(t)
	s.TrashDir = t.TempDir()
	for _, f := range []string{"gcp", "gcp/disks", "gcp/disks/archive", "oncall"} {
		if err := s.CreateFolder(f); err != nil {
			t.Fatalf("create folder %s: %v", f, err)
		}
	}
	mkRunbook(t, s, "gcp/disks/1-find")
	mkRunbook(t, s, "gcp/disks/archive/old-one")
	mkRunbook(t, s, "loose")

	folders, _ := s.Folders()
	if !reflect.DeepEqual(folders, []string{"gcp", "gcp/disks", "gcp/disks/archive", "oncall"}) {
		t.Errorf("folders %q", folders)
	}
	list, _ := s.List()
	got := map[string]string{}
	for _, r := range list {
		got[r.Name] = r.Folder
	}
	want := map[string]string{"gcp/disks/1-find": "gcp/disks", "gcp/disks/archive/old-one": "gcp/disks/archive", "loose": ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("list %v", got)
	}

	// A runbook is never descended into, even if it holds directories (versions/).
	d, _ := s.Load("gcp/disks/1-find")
	if _, err := s.SaveVersion("gcp/disks/1-find", d); err != nil {
		t.Fatal(err)
	}
	if f, _ := s.Folders(); len(f) != 4 {
		t.Errorf("folders after SaveVersion: %q", f)
	}

	// Depth limit: MaxDepth segments in a ref, so MaxDepth-1 folders.
	if err := s.CreateFolder("gcp/disks/archive/deep"); err != nil {
		t.Fatalf("4 folders deep should be allowed: %v", err)
	}
	if err := s.CreateFolder("gcp/disks/archive/deep/deeper"); err == nil {
		t.Error("5 folders deep should be refused")
	}
	if err := s.Create("gcp/disks/archive/deep/rb", NewDoc("rb", "~")); err != nil {
		t.Errorf("runbook at max depth: %v", err)
	}
}

func TestMoveFolder(t *testing.T) {
	s := newStore(t)
	for _, f := range []string{"gcp", "gcp/disks", "oncall"} {
		s.CreateFolder(f)
	}
	mkRunbook(t, s, "gcp/disks/1-find")
	s.SetStepArgs("gcp/disks/1-find", "sx", map[string]string{"disk": "pvc-1"})
	s.Append("gcp/disks/1-find", AuditEntry{Event: "run.start"})

	// Into itself or a descendant: refused, and nothing moves.
	for _, bad := range []string{"gcp", "gcp/disks", "gcp/disks/deeper"} {
		if err := s.MoveFolder("gcp", bad); err == nil {
			t.Errorf("moving gcp into %q was allowed", bad)
		}
	}
	if !s.isFolder("gcp/disks") || !s.isRunbook("gcp/disks/1-find") {
		t.Fatal("a refused move changed the library")
	}

	// Onto an existing name, or into a runbook: refused.
	if err := s.MoveFolder("oncall", "gcp"); !errors.Is(err, ErrExists) {
		t.Errorf("onto an existing folder: %v", err)
	}
	if err := s.MoveFolder("oncall", "gcp/disks/1-find/inside"); err == nil {
		t.Error("into a runbook was allowed")
	}

	// A real move carries everything inside it.
	if err := s.MoveFolder("gcp/disks", "oncall/disks"); err != nil {
		t.Fatal(err)
	}
	if s.isFolder("gcp/disks") {
		t.Error("old folder still there")
	}
	if !s.isRunbook("oncall/disks/1-find") {
		t.Fatal("runbook didn't travel")
	}
	if s.StepArgs("oncall/disks/1-find")["sx"]["disk"] != "pvc-1" {
		t.Error("remembered arguments didn't travel")
	}
	if p, _ := s.LogPath("oncall/disks/1-find"); !fileExists(p) {
		t.Error("runs.log didn't travel")
	}

	// Rename in place is the same operation.
	if err := s.MoveFolder("oncall/disks", "oncall/gcp-disks"); err != nil {
		t.Fatal(err)
	}
	if !s.isRunbook("oncall/gcp-disks/1-find") {
		t.Error("rename lost the runbook")
	}

	// A move that would push contents past the depth limit is refused.
	s.CreateFolder("a")
	s.CreateFolder("a/b")
	s.CreateFolder("a/b/c")
	if err := s.MoveFolder("oncall", "a/b/c"); err == nil {
		t.Error("move past the depth limit was allowed")
	}
	if !s.isRunbook("oncall/gcp-disks/1-find") {
		t.Error("a refused deep move moved things anyway")
	}
}
