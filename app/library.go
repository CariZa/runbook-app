package main

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"runbook/redact"
	"runbook/runbook"
)

// Opened is everything the UI needs to show a runbook.
type Opened struct {
	Doc      runbook.Doc                  `json:"doc"`
	Outputs  map[string]runbook.Output    `json:"outputs"`
	Versions []runbook.Version            `json:"versions"`
	StepArgs map[string]map[string]string `json:"stepArgs"` // last-used argument values per step
}

// Library is the sidebar's contents: every runbook (by ref) and every folder.
type Library struct {
	Runbooks []runbook.Summary `json:"runbooks"`
	Folders  []string          `json:"folders"`
}

// ListRunbooks returns the library. On the very first launch it writes the shell-demo
// runbook so there is something to try; after that an empty library stays empty.
func (a *App) ListRunbooks() (Library, error) {
	list, err := a.store.List()
	if err != nil {
		return Library{}, err
	}
	if a.store.FirstLaunch(len(list) == 0) {
		if err := a.store.Create("shell-demo", sampleDoc()); err != nil && err != runbook.ErrExists {
			return Library{}, err
		}
		if list, err = a.store.List(); err != nil {
			return Library{}, err
		}
	}
	folders, err := a.store.Folders()
	return Library{Runbooks: list, Folders: folders}, err
}

// CreateFolder makes an empty folder at ref ("name" or "parent/name").
func (a *App) CreateFolder(ref string) error {
	return a.store.CreateFolder(ref)
}

// MoveFolder moves a folder (with everything in it) into parent ("" = the top level).
// It returns the folder's new ref. Refused during a run.
func (a *App) MoveFolder(ref, parent string) (string, error) {
	_, name := runbook.SplitRef(ref)
	return a.relocateFolder(ref, runbook.JoinRef(parent, name))
}

// RenameFolder renames a folder in place, keeping it where it is.
func (a *App) RenameFolder(ref, newName string) (string, error) {
	parent, _ := runbook.SplitRef(ref)
	return a.relocateFolder(ref, runbook.JoinRef(parent, newName))
}

// relocateFolder moves ref to newRef and keeps the open runbook's ref current when it sits
// inside the folder that moved.
func (a *App) relocateFolder(ref, newRef string) (string, error) {
	if ref == newRef {
		return ref, nil
	}
	a.mu.Lock()
	running := a.cancel != nil
	a.mu.Unlock()
	if running {
		return ref, errors.New("stop the current run first")
	}
	if err := a.store.MoveFolder(ref, newRef); err != nil {
		return ref, err
	}
	a.mu.Lock()
	if strings.HasPrefix(a.openName, ref+"/") {
		a.openName = newRef + strings.TrimPrefix(a.openName, ref)
	}
	a.mu.Unlock()
	return newRef, nil
}

// DeleteFolder moves a folder and every runbook in it to the Trash. It refuses during a run.
func (a *App) DeleteFolder(name string) (string, error) {
	a.mu.Lock()
	running := a.cancel != nil
	openInside := strings.HasPrefix(a.openName, name+"/")
	if openInside && !running {
		a.openName = ""
	}
	a.mu.Unlock()
	if running {
		return "", errors.New("stop the current run first")
	}
	if openInside {
		a.ResetShell()
	}
	return a.store.TrashFolder(name)
}

// MoveRunbook moves a runbook into folder ("" = the top level). It reads the runbook from
// disk (the UI saves the open one first). Its versions, outputs, remembered arguments and
// runs.log move with it. It returns the new ref.
func (a *App) MoveRunbook(ref string, folder string) (string, error) {
	_, name := runbook.SplitRef(ref)
	to := runbook.JoinRef(folder, name)
	if to == ref {
		return ref, nil
	}
	doc, err := a.store.Load(ref)
	if err != nil {
		return ref, err
	}
	if err := a.moveRunbook(ref, doc, to); err != nil {
		return ref, err
	}
	if doc.Running.Audit {
		where := folder
		if where == "" {
			where = "top level"
		}
		a.appendAudit(to, runbook.AuditEntry{Event: "runbook.move", Message: ref + " → " + where})
	}
	return to, nil
}

// moveRunbook renames/moves on disk, refusing during a run and keeping openName current.
func (a *App) moveRunbook(from string, doc runbook.Doc, to string) error {
	a.mu.Lock()
	running := a.cancel != nil
	a.mu.Unlock()
	if running {
		return errors.New("stop the current run first")
	}
	if _, err := a.store.Rename(from, doc, to); err != nil {
		return err
	}
	a.mu.Lock()
	if a.openName == from {
		a.openName = to
	}
	a.mu.Unlock()
	return nil
}

// DeleteRunbook moves a runbook to the macOS Trash. It refuses while a run is active.
func (a *App) DeleteRunbook(name string) (string, error) {
	a.mu.Lock()
	running := a.cancel != nil
	wasOpen := a.openName == name
	if wasOpen && !running {
		a.openName = ""
	}
	a.mu.Unlock()
	if running {
		return "", errors.New("stop the current run first")
	}
	if wasOpen {
		a.ResetShell()
	}
	return a.store.Trash(name)
}

