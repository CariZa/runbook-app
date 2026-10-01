package runbook

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func exportFixture() (Doc, map[string]Output) {
	yes, no, five, none := true, false, 5, 0
	d := NewDoc("db-latency-triage", "~/work/example-api")
	d.Defaults.ContinueOnFail = true // a runbook default: must travel as an explicit setting
	d.Steps = []Step{
		{ID: "a", Title: "Check pod health", Kind: "command", Command: "kubectl get pods -n example-app | grep -v Running"},
		{ID: "b", Title: "Tail logs", Kind: "command", Command: "kubectl logs example-api-7f9 --since=15m \\\n  | tail -50", TimeoutSec: &five},
		{ID: "c", Title: "Heredoc with a fence", Kind: "command", Command: "cat <<'EOF'\n```\nnested\n```\nEOF", Destructive: &yes, SkipInRunAll: &yes},
		{ID: "d", Title: "Escalation", Kind: "note", Note: "If pool timeouts persist past 5m, page the DB oncall.\n\nThen update the incident channel."},
		{ID: "e", Title: "Vault read", Kind: "command", Command: "vault read secret/x", RecordOutput: &no, ContinueOnFail: &no, TimeoutSec: &none, Cwd: "/tmp"},
	}
	out := map[string]Output{
		"a": {Output: "example-api-7f9   0/1   CrashLoopBackOff   4   12m\n", ExitCode: 0, DurationMs: 1200, StartedAt: time.Date(2026, 9, 21, 14, 3, 0, 0, time.UTC), Reason: "exit"},
		"b": {Output: "```\nfenced log line\n```\n", ExitCode: 1, DurationMs: 300, Reason: "exit"},
	}
	return d, out
}

func TestExportMatchesWireframe(t *testing.T) {
	d, out := exportFixture()
	got := Export(d, out, ExportOptions{StepIDs: []string{"a", "b"}, Outputs: true})
	want := "# db-latency-triage\nPath: `~/work/example-api`\n\n" +
		"## 1. Check pod health\n\n```sh\nkubectl get pods -n example-app | grep -v Running\n```\n\n" +
		"```\nexample-api-7f9   0/1   CrashLoopBackOff   4   12m\nexit 0 · 1.2s\n```\n\n" +
		"## 2. Tail logs\n\n```sh\nkubectl logs example-api-7f9 --since=15m \\\n  | tail -50\n```\n\n" +
		"````\n```\nfenced log line\n```\nexit 1 · 0.3s\n````\n"
	if got != want {
		t.Errorf("export\n--- want\n%s\n--- got\n%s", want, got)
	}
}

func TestExportPastesBackToTheSameSteps(t *testing.T) {
	d, out := exportFixture()
	for _, opts := range []ExportOptions{
		{},
		{Outputs: true, Timestamps: true},
		{Outputs: true, Settings: true, Timestamps: true},
	} {
		res := ParsePaste(Export(d, out, opts))
		if len(res.Steps) != len(d.Steps) {
			t.Fatalf("%+v: %d steps, want %d\n%+v", opts, len(res.Steps), len(d.Steps), res)
		}
		for i, got := range res.Steps {
			want := d.Steps[i]
			if got.Title != want.Title || got.Kind != want.Kind || got.Command != want.Command || got.Note != want.Note {
				t.Errorf("%+v step %d:\n got  %+v\n want %+v", opts, i+1, got, want)
			}
			if opts.Settings && want.Kind == "command" {
				// Settings must mean the same thing in a runbook with built-in defaults.
				if g, w := NewDoc("", "").Resolve(got), d.Resolve(want); g != w {
					t.Errorf("step %d settings: got %+v, want %+v", i+1, g, w)
				}
			}
		}
		for _, drop := range res.Dropped {
			if !strings.Contains(drop, "title line") && !strings.Contains(drop, "paragraph") && !strings.Contains(drop, "example output") {
				t.Errorf("unexpected drop %q", drop)
			}
		}
	}
}

