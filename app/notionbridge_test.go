package main

import "testing"

func TestRunbookNameFromPageTitle(t *testing.T) {
	for title, want := range map[string]string{
		"Disk cleanup":               "disk-cleanup",
		"GCP / disks: orphaned PVCs": "gcp-disks-orphaned-pvcs",
		"  Spaces  everywhere  ":     "spaces-everywhere",
		"Runbook (v2) — 2026":        "runbook-v2-2026",
		"🚨 incident response":        "incident-response",
		"":                           "from-notion",
		"🚀🚀🚀":                        "from-notion",
	} {
		if got := runbookName(title); got != want {
			t.Errorf("runbookName(%q) = %q, want %q", title, got, want)
		}
	}
	long := runbookName(string(make([]byte, 0)) + "a-very-long-title-that-keeps-going-and-going-and-going-and-going-and-going")
	if len(long) > 64 {
		t.Errorf("not truncated: %q", long)
	}
}
