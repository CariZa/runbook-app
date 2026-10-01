package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"runbook/journal"
	"runbook/redact"
	"runbook/runbook"
	"runbook/runner"
)

// RunEventName is the Wails event carrying runner.Event values to the frontend.
const RunEventName = "run"

// ShellEventName carries ShellStatus whenever the shared shell changes.
const ShellEventName = "shell"

// outputFlushInterval batches streamed output so a chatty command can't flood the UI.
const outputFlushInterval = 50 * time.Millisecond

// App is the thin Wails binding layer over packages runner, runbook and journal.
type App struct {
	ctx   context.Context
	store *runbook.Store

	envOnce sync.Once
	env     []string
	red     *redact.Redactor
	envDone chan struct{}

	mu      sync.Mutex
	cancel  context.CancelFunc // non-nil while a run is active
	gate    chan runner.GateDecision
	running sync.WaitGroup

	// shared is the long-lived shell used by single-step runs (DECISIONS.md "Shell sharing").
	shared    *runner.Session
	sharedDir string

	openName  string               // runbook shown in the UI
	lastPrune map[string]time.Time // per runbook, for daily runs.log pruning
}

// ShellStatus describes the shared shell for the UI.
type ShellStatus struct {
	Alive   bool   `json:"alive"`
	Cwd     string `json:"cwd"`
	Started string `json:"started"` // RFC 3339, empty when not alive
}

func NewApp(store *runbook.Store) *App {
	return &App{store: store, envDone: make(chan struct{}), lastPrune: map[string]time.Time{}}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	go a.loadEnv()
}

func (a *App) loadEnv() {
	a.envOnce.Do(func() {
		a.env = runner.LoginEnv(10 * time.Second)
		a.red = redact.New(a.env)
		close(a.envDone)
	})
}

// redactor waits for the login environment, which seeds the redactor.
func (a *App) redactor() *redact.Redactor {
	a.loadEnv()
	<-a.envDone
	return a.red
}

// RunRequest is what the frontend sends to start a run.
type RunRequest struct {
	Runbook string      `json:"runbook"` // directory name, for runs.log and the output cache
	Doc     runbook.Doc `json:"doc"`
	StepIDs []string    `json:"stepIds"` // in runbook order
	Single  bool        `json:"single"`
	Mode    string      `json:"mode"` // "run all" | "run selected" | "single", for the audit log
	// StepArgs fills each step's {{name}} blanks, keyed by step id. Every blank in the steps
	// that will run must have a value.
	StepArgs map[string]map[string]string `json:"stepArgs"`
}

// resolveSteps applies runbook defaults (SPEC.md §1) to the requested steps and fills their
// {{argument}} blanks. It fails, before anything runs, if a blank in a step that will
// actually run has no value (steps Run all skips don't count).
func resolveSteps(d runbook.Doc, ids []string, stepArgs map[string]map[string]string, single bool) ([]runner.Step, error) {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []runner.Step
	var missing []string
	for i, st := range d.Steps {
		if !want[st.ID] {
			continue
		}
		r := d.Resolve(st)
		cmd := st.Command
		if st.Kind != "note" {
			var m []string
			cmd, m = runbook.Substitute(st.Command, stepArgs[st.ID])
			if len(m) > 0 && (single || !r.SkipInRunAll) {
				missing = append(missing, fmt.Sprintf("%s in step %d", strings.Join(m, ", "), i+1))
			}
		}
		out = append(out, runner.Step{
			ID: st.ID, Title: st.Title, Kind: st.Kind, Command: cmd,
			Destructive: r.Destructive, ContinueOnFail: r.ContinueOnFail, SkipInRunAll: r.SkipInRunAll,
			Cwd: r.Cwd, TimeoutSec: r.TimeoutSec,
		})
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("fill in %s first", strings.Join(missing, "; "))
	}
	return out, nil
}