// OpenRunbook loads a runbook, prunes its audit log to the retention window, and resets
// the shared shell if this is a different runbook from the last one opened.
func (a *App) OpenRunbook(name string) (Opened, error) {
	doc, err := a.store.Load(name)
	if err != nil {
		return Opened{}, err
	}
	a.pruneLog(name, true)

	a.mu.Lock()
	switched := a.openName != name
	a.openName = name
	a.mu.Unlock()
	if switched {
		a.ResetShell()
	}

	versions, err := a.store.Versions(name)
	if err != nil {
		return Opened{}, err
	}
	return Opened{Doc: doc, Outputs: a.store.Outputs(name), Versions: versions, StepArgs: a.store.StepArgs(name)}, nil
}

// pruneLog drops runs.log entries older than runbook.Retention: always when forced
// (on open), otherwise at most once a day per runbook.
func (a *App) pruneLog(name string, force bool) {
	a.mu.Lock()
	last := a.lastPrune[name]
	if !force && time.Since(last) < 24*time.Hour {
		a.mu.Unlock()
		return
	}
	a.lastPrune[name] = time.Now()
	a.mu.Unlock()
	a.store.Prune(name, time.Now().Add(-runbook.Retention))
}

// SaveStepArgs remembers the argument values typed into a step, so they're there next time.
// Values of secret-sounding arguments (token, password, …) are never written to disk.
func (a *App) SaveStepArgs(name, stepID string, args map[string]string) error {
	keep := map[string]string{}
	for k, v := range args {
		if v != "" && !redact.SensitiveName(k) {
			keep[k] = v
		}
	}
	return a.store.SetStepArgs(name, stepID, keep)
}

// CreateRunbook makes an empty runbook that runs in ~.
func (a *App) CreateRunbook(name string) (runbook.Doc, error) {
	d := runbook.NewDoc(name, "~")
	if err := a.store.Create(name, d); err != nil {
		return runbook.Doc{}, err
	}
	return a.store.Load(name)
}

// SaveRunbook is the autosave: it writes runbook.md. It never creates a version.
func (a *App) SaveRunbook(name string, doc runbook.Doc) error {
	return a.store.Save(name, doc)
}

// RenameRunbook renames the runbook's directory and front-matter name.
// RenameRunbook renames a runbook within its folder. It returns the new ref.
func (a *App) RenameRunbook(old string, doc runbook.Doc, newName string) (string, error) {
	folder, _ := runbook.SplitRef(old)
	to := runbook.JoinRef(folder, newName)
	if err := a.moveRunbook(old, doc, to); err != nil {
		return old, err
	}
	if doc.Running.Audit {
		a.appendAudit(to, runbook.AuditEntry{Event: "runbook.rename", Message: old + " → " + to})
	}
	return to, nil
}

// VersionSaved is the result of SaveVersion.
type VersionSaved struct {
	Doc      runbook.Doc       `json:"doc"`
	Versions []runbook.Version `json:"versions"`
}

// SaveVersion snapshots the runbook to versions/vN.md (explicit only, never automatic).
func (a *App) SaveVersion(name string, doc runbook.Doc) (VersionSaved, error) {
	d, err := a.store.SaveVersion(name, doc)
	if err != nil {
		return VersionSaved{}, err
	}
	if d.Running.Audit {
		a.appendAudit(name, runbook.AuditEntry{Event: "version.save", Message: "v" + strconv.Itoa(d.Version)})
	}
	vs, err := a.store.Versions(name)
	return VersionSaved{Doc: d, Versions: vs}, err
}

// ForkRunbook is "Generate from selected" (2c): a new runbook from the chosen steps.
func (a *App) ForkRunbook(src string, doc runbook.Doc, stepIDs []string, newName string, keepOutputs, linkParent bool) (runbook.Doc, error) {
	d, err := a.store.Fork(src, doc, stepIDs, newName, keepOutputs, linkParent)
	if err != nil {
		return runbook.Doc{}, err
	}
	if doc.Running.Audit {
		a.appendAudit(src, runbook.AuditEntry{Event: "runbook.fork", Message: "→ " + newName})
		a.appendAudit(newName, runbook.AuditEntry{Event: "runbook.fork", Message: "from " + src})
	}
	return d, nil
}

// LogEvent records a UI-originated audit entry (edits, deletes, confirms, path changes).
// Commands and step bodies are redacted before they are written.
func (a *App) LogEvent(name string, e runbook.AuditEntry) error {
	r := a.redactor()
	e.Command = r.Redact(e.Command)
	for _, t := range []*runbook.StepText{e.Before, e.After} {
		if t != nil {
			r.LearnFromCommand(t.Body)
			t.Body = r.Redact(t.Body)
		}
	}
	e.Output = ""
	return a.appendAudit(name, e)
}

func (a *App) appendAudit(name string, e runbook.AuditEntry) error {
	if e.Ts.IsZero() {
		e.Ts = time.Now().UTC()
	}
	return a.store.Append(name, e)
}

// OpenAuditLog opens the runbook's runs.log in the default text editor, or reveals it in
// the Finder. It fails with a readable message when nothing has been recorded yet.
func (a *App) OpenAuditLog(name string, reveal bool) error {
	path, err := a.store.LogPath(name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return errors.New("nothing recorded yet: runs.log appears after the first run or edit")
	}
	flag := "-t" // default text editor
	if reveal {
		flag = "-R" // select it in the Finder
	}
	return exec.Command("open", flag, path).Run()
}
