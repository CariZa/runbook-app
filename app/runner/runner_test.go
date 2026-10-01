package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const testGrace = 300 * time.Millisecond

type recorder struct {
	mu     sync.Mutex
	events []Event
}

func (r *recorder) add(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recorder) output(stepID string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var b strings.Builder
	for _, e := range r.events {
		if e.Type == EventStepOutput && e.StepID == stepID {
			b.WriteString(e.Chunk)
		}
	}
	return b.String()
}

func (r *recorder) find(t EventType, stepID string) (Event, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if e.Type == t && e.StepID == stepID {
			return e, true
		}
	}
	return Event{}, false
}

func (r *recorder) finish(t *testing.T, stepID string) Event {
	t.Helper()
	e, ok := r.find(EventStepFinish, stepID)
	if !ok {
		t.Fatalf("step %s never finished; events: %+v", stepID, r.events)
	}
	return e
}

func (r *recorder) notStarted(t *testing.T, stepID string) {
	t.Helper()
	if _, ok := r.find(EventStepStart, stepID); ok {
		t.Fatalf("step %s should not have run", stepID)
	}
}

func cmd(id, command string) Step {
	return Step{ID: id, Title: id, Kind: "command", Command: command, TimeoutSec: 10}
}

func run(t *testing.T, steps []Step, opts RunOptions, gate func(context.Context, Step) GateDecision) (*recorder, Outcome) {
	t.Helper()
	if opts.Dir == "" {
		opts.Dir = t.TempDir()
	}
	if opts.KillGrace == 0 {
		opts.KillGrace = testGrace
	}
	rec := &recorder{}
	out := Run(context.Background(), steps, opts, Hooks{Event: rec.add, Gate: gate})
	return rec, out
}

func TestOutputAndExitCodes(t *testing.T) {
	rec, out := run(t, []Step{
		cmd("ok", "echo hello; echo oops >&2"),
		{ID: "bad", Kind: "command", Command: "false", ContinueOnFail: true},
		cmd("noeol", "printf abc"),
	}, RunOptions{}, nil)

	if out != OutcomeCompleted {
		t.Fatalf("outcome = %s", out)
	}
	if got := rec.output("ok"); got != "hello\noops\n" {
		t.Errorf("stdout+stderr = %q", got)
	}
	if e := rec.finish(t, "bad"); e.ExitCode != 1 || e.Reason != ReasonExit {
		t.Errorf("false: %+v", e)
	}
	if got := rec.output("noeol"); got != "abc" {
		t.Errorf("no trailing newline = %q", got)
	}
}

func TestStatePersistsBetweenSteps(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	rec, _ := run(t, []Step{
		cmd("a", "mkdir sub && cd sub && export FOO=bar && greet() { echo hi-$1; }"),
		cmd("b", `pwd -P; echo "$FOO"; greet x`),
	}, RunOptions{Dir: dir}, nil)

	want, _ := filepath.EvalSymlinks(sub)
	if got := rec.output("b"); got != want+"\nbar\nhi-x\n" {
		t.Errorf("got %q, want cwd %s, FOO=bar, function kept", got, want)
	}
}

func TestMultilineCommandsAndQuoting(t *testing.T) {
	rec, _ := run(t, []Step{
		cmd("m", "x='it'\"'\"'s'\necho \"$x\" \\\n  | tr a-z A-Z\ncat <<'EOF'\n$HOME stays literal\nEOF"),
	}, RunOptions{}, nil)
	if got := rec.output("m"); got != "IT'S\n$HOME stays literal\n" {
		t.Errorf("got %q", got)
	}
}

func TestStepCannotReadCommandStream(t *testing.T) {
	rec, out := run(t, []Step{
		cmd("cat", "cat; echo cat-done"),
		cmd("read", `read x; echo "read-rc=$?"`),
		cmd("after", "echo still-here"),
	}, RunOptions{}, nil)
	if out != OutcomeCompleted {
		t.Fatalf("outcome = %s", out)
	}
	if got := rec.output("cat"); got != "cat-done\n" {
		t.Errorf("cat = %q", got)
	}
	if got := rec.output("read"); got != "read-rc=1\n" {
		t.Errorf("read = %q", got)
	}
	if got := rec.output("after"); got != "still-here\n" {
		t.Errorf("after = %q", got)
	}
}