// RunSteps starts a run in the background. Progress arrives as "run" events.
// Only one run can be active at a time.
func (a *App) RunSteps(req RunRequest) error {
	steps, err := resolveSteps(req.Doc, req.StepIDs, req.StepArgs, req.Single)
	if err != nil {
		return err
	}
	a.mu.Lock()
	if a.cancel != nil {
		a.mu.Unlock()
		return errors.New("a run is already in progress")
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.cancel = cancel
	a.gate = make(chan runner.GateDecision)
	gate := a.gate
	a.running.Add(1)
	a.mu.Unlock()
	emitShell := false

	go func() {
		defer a.running.Done()
		defer func() {
			a.mu.Lock()
			a.cancel = nil
			a.gate = nil
			a.mu.Unlock()
			cancel()
		}()
		red := a.redactor()
		a.pruneLog(req.Runbook, false)

		var sess *runner.Session
		if req.Single {
			sess = a.sharedSession(req.Doc.Cwd)
			emitShell = true
		}
		rec := &journal.Recorder{
			Store: a.store, Name: req.Runbook, Doc: req.Doc, Redactor: red, Mode: req.Mode,
		}
		emit, flush := a.batcher()
		runner.Run(ctx, steps, runner.RunOptions{
			Dir:                req.Doc.Cwd,
			Env:                a.env,
			Single:             req.Single,
			PauseAtDestructive: req.Doc.Running.PauseAtDestructive,
			Session:            sess,
		}, runner.Hooks{
			Event: func(e runner.Event) {
				rec.Handle(e)
				emit(e)
			},
			Gate: func(ctx context.Context, st runner.Step) runner.GateDecision {
				d := runner.GateStop
				select {
				case d = <-gate:
				case <-ctx.Done():
				}
				rec.Gate(st.ID, d)
				return d
			},
		})
		flush()
		for _, err := range rec.Errors() {
			runtime.LogErrorf(a.ctx, "journal %s: %v", req.Runbook, err)
		}
		if emitShell {
			a.emitShellStatus()
		}
	}()
	return nil
}

// sharedSession returns the shared shell, starting a fresh one if there is none, it died,
// or the runbook path changed. It returns nil if a shell can't be started; Run then reports
// the error itself.
func (a *App) sharedSession(dir string) *runner.Session {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.shared != nil && a.shared.Alive() && a.sharedDir == dir {
		return a.shared
	}
	if a.shared != nil {
		a.shared.Close()
		a.shared = nil
	}
	sess, err := runner.StartSession(runner.SessionOptions{Dir: dir, Env: a.env})
	if err != nil {
		return nil
	}
	a.shared, a.sharedDir = sess, dir
	return sess
}

// ResetShell closes the shared shell; the next single-step run starts a fresh one.
func (a *App) ResetShell() error {
	a.mu.Lock()
	if a.cancel != nil {
		a.mu.Unlock()
		return errors.New("stop the current run first")
	}
	sess := a.shared
	a.shared = nil
	a.mu.Unlock()
	if sess != nil {
		sess.Close()
	}
	a.emitShellStatus()
	return nil
}

// GetShellStatus reports the shared shell's state.
func (a *App) GetShellStatus() ShellStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.shared == nil || !a.shared.Alive() {
		return ShellStatus{}
	}
	return ShellStatus{Alive: true, Cwd: a.shared.Cwd(), Started: a.shared.Started().UTC().Format(time.RFC3339)}
}

func (a *App) emitShellStatus() {
	runtime.EventsEmit(a.ctx, ShellEventName, a.GetShellStatus())
}

// ChooseDirectory opens the native folder picker. It returns "" if cancelled.
func (a *App) ChooseDirectory(current string) (string, error) {
	start, _ := runner.ExpandHome(current)
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:                "Where should this runbook's commands run?",
		DefaultDirectory:     start,
		CanCreateDirectories: true,
	})
}

// shutdown stops any active run and the shared shell so nothing outlives the app.
func (a *App) shutdown(ctx context.Context) {
	a.StopRun()
	a.mu.Lock()
	shared := a.shared
	a.shared = nil
	a.mu.Unlock()
	done := make(chan struct{})
	go func() {
		a.running.Wait()
		if shared != nil {
			shared.Close()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(runner.DefaultKillGrace + time.Second):
	}
}

// StopRun ends the active run: SIGTERM, then SIGKILL after 3s. It reports whether a run
// was active, so the UI can reset itself if it's out of sync.
func (a *App) StopRun() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel == nil {
		return false
	}
	a.cancel()
	return true
}

// AnswerGate resolves a pending destructive-step gate: "approve", "skip" or "stop".
func (a *App) AnswerGate(decision string) error {
	a.mu.Lock()
	gate := a.gate
	a.mu.Unlock()
	if gate == nil {
		return errors.New("no run in progress")
	}
	select {
	case gate <- runner.GateDecision(decision):
		return nil
	case <-time.After(time.Second):
		return errors.New("no step is waiting at a gate")
	}
}

// batcher coalesces consecutive step.output events per step and flushes them every
// outputFlushInterval, or immediately before any other event so ordering is kept.
func (a *App) batcher() (emit func(runner.Event), flush func()) {
	var (
		mu      sync.Mutex
		pending *runner.Event
		buf     strings.Builder
		timer   *time.Timer
	)
	flushLocked := func() {
		if pending != nil {
			pending.Chunk = buf.String()
			runtime.EventsEmit(a.ctx, RunEventName, *pending)
			pending = nil
			buf.Reset()
		}
		if timer != nil {
			timer.Stop()
			timer = nil
		}
	}
	emit = func(e runner.Event) {
		mu.Lock()
		defer mu.Unlock()
		if e.Type == runner.EventStepOutput {
			if pending != nil && pending.StepID != e.StepID {
				flushLocked()
			}
			if pending == nil {
				p := e
				pending = &p
				timer = time.AfterFunc(outputFlushInterval, func() {
					mu.Lock()
					defer mu.Unlock()
					flushLocked()
				})
			}
			buf.WriteString(e.Chunk)
			return
		}
		flushLocked()
		runtime.EventsEmit(a.ctx, RunEventName, e)
	}
	flush = func() {
		mu.Lock()
		defer mu.Unlock()
		flushLocked()
	}
	return emit, flush
}
