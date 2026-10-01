package runbook

import (
	"os/exec"
	"reflect"
	"testing"
)

const diskCmd = "gcloud compute disks delete {{disk}} --zone={{zone}} --project={{project}}"

func TestArgNames(t *testing.T) {
	if got := ArgNames(diskCmd + " && echo {{ disk }} {{1}}"); !reflect.DeepEqual(got, []string{"disk", "zone", "project", "1"}) {
		t.Errorf("names %q", got)
	}
	if got := ArgNames("echo ${HOME} $1 {single} {{ not valid! }}"); got != nil {
		t.Errorf("shell syntax mistaken for args: %q", got)
	}
}

func TestSubstitute(t *testing.T) {
	got, missing := Substitute(diskCmd, map[string]string{
		"disk": "pvc-example-0001", "zone": " us-central1-a ", "project": "example-project",
	})
	if want := "gcloud compute disks delete pvc-example-0001 --zone=us-central1-a --project=example-project"; got != want || missing != nil {
		t.Errorf("got %q missing %q", got, missing)
	}

	got, missing = Substitute(diskCmd+" {{disk}}", map[string]string{"zone": "z", "disk": "  "})
	if !reflect.DeepEqual(missing, []string{"disk", "project"}) || got != "gcloud compute disks delete {{disk}} --zone=z --project={{project}} {{disk}}" {
		t.Errorf("got %q missing %q", got, missing)
	}
}

// Whatever the value, it must reach the command as exactly one argument, unchanged.
func TestSubstitutedValuesAreOneLiteralWord(t *testing.T) {
	for _, v := range []string{
		"pvc-1", "has space", "semi;colon", "$(touch /tmp/pwned)", "`id`", "it's", `back\slash`,
		"a && rm -rf ~", "*", "new\nline", `"double"`, "{{nested}}", "--flag=x y",
	} {
		cmd, missing := Substitute(`f() { printf '%s\n' "$#" "$@"; }; f {{v}}`, map[string]string{"v": v})
		if missing != nil {
			t.Fatalf("missing for %q", v)
		}
		out, err := exec.Command("/bin/bash", "--noprofile", "--norc", "-c", cmd).Output()
		if err != nil {
			t.Fatalf("%q: %v", v, err)
		}
		if want := "1\n" + v + "\n"; string(out) != want {
			t.Errorf("value %q ran as %q, want %q (command %q)", v, out, want, cmd)
		}
	}
}