func TestPerStepCwdIsRestored(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	st := cmd("elsewhere", "pwd -P")
	st.Cwd = other
	rec, _ := run(t, []Step{st, cmd("back", "pwd -P")}, RunOptions{Dir: dir}, nil)

	wantOther, _ := filepath.EvalSymlinks(other)
	wantDir, _ := filepath.EvalSymlinks(dir)
	if got := rec.output("elsewhere"); got != wantOther+"\n" {
		t.Errorf("step cwd = %q", got)
	}
	if got := rec.output("back"); got != wantDir+"\n" {
		t.Errorf("after step cwd = %q", got)
	}
}

func TestBadStepCwdFailsTheStep(t *testing.T) {
	st := cmd("x", "echo should-not-run")
	st.Cwd = "/does/not/exist"
	rec, out := run(t, []Step{st, cmd("y", "echo y")}, RunOptions{}, nil)
	if out != OutcomeFailed {
		t.Fatalf("outcome = %s", out)
	}
	if e := rec.finish(t, "x"); e.ExitCode == 0 {
		t.Errorf("expected failure: %+v", e)
	}
	if strings.Contains(rec.output("x"), "should-not-run") {
		t.Error("command ran despite failed cd")
	}
	rec.notStarted(t, "y")
}

func TestStopOnFailIsTheDefault(t *testing.T) {
	rec, out := run(t, []Step{cmd("1", "true"), cmd("2", "exit_code() { return 7; }; exit_code"), cmd("3", "echo nope")}, RunOptions{}, nil)
	if out != OutcomeFailed {
		t.Fatalf("outcome = %s", out)
	}
	if e := rec.finish(t, "2"); e.ExitCode != 7 {
		t.Errorf("exit = %d", e.ExitCode)
	}
	rec.notStarted(t, "3")
}

func TestContinueOnFail(t *testing.T) {
	bad := cmd("2", "ls /definitely/missing")
	bad.ContinueOnFail = true
	rec, out := run(t, []Step{cmd("1", "true"), bad, cmd("3", "echo yes")}, RunOptions{}, nil)
	if out != OutcomeCompleted {
		t.Fatalf("outcome = %s", out)
	}
	if e := rec.finish(t, "2"); e.ExitCode == 0 {
		t.Errorf("step 2 should fail: %+v", e)
	}
	if rec.output("3") != "yes\n" {
		t.Error("step 3 did not run")
	}
}

func TestSkipInRunAllAndNotes(t *testing.T) {
	skip := cmd("skip", "echo skipped")
	skip.SkipInRunAll = true
	note := Step{ID: "note", Kind: "note"}
	rec, _ := run(t, []Step{skip, note, cmd("next", "echo next")}, RunOptions{}, nil)
	if _, ok := rec.find(EventStepSkip, "skip"); !ok {
		t.Error("no step.skip event")
	}
	rec.notStarted(t, "skip")
	rec.notStarted(t, "note")
	if rec.output("next") != "next\n" {
		t.Error("next step did not run")
	}

	// Its own ▶ still runs it.
	rec, _ = run(t, []Step{skip}, RunOptions{Single: true}, nil)
	if rec.output("skip") != "skipped\n" {
		t.Error("single run should ignore skipInRunAll")
	}
}

func TestTimeoutEndsRunByDefault(t *testing.T) {
	st := cmd("slow", "echo started; sleep 30")
	st.TimeoutSec = 1
	start := time.Now()
	rec, out := run(t, []Step{st, cmd("after", "echo nope")}, RunOptions{}, nil)
	if d := time.Since(start); d > 4*time.Second {
		t.Errorf("took %s", d)
	}
	if out != OutcomeFailed {
		t.Errorf("outcome = %s", out)
	}
	if e := rec.finish(t, "slow"); e.Reason != ReasonTimeout {
		t.Errorf("reason = %s", e.Reason)
	}
	if rec.output("slow") != "started\n" {
		t.Errorf("output = %q", rec.output("slow"))
	}
	rec.notStarted(t, "after")
}

func TestTimeoutWithContinueOnFailKeepsTheShell(t *testing.T) {
	slow := cmd("slow", "sleep 30 | cat")
	slow.TimeoutSec = 1
	slow.ContinueOnFail = true
	rec, out := run(t, []Step{cmd("set", "export KEEP=me"), slow, cmd("check", `echo "$KEEP"`)}, RunOptions{}, nil)
	if out != OutcomeCompleted {
		t.Fatalf("outcome = %s", out)
	}
	if e := rec.finish(t, "slow"); e.Reason != ReasonTimeout || e.ExitCode == 0 {
		t.Errorf("slow: %+v", e)
	}
	if got := rec.output("check"); got != "me\n" {
		t.Errorf("shell state lost after timeout: %q", got)
	}
}

