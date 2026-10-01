package runner

import (
	"context"
	"time"
)

// EndReason says why a step or run ended.
type EndReason string

const (
	ReasonExit        EndReason = "exit"         // the command finished; see the exit code
	ReasonTimeout     EndReason = "timeout"      // killed after timeoutSec
	ReasonStopped     EndReason = "stopped"      // the user pressed Stop
	ReasonShellExited EndReason = "shell-exited" // the step ended the shell (e.g. `exit 3`)
	ReasonError       EndReason = "error"        // runbook itself failed (bad cwd, I/O)
)

// Step is a runbook step with defaults already resolved (SPEC.md §1).
type Step struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Kind           string `json:"kind"` // "command" | "note"
	Command        string `json:"command"`
	Destructive    bool   `json:"destructive"`
	ContinueOnFail bool   `json:"continueOnFail"`
	SkipInRunAll   bool   `json:"skipInRunAll"`
	Cwd            string `json:"cwd"`        // "" = inherit
	TimeoutSec     int    `json:"timeoutSec"` // 0 = no timeout
}

// RunOptions configures one run.
type RunOptions struct {
	Dir                string // runbook cwd
	Env                []string
	KillGrace          time.Duration
	Single             bool // one step run from its own ▶: no skipInRunAll, no gate
	PauseAtDestructive bool
	// Session, when set, is used instead of a fresh shell and left open afterwards (unless
	// the run killed it). This is how single-step runs share one long-lived shell.
	Session *Session
}

// GateDecision is the answer to the inline destructive gate (screen 3b, right card).
type GateDecision string

const (
	GateApprove GateDecision = "approve"
	GateSkip    GateDecision = "skip"
	GateStop    GateDecision = "stop"
)

// EventType names match runs.log events (SPEC.md §1), plus step.output for streaming.
type EventType string

const (
	EventRunStart   EventType = "run.start"
	EventRunFinish  EventType = "run.finish"
	EventStepStart  EventType = "step.start"
	EventStepOutput EventType = "step.output"
	EventStepFinish EventType = "step.finish"
	EventStepSkip   EventType = "step.skip"
	EventGate       EventType = "step.gate" // waiting on the user before a destructive step
)

// Event is what the run reports to its caller.
type Event struct {
	Type       EventType `json:"type"`
	Ts         time.Time `json:"ts"`
	StepID     string    `json:"stepId,omitempty"`
	Chunk      string    `json:"chunk,omitempty"`
	ExitCode   int       `json:"exit"`
	DurationMs int64     `json:"ms,omitempty"`
	Reason     EndReason `json:"reason,omitempty"`
	Cwd        string    `json:"cwd,omitempty"`     // step.start: where it runs; step.finish: shell cwd after
	Outcome    Outcome   `json:"outcome,omitempty"` // run.finish only
	Message    string    `json:"message,omitempty"`
}

// Outcome is how a whole run ended.
type Outcome string

const (
	OutcomeCompleted Outcome = "completed"
	OutcomeFailed    Outcome = "failed"
	OutcomeStopped   Outcome = "stopped"
	OutcomeError     Outcome = "error"
)

// Hooks connect a run to the UI.
type Hooks struct {
	Event func(Event)
	// Gate blocks until the user decides. nil means destructive steps run ungated.
	Gate func(ctx context.Context, s Step) GateDecision
}

// Run executes steps top to bottom in one shell session (SPEC.md §2):
//   - notes are passed over silently;
//   - skipInRunAll steps are skipped (logged) unless Single;
//   - a failing step ends the run unless continueOnFail;
//   - with PauseAtDestructive, destructive steps wait on Hooks.Gate first;
//   - cancelling ctx is Stop.
func Run(ctx context.Context, steps []Step, opts RunOptions, h Hooks) Outcome {
	emit := func(e Event) {
		e.Ts = time.Now().UTC()
		if h.Event != nil {
			h.Event(e)
		}
	}
	finish := func(o Outcome, msg string) Outcome {
		emit(Event{Type: EventRunFinish, Outcome: o, Message: msg})
		return o
	}

	emit(Event{Type: EventRunStart})
	sess := opts.Session
	if sess == nil {
		var err error
		sess, err = StartSession(SessionOptions{Dir: opts.Dir, Env: opts.Env, KillGrace: opts.KillGrace})
		if err != nil {
			return finish(OutcomeError, err.Error())
		}
		defer sess.Close()
	}

	for _, st := range steps {
		if st.Kind == "note" {
			continue
		}
		if ctx.Err() != nil {
			return finish(OutcomeStopped, "")
		}
		if !opts.Single && st.SkipInRunAll {
			emit(Event{Type: EventStepSkip, StepID: st.ID, Message: "skipInRunAll"})
			continue
		}
		if !opts.Single && st.Destructive && opts.PauseAtDestructive && h.Gate != nil {
			emit(Event{Type: EventGate, StepID: st.ID})
			switch h.Gate(ctx, st) {
			case GateSkip:
				emit(Event{Type: EventStepSkip, StepID: st.ID, Message: "gate"})
				continue
			case GateStop:
				return finish(OutcomeStopped, "")
			}
			if ctx.Err() != nil {
				return finish(OutcomeStopped, "")
			}
		}

		startCwd := sess.Cwd()
		if st.Cwd != "" {
			startCwd = st.Cwd
		}
		emit(Event{Type: EventStepStart, StepID: st.ID, Cwd: startCwd, Chunk: st.Command})
		res := sess.Exec(ctx, ExecRequest{
			Command:        st.Command,
			Cwd:            st.Cwd,
			Timeout:        time.Duration(st.TimeoutSec) * time.Second,
			ContinueOnFail: st.ContinueOnFail,
		}, func(b []byte) {
			emit(Event{Type: EventStepOutput, StepID: st.ID, Chunk: string(b)})
		})
		emit(Event{
			Type:       EventStepFinish,
			StepID:     st.ID,
			ExitCode:   res.ExitCode,
			DurationMs: res.Duration.Milliseconds(),
			Reason:     res.Reason,
			Cwd:        res.Cwd,
		})

		switch {
		case res.Reason == ReasonStopped:
			return finish(OutcomeStopped, "")
		case res.Reason == ReasonError:
			return finish(OutcomeError, "")
		case res.SessionDead:
			return finish(OutcomeFailed, "shell session ended")
		case res.Reason == ReasonExit && res.ExitCode == 0:
			// passed
		case !st.ContinueOnFail:
			return finish(OutcomeFailed, "")
		}
	}
	return finish(OutcomeCompleted, "")
}