func TestPasteTerminalSession(t *testing.T) {
	res := ParsePaste("$ kubectl get pods\nNAME      READY\npod-1     1/1\n% echo hi\n> ls -la\n$ exit_code() { return 3; }\n")
	var cmds []string
	for _, s := range res.Steps {
		if s.Kind != "command" || s.Title != s.Command {
			t.Errorf("step %+v", s)
		}
		cmds = append(cmds, s.Command)
	}
	want := []string{"kubectl get pods", "echo hi", "ls -la", "exit_code() { return 3; }"}
	if !reflect.DeepEqual(cmds, want) {
		t.Errorf("commands %q", cmds)
	}
	if !reflect.DeepEqual(res.Dropped, []string{"2 output lines"}) {
		t.Errorf("dropped %q", res.Dropped)
	}
}

func TestPasteNumberedList(t *testing.T) {
	src := "Runbook for the DB.\n\n1. Check pods\n   ```\n   kubectl get pods\n   ```\n2) Restart it\n   ```bash\n   kubectl rollout restart deploy/x\n     --timeout=60s\n   ```\n3. Call the DB oncall\n"
	res := ParsePaste(src)
	if len(res.Steps) != 3 {
		t.Fatalf("steps: %+v", res.Steps)
	}
	if s := res.Steps[0]; s.Title != "Check pods" || s.Command != "kubectl get pods" {
		t.Errorf("1: %+v", s)
	}
	if s := res.Steps[1]; s.Title != "Restart it" || s.Command != "kubectl rollout restart deploy/x\n  --timeout=60s" {
		t.Errorf("2: %+v", s)
	}
	if s := res.Steps[2]; s.Title != "Call the DB oncall" || s.Kind != "note" {
		t.Errorf("3 (manual step) %+v", s)
	}
	if !reflect.DeepEqual(res.Dropped, []string{"1 paragraph"}) {
		t.Errorf("dropped %q", res.Dropped)
	}
}

func TestPasteHeadings(t *testing.T) {
	src := "# Incident notes\n\n## Step 1: Look at pods\n\nFirst we look.\n\n```sh\nkubectl get pods\n```\n\n```\npod-1 Running\n```\n\n" +
		"### 2) Decide\n\nIf more than 3 pods restart,\nescalate to the platform team.\n\n" +
		"## Reminder\n\n> Update the status page.\n> Every 30 minutes.\n\n" +
		"## 4. Prompt inside a heading doc\n\n$ uptime\n"
	res := ParsePaste(src)
	if len(res.Steps) != 4 {
		t.Fatalf("steps: %+v", res.Steps)
	}
	if s := res.Steps[0]; s.Title != "Look at pods" || s.Command != "kubectl get pods" || s.Kind != "command" {
		t.Errorf("1: %+v", s)
	}
	if s := res.Steps[1]; s.Title != "Decide" || s.Kind != "note" || s.Note != "If more than 3 pods restart,\nescalate to the platform team." {
		t.Errorf("2: %+v", s)
	}
	if s := res.Steps[2]; s.Kind != "note" || s.Note != "Update the status page.\nEvery 30 minutes." {
		t.Errorf("3 (blockquote is a note, not a prompt): %+v", s)
	}
	if s := res.Steps[3]; s.Kind != "command" || s.Command != "uptime" || s.Title != "Prompt inside a heading doc" {
		t.Errorf("4: %+v", s)
	}
	want := []string{"1 example output block", "1 paragraph", "1 title line"}
	if !reflect.DeepEqual(res.Dropped, want) {
		t.Errorf("dropped %q, want %q", res.Dropped, want)
	}
}

func TestPasteBareFenceBecomesAStep(t *testing.T) {
	res := ParsePaste("```\nsystemctl status nginx\njournalctl -u nginx -n 50\n```")
	if len(res.Steps) != 1 || res.Steps[0].Title != "systemctl status nginx" || !strings.Contains(res.Steps[0].Command, "journalctl") {
		t.Errorf("%+v", res)
	}
	if res := ParsePaste("   \n\n"); len(res.Steps) != 0 {
		t.Errorf("blank paste gave %+v", res)
	}
}