func TestTimeoutInShellBuiltinLoopKillsSession(t *testing.T) {
	loop := cmd("loop", "while :; do :; done")
	loop.TimeoutSec = 1
	loop.ContinueOnFail = true
	start := time.Now()
	rec, out := run(t, []Step{loop, cmd("after", "echo nope")}, RunOptions{}, nil)
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("took %s", d)
	}
	if out != OutcomeFailed {
		t.Errorf("outcome = %s", out)
	}
	if e := rec.finish(t, "loop"); e.Reason != ReasonTimeout {
		t.Errorf("reason = %s", e.Reason)
	}
	rec.notStarted(t, "after")
}

func TestStopKillsProcessesEvenIfTheyIgnoreSIGTERM(t *testing.T) {
	marker := fmt.Sprintf("%d.%d", 4000+os.Getpid()%1000, time.Now().UnixMicro()%100000)
	steps := []Step{
		cmd("stubborn", `bash -c "trap '' TERM; sleep `+marker+`"`),
		cmd("after", "echo nope"),
	}
	ctx, cancel := context.WithCancel(context.Background())
	rec := &recorder{}
	done := make(chan Outcome)
	go func() {
		done <- Run(ctx, steps, RunOptions{Dir: t.TempDir(), KillGrace: testGrace}, Hooks{Event: rec.add})
	}()
	time.Sleep(500 * time.Millisecond)
	start := time.Now()
	cancel()
	out := <-done
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("stop took %s", d)
	}
	if out != OutcomeStopped {
		t.Errorf("outcome = %s", out)
	}
	if e := rec.finish(t, "stubborn"); e.Reason != ReasonStopped {
		t.Errorf("reason = %s", e.Reason)
	}
	rec.notStarted(t, "after")

	time.Sleep(200 * time.Millisecond)
	if ps, _ := exec.Command("pgrep", "-f", "sleep "+marker).Output(); len(ps) > 0 {
		t.Errorf("leftover processes: %s", ps)
	}
}

func TestExitInStepEndsRun(t *testing.T) {
	rec, out := run(t, []Step{cmd("x", "echo bye; exit 3"), cmd("y", "echo nope")}, RunOptions{}, nil)
	if out != OutcomeFailed {
		t.Errorf("outcome = %s", out)
	}
	e := rec.finish(t, "x")
	if e.Reason != ReasonShellExited || e.ExitCode != 3 {
		t.Errorf("finish = %+v", e)
	}
	if rec.output("x") != "bye\n" {
		t.Errorf("output = %q", rec.output("x"))
	}
	rec.notStarted(t, "y")
}

func TestOutputStreamsBeforeStepEnds(t *testing.T) {
	sess, err := StartSession(SessionOptions{Dir: t.TempDir(), KillGrace: testGrace})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	first := make(chan time.Time, 1)
	start := time.Now()
	sess.Exec(context.Background(), ExecRequest{Command: "echo hi; sleep 1"}, func(b []byte) {
		select {
		case first <- time.Now():
		default:
		}
	})
	select {
	case at := <-first:
		if d := at.Sub(start); d > 500*time.Millisecond {
			t.Errorf("first output arrived after %s; want it streamed immediately", d)
		}
	default:
		t.Fatal("no output")
	}
}

func TestLargeOutputAcrossChunks(t *testing.T) {
	rec, _ := run(t, []Step{cmd("seq", "seq 1 200000"), cmd("next", "echo next")}, RunOptions{}, nil)
	got := rec.output("seq")
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(lines) != 200000 || lines[0] != "1" || lines[len(lines)-1] != "200000" {
		t.Errorf("got %d lines", len(lines))
	}
	if rec.output("next") != "next\n" {
		t.Error("marker lost across chunks")
	}
}

