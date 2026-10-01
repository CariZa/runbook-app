package journal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"runbook/redact"
	"runbook/runbook"
	"runbook/runner"
)

func setup(t *testing.T, audit bool) (*runbook.Store, runbook.Doc) {
	t.Helper()
	s := &runbook.Store{Root: filepath.Join(t.TempDir(), "runbooks")}
	no := false
	d := runbook.NewDoc("rb", t.TempDir())
	d.Running.Audit = audit
	d.Steps = []runbook.Step{
		{Title: "Export a token", Kind: "command", Command: `export API_TOKEN=tok-abcdef123456; echo "using $API_TOKEN"`},
		{Title: "Read the vault", Kind: "command", Command: "echo $((40+2))-vault-output", RecordOutput: &no},
		{Title: "Fail", Kind: "command", Command: "echo nope; false"},
	}
	if err := s.Create(d.Name, d); err != nil {
		t.Fatal(err)
	}
	d, err := s.Load("rb")
	if err != nil {
		t.Fatal(err)
	}
	return s, d
}

// runReal drives the real runner so the recorder sees genuine event sequences.
func runReal(t *testing.T, s *runbook.Store, d runbook.Doc, env []string) (*Recorder, []runbook.AuditEntry) {
	t.Helper()
	var live []runbook.AuditEntry
	rec := &Recorder{Store: s, Name: "rb", Doc: d, Redactor: redact.New(env), Mode: "run all",
		OnEntry: func(e runbook.AuditEntry) { live = append(live, e) }}
	var steps []runner.Step
	for _, st := range d.Steps {
		steps = append(steps, runner.Step{ID: st.ID, Title: st.Title, Kind: st.Kind, Command: st.Command, TimeoutSec: 10})
	}
	runner.Run(context.Background(), steps, runner.RunOptions{Dir: d.Cwd}, runner.Hooks{Event: rec.Handle})
	if errs := rec.Errors(); len(errs) > 0 {
		t.Fatal(errs)
	}
	return rec, live
}

func TestRunIsJournalledWithCommandsOutputsAndRedaction(t *testing.T) {
	s, d := setup(t, true)
	_, live := runReal(t, s, d, []string{"GITHUB_TOKEN=ghtoken-from-login-env"})

	raw, err := os.ReadFile(filepath.Join(s.Root, "rb", "runs.log"))
	if err != nil {
		t.Fatal(err)
	}
	log := string(raw)
	for _, want := range []string{`"event":"run.start"`, `"event":"step.start"`, `"event":"step.finish"`, `"event":"run.finish"`,
		`"outcome":"failed"`, `"title":"Fail"`, `"output":"nope\n"`, `"outputRecorded":false`, `"message":"run all"`} {
		if !strings.Contains(log, want) {
			t.Errorf("runs.log missing %s\n%s", want, log)
		}
	}
	if strings.Contains(log, "tok-abcdef123456") {
		t.Errorf("exported token leaked into runs.log:\n%s", log)
	}
	if !strings.Contains(log, `export API_TOKEN=[REDACTED]`) || !strings.Contains(log, `using [REDACTED]`) {
		t.Errorf("command/output not redacted as expected:\n%s", log)
	}
	if strings.Contains(log, "42-vault-output") {
		t.Error("recordOutput:false step's output was logged")
	}

	outs := s.Outputs("rb")
	if o := outs[d.Steps[0].ID]; o.Status != "passed" || o.Output != "using [REDACTED]\n" || !strings.Contains(o.Command, "[REDACTED]") {
		t.Errorf("cached output 1: %+v", o)
	}
	if _, ok := outs[d.Steps[1].ID]; ok {
		t.Error("recordOutput:false step's output was cached")
	}
	if o := outs[d.Steps[2].ID]; o.Status != "failed" || o.ExitCode != 1 {
		t.Errorf("cached output 3: %+v", o)
	}

	if len(live) == 0 || live[0].Event != "run.start" {
		t.Fatalf("live entries: %+v", live)
	}
	for _, e := range live {
		if e.Output != "" {
			t.Error("live entries should not carry output")
		}
		if e.RunID == "" {
			t.Error("missing run id")
		}
	}
}

func TestAuditOffStillCachesOutput(t *testing.T) {
	s, d := setup(t, false)
	runReal(t, s, d, nil)
	if _, err := os.Stat(filepath.Join(s.Root, "rb", "runs.log")); !os.IsNotExist(err) {
		t.Error("runs.log written with audit off")
	}
	if len(s.Outputs("rb")) != 2 {
		t.Errorf("outputs: %+v", s.Outputs("rb"))
	}
}

func TestOutputIsCapped(t *testing.T) {
	s, d := setup(t, true)
	rec := &Recorder{Store: s, Name: "rb", Doc: d, Redactor: redact.New(nil)}
	id := d.Steps[0].ID
	rec.Handle(runner.Event{Type: runner.EventRunStart})
	rec.Handle(runner.Event{Type: runner.EventStepStart, StepID: id, Chunk: "yes"})
	chunk := strings.Repeat("y\n", 64*1024)
	for i := 0; i < 20; i++ { // 2.5MB
		rec.Handle(runner.Event{Type: runner.EventStepOutput, StepID: id, Chunk: chunk})
	}
	rec.Handle(runner.Event{Type: runner.EventStepFinish, StepID: id, Reason: runner.ReasonExit})
	o := s.Outputs("rb")[id].Output
	if len(o) > OutputCap+len(trimmedNote) || !strings.HasPrefix(o, trimmedNote) {
		t.Errorf("output len %d, prefix %q", len(o), o[:40])
	}
}

func TestGateDecisionsAreLogged(t *testing.T) {
	s, d := setup(t, true)
	rec := &Recorder{Store: s, Name: "rb", Doc: d, Redactor: redact.New(nil)}
	rec.Gate(d.Steps[0].ID, runner.GateApprove)
	rec.Gate(d.Steps[1].ID, runner.GateSkip)
	entries, _ := s.ReadLog("rb", 10)
	if len(entries) != 2 || entries[0].Event != "confirm.approve" || entries[1].Event != "confirm.skip" || entries[1].Step != 2 {
		t.Errorf("entries: %+v", entries)
	}
}
