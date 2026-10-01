package main

import "runbook/runbook"

// sampleDoc is written to ~/runbooks/shell-demo when the library is empty, so a first
// launch has something to run. Every command is harmless.
func sampleDoc() runbook.Doc {
	yes, three := true, 3
	d := runbook.NewDoc("shell-demo", "~")
	d.Preamble = "A tour of how steps run. Every command here is harmless."
	d.Steps = []runbook.Step{
		{Title: "Move to /tmp and set a variable", Kind: "command", Command: "cd /tmp && export DEMO_GREETING=\"hello from step 1\"\npwd"},
		{Title: "State carries over from step 1", Kind: "command", Command: "pwd\necho \"$DEMO_GREETING\""},
		{Title: "Stream some output", Kind: "command", Command: "for i in 1 2 3 4 5 6; do\n  echo \"tick $i\"\n  sleep 0.5\ndone"},
		{Title: "Look for a missing file (keeps going on failure)", Kind: "command", Command: "ls /tmp/runbook-demo-does-not-exist", ContinueOnFail: &yes},
		{Title: "Slow step with a 3s timeout (keeps going)", Kind: "command", Command: "echo \"waiting...\"\nsleep 10", TimeoutSec: &three, ContinueOnFail: &yes},
		{Title: "Variable survived the timeout", Kind: "command", Command: "echo \"$DEMO_GREETING\""},
		{Title: "Pretend restart (destructive)", Kind: "command", Command: "echo \"pretend: kubectl rollout restart deploy/example-api\"", Destructive: &yes},
		{Title: "This one fails and stops the run", Kind: "command", Command: "echo \"about to fail\"\nexit_with() { return \"$1\"; }; exit_with 2"},
		{Title: "Never reached by Run all", Kind: "command", Command: "echo \"you ran me on my own\""},
		{Title: "Escalation note", Kind: "note", Note: "If pool timeouts persist past 5m, page the DB oncall."},
	}
	return d
}