func TestDestructiveGate(t *testing.T) {
	danger := cmd("danger", "echo boom")
	danger.Destructive = true
	steps := []Step{danger, cmd("after", "echo after")}
	opts := RunOptions{PauseAtDestructive: true}

	for _, tc := range []struct {
		decision   GateDecision
		ranDanger  bool
		ranAfter   bool
		wantResult Outcome
	}{
		{GateApprove, true, true, OutcomeCompleted},
		{GateSkip, false, true, OutcomeCompleted},
		{GateStop, false, false, OutcomeStopped},
	} {
		t.Run(string(tc.decision), func(t *testing.T) {
			rec, out := run(t, steps, opts, func(context.Context, Step) GateDecision { return tc.decision })
			if out != tc.wantResult {
				t.Errorf("outcome = %s", out)
			}
			if _, ok := rec.find(EventGate, "danger"); !ok {
				t.Error("no gate event")
			}
			if got := rec.output("danger") != ""; got != tc.ranDanger {
				t.Errorf("danger ran = %v", got)
			}
			if got := rec.output("after") != ""; got != tc.ranAfter {
				t.Errorf("after ran = %v", got)
			}
		})
	}

	// No gate without pauseAtDestructive, or for a single-step run (the UI confirms first).
	rec, _ := run(t, steps, RunOptions{}, func(context.Context, Step) GateDecision { return GateStop })
	if rec.output("danger") != "boom\n" {
		t.Error("gate should be off")
	}
}

func TestBadRunbookDir(t *testing.T) {
	rec, out := run(t, []Step{cmd("x", "true")}, RunOptions{Dir: "/does/not/exist"}, nil)
	if out != OutcomeError {
		t.Errorf("outcome = %s", out)
	}
	rec.notStarted(t, "x")
}

func TestLoginEnvHasPath(t *testing.T) {
	env := LoginEnv(10 * time.Second)
	var path, pager string
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok {
			path = v
		}
		if v, ok := strings.CutPrefix(kv, "PAGER="); ok {
			pager = v
		}
	}
	if path == "" {
		t.Error("no PATH")
	}
	if pager != "cat" {
		t.Errorf("PAGER = %q", pager)
	}
}

func TestMarkerSplitAcrossReads(t *testing.T) {
	s := &Session{nonce: "abc"}
	marker := s.markerPrefix() + "42:/some/dir\x1e\n"
	var got []byte
	collect := func(p []byte) { got = append(got, p...) }

	found := false
	for _, b := range []byte("out\x1eput\n" + marker) {
		code, ok := s.consume([]byte{b}, collect)
		if ok {
			if found || code != 42 {
				t.Fatalf("found twice or wrong code %d", code)
			}
			found = true
		}
	}
	if !found || string(got) != "out\x1eput\n" || s.cwd != "/some/dir" {
		t.Fatalf("found=%v output=%q", found, got)
	}

	// The marker's newline arrived in its own read; it must not leak into the next step.
	got = nil
	for _, b := range []byte("next") {
		s.consume([]byte{b}, collect)
	}
	if string(got)+string(s.pending) != "next" {
		t.Fatalf("next step output = %q + pending %q", got, s.pending)
	}
}

func TestShellMessagesDontLeakTempPaths(t *testing.T) {
	st := cmd("slow", "sleep 30")
	st.TimeoutSec = 1
	st.ContinueOnFail = true
	rec, _ := run(t, []Step{st}, RunOptions{}, nil)
	if out := rec.output("slow"); strings.Contains(out, "runbook-session-") {
		t.Errorf("temp path leaked: %q", out)
	}
}

func TestSharedSessionKeepsStateAcrossRuns(t *testing.T) {
	dir := t.TempDir()
	sess, err := StartSession(SessionOptions{Dir: dir, KillGrace: testGrace})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	opts := RunOptions{Single: true, Session: sess}

	run(t, []Step{cmd("a", "cd /tmp && export SHARED=yes")}, opts, nil)
	if !sess.Alive() {
		t.Fatal("run closed a shared session")
	}
	if got, _ := filepath.EvalSymlinks(sess.Cwd()); got != "/private/tmp" {
		t.Errorf("session cwd = %q", sess.Cwd())
	}
	rec, _ := run(t, []Step{cmd("b", `echo "$SHARED"`)}, opts, nil)
	if rec.output("b") != "yes\n" {
		t.Errorf("state not shared: %q", rec.output("b"))
	}
	if e := rec.finish(t, "b"); e.Cwd != sess.Cwd() {
		t.Errorf("finish cwd = %q", e.Cwd)
	}
	if e, _ := rec.find(EventStepStart, "b"); e.Chunk != `echo "$SHARED"` || e.Cwd != sess.Cwd() {
		t.Errorf("step.start = %+v", e)
	}

	// Stop kills the shared shell.
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	Run(ctx, []Step{cmd("c", "sleep 30")}, opts, Hooks{})
	if sess.Alive() {
		t.Error("stop should kill the shared session")
	}
}
