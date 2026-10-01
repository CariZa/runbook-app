// Package journal records a run to disk: audit lines in runs.log and each step's last
// output in the runbook's cache. Everything written is redacted first. No Wails dependency.
package journal

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"runbook/redact"
	"runbook/runbook"
	"runbook/runner"
)

// OutputCap bounds how much of one step run's output is stored; the tail is kept.
const OutputCap = 1 << 20

const trimmedNote = "… (earlier output trimmed)\n"

// Recorder turns runner events for one run into audit entries and cached outputs.
type Recorder struct {
	Store    *runbook.Store
	Name     string // runbook directory
	Doc      runbook.Doc
	Redactor *redact.Redactor
	Mode     string                   // "single" | "run all" | "run selected"
	OnEntry  func(runbook.AuditEntry) // called after each entry is written (output stripped)

	runID  string
	output map[string][]byte
	starts map[string]runner.Event
	errs   []error
}

// Handle consumes one runner event.
func (r *Recorder) Handle(e runner.Event) {
	if r.output == nil {
		r.output = map[string][]byte{}
		r.starts = map[string]runner.Event{}
	}
	switch e.Type {
	case runner.EventRunStart:
		r.runID = newRunID()
		r.log(runbook.AuditEntry{Ts: e.Ts, Event: "run.start", Message: r.Mode})

	case runner.EventStepStart:
		r.Redactor.LearnFromCommand(e.Chunk)
		r.starts[e.StepID] = e
		r.output[e.StepID] = nil
		r.log(r.stepEntry(e, "step.start", runbook.AuditEntry{
			Command: r.Redactor.Redact(e.Chunk),
			Cwd:     e.Cwd,
		}))

	case runner.EventStepOutput:
		buf := append(r.output[e.StepID], e.Chunk...)
		if len(buf) > OutputCap {
			buf = append([]byte(trimmedNote), buf[len(buf)-OutputCap:]...)
		}
		r.output[e.StepID] = buf

	case runner.EventStepFinish:
		r.finish(e)

	case runner.EventStepSkip:
		r.log(r.stepEntry(e, "step.skip", runbook.AuditEntry{Message: e.Message}))

	case runner.EventRunFinish:
		r.log(runbook.AuditEntry{Ts: e.Ts, Event: "run.finish", Outcome: string(e.Outcome), Message: e.Message})
	}
}

// Gate records the answer to the inline destructive-step gate.
func (r *Recorder) Gate(stepID string, decision runner.GateDecision) {
	event := map[runner.GateDecision]string{
		runner.GateApprove: "confirm.approve",
		runner.GateSkip:    "confirm.skip",
		runner.GateStop:    "confirm.stop",
	}[decision]
	if event == "" {
		return
	}
	r.log(r.stepEntry(runner.Event{StepID: stepID, Ts: time.Now().UTC()}, event, runbook.AuditEntry{Message: "gate"}))
}

// Errors returns any disk errors hit while recording.
func (r *Recorder) Errors() []error { return r.errs }

func (r *Recorder) finish(e runner.Event) {
	step, _, _ := r.Doc.StepByID(e.StepID)
	record := r.Doc.Resolve(step).RecordOutput
	start := r.starts[e.StepID]
	out := r.Redactor.Redact(string(r.output[e.StepID]))
	delete(r.output, e.StepID)

	status := "failed"
	if e.Reason == runner.ReasonExit && e.ExitCode == 0 {
		status = "passed"
	}
	var cached *runbook.Output
	if record {
		cached = &runbook.Output{
			Status:     status,
			Output:     out,
			ExitCode:   e.ExitCode,
			DurationMs: e.DurationMs,
			StartedAt:  start.Ts,
			Reason:     string(e.Reason),
			Command:    r.Redactor.Redact(start.Chunk),
			Cwd:        start.Cwd,
		}
	}
	if err := r.Store.SetOutput(r.Name, e.StepID, cached); err != nil {
		r.errs = append(r.errs, err)
	}

	exit := e.ExitCode
	entry := runbook.AuditEntry{Exit: &exit, Ms: e.DurationMs, Reason: string(e.Reason), Cwd: e.Cwd}
	if record {
		entry.Output = out
	} else {
		no := false
		entry.OutputRecorded = &no
	}
	r.log(r.stepEntry(e, "step.finish", entry))
}

func (r *Recorder) stepEntry(e runner.Event, event string, base runbook.AuditEntry) runbook.AuditEntry {
	base.Ts, base.Event, base.StepID = e.Ts, event, e.StepID
	if step, i, ok := r.Doc.StepByID(e.StepID); ok {
		base.Step, base.Title = i+1, step.Title
	}
	return base
}

func (r *Recorder) log(e runbook.AuditEntry) {
	if !r.Doc.Running.Audit {
		return
	}
	e.RunID = r.runID
	if e.Ts.IsZero() {
		e.Ts = time.Now().UTC()
	}
	if err := r.Store.Append(r.Name, e); err != nil {
		r.errs = append(r.errs, err)
		return
	}
	if r.OnEntry != nil {
		e.Output = ""
		r.OnEntry(e)
	}
}

func newRunID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return "r" + hex.EncodeToString(b)
}
