package main

import (
	"testing"

	"runbook/runbook"
)

func TestResolveStepsFillsEachStepsOwnArgs(t *testing.T) {
	d := runbook.NewDoc("x", "~")
	d.Steps = []runbook.Step{
		{ID: "del", Kind: "command", Command: "gcloud compute disks delete {{disk}} --zone={{zone}}"},
		{ID: "note", Kind: "note", Note: "mentions {{nothing}}"},
		{ID: "other", Kind: "command", Command: "echo {{disk}}"},
	}
	args := map[string]map[string]string{
		"del":   {"disk": "pvc-1", "zone": "us-central1-a"},
		"other": {"disk": "pvc-2"},
	}
	steps, err := resolveSteps(d, []string{"del", "note", "other"}, args, false)
	if err != nil {
		t.Fatal(err)
	}
	if steps[0].Command != "gcloud compute disks delete pvc-1 --zone=us-central1-a" || steps[2].Command != "echo pvc-2" {
		t.Errorf("commands %q / %q", steps[0].Command, steps[2].Command)
	}

	// A value typed into one step is not used by another.
	_, err = resolveSteps(d, []string{"del", "other"}, map[string]map[string]string{"del": {"disk": "pvc-1", "zone": "z"}}, false)
	if err == nil || err.Error() != "fill in disk in step 3 first" {
		t.Errorf("err %v", err)
	}

	// Run all passes over skipInRunAll steps, so their blanks don't block it.
	yes := true
	d.Steps[2].SkipInRunAll = &yes
	if _, err := resolveSteps(d, []string{"other"}, nil, false); err != nil {
		t.Errorf("skipped step blocked Run all: %v", err)
	}
	if _, err := resolveSteps(d, []string{"other"}, nil, true); err == nil {
		t.Error("its own ▶ must still require the value")
	}
}
