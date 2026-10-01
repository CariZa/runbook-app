package runbook

import (
	"strings"
	"testing"
)

// The example from SPEC.md §1, verbatim (front-matter comments included).
const specExample = "---\n" +
	"name: db-latency-triage\n" +
	"cwd: ~/work/example-api\n" +
	"version: 3\n" +
	"parent: queue-backlog          # optional, set when forked\n" +
	"defaults:                       # runbook-level step defaults\n" +
	"  destructive: false\n" +
	"  continueOnFail: false\n" +
	"  timeoutSec: 60\n" +
	"running:\n" +
	"  pauseAtDestructive: true\n" +
	"  audit: true\n" +
	"---\n" +
	"\n" +
	"## 1. Check pod health\n" +
	"\n" +
	"```sh\n" +
	"kubectl get pods -n example-app | grep -v Running\n" +
	"```\n" +
	"\n" +
	"## 2. Restart the deployment\n" +
	"\n" +
	"```yaml meta\n" +
	"destructive: true\n" +
	"skipInRunAll: true\n" +
	"```\n" +
	"\n" +
	"```sh\n" +
	"kubectl rollout restart deploy/example-api\n" +
	"```\n" +
	"\n" +
	"## 3. Escalation note\n" +
	"\n" +
	"> If pool timeouts persist past 5m, page the DB oncall.\n"

func mustParse(t *testing.T, s string) Doc {
	t.Helper()
	d, err := Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func assertRoundTrip(t *testing.T, src string) {
	t.Helper()
	got := string(Serialize(mustParse(t, src)))
	if got != src {
		t.Errorf("round-trip changed the file\n--- want\n%s\n--- got\n%s", src, got)
	}
}

func TestSpecExampleRoundTripsByteIdentical(t *testing.T) {
	assertRoundTrip(t, specExample)
}

func TestSpecExampleParsesAsExpected(t *testing.T) {
	d := mustParse(t, specExample)
	if d.Name != "db-latency-triage" || d.Cwd != "~/work/example-api" || d.Version != 3 || d.Parent != "queue-backlog" {
		t.Errorf("front-matter: %+v", frontMatterFields(d))
	}
	if d.Defaults != (Defaults{TimeoutSec: 60}) || d.Running != (Running{PauseAtDestructive: true, Audit: true}) {
		t.Errorf("defaults/running: %+v %+v", d.Defaults, d.Running)
	}
	if len(d.Steps) != 3 {
		t.Fatalf("steps = %d", len(d.Steps))
	}
	s2 := d.Steps[1]
	r := d.Resolve(s2)
	if s2.Title != "Restart the deployment" || !r.Destructive || !r.SkipInRunAll || r.TimeoutSec != 60 || !r.RecordOutput {
		t.Errorf("step 2: %+v resolved %+v", s2, r)
	}
	if s3 := d.Steps[2]; s3.Kind != "note" || s3.Note != "If pool timeouts persist past 5m, page the DB oncall." {
		t.Errorf("step 3: %+v", s3)
	}
}

func TestCanonicalFeaturesRoundTrip(t *testing.T) {
	for name, src := range map[string]string{
		"no steps": "---\nname: empty\ncwd: ~\ndefaults:\n  destructive: false\n  continueOnFail: false\n  timeoutSec: 60\nrunning:\n  pauseAtDestructive: true\n  audit: true\n---\n",
		"preamble + multi-line command": "---\nname: x\n---\n\nSome context for the on-call.\n\nSecond paragraph.\n\n" +
			"## 1. Multi\n\n```sh\nkubectl logs pod \\\n  | tail -50\n\nexport A=1\n```\n",
		"command containing a fence": "---\nname: x\n---\n\n## 1. Heredoc\n\n````sh\ncat <<'EOF'\n```\nnested\n```\nEOF\n````\n",
		"multi-paragraph note":       "---\nname: x\n---\n\n## 1. Note\n\n> line one\n>\n>   indented\n",
		"all meta fields":            "---\nname: x\n---\n\n## 1. Everything\n\n```yaml meta\ndestructive: true\ncontinueOnFail: true\nskipInRunAll: true\ncwd: /tmp\ntimeoutSec: null\nrecordOutput: false\n```\n\n```sh\nvault read secret/x\n```\n",
		"extra content kept":         "---\nname: x\n---\n\n## 1. With notes\n\n```sh\ndf -h\n```\n\nCheck the /var line.\n\n```\nexample output\n```\n",
		"empty command":              "---\nname: x\n---\n\n## 1. Placeholder\n\n```sh\n```\n",
		"heading inside fence":       "---\nname: x\n---\n\n## 1. Docs\n\n```sh\ncat <<'EOF'\n## not a step\nEOF\n```\n",
	} {
		t.Run(name, func(t *testing.T) { assertRoundTrip(t, src) })
	}
}

func TestHeadingInsideFenceIsNotAStep(t *testing.T) {
	d := mustParse(t, "---\nname: x\n---\n\n## 1. Docs\n\n```sh\n## not a step\n```\n")
	if len(d.Steps) != 1 || d.Steps[0].Command != "## not a step" {
		t.Errorf("steps: %+v", d.Steps)
	}
}

func TestNonCanonicalInputIsNormalisedIdempotently(t *testing.T) {
	messy := "---\r\nname: messy\r\ncwd: /srv\r\n---\r\n" +
		"## 7) First\r\n\r\n\r\n```yaml meta\r\ndestructive: false\r\ntimeoutSec: 60\r\ncwd: inherit\r\n```\r\n```bash\r\nuptime\r\n```\r\n" +
		"## Second\n```\nls\n```\n"
	once := Serialize(mustParse(t, messy))
	twice := Serialize(mustParse(t, string(once)))
	if string(once) != string(twice) {
		t.Errorf("not idempotent\n--- once\n%s\n--- twice\n%s", once, twice)
	}
	want := "---\nname: messy\ncwd: /srv\n---\n\n## 1. First\n\n```sh\nuptime\n```\n\n## 2. Second\n\n```sh\nls\n```\n"
	if string(once) != want {
		t.Errorf("normalised form\n--- want\n%s\n--- got\n%s", want, once)
	}
}

func TestEditingARunbookFieldRegeneratesFrontMatter(t *testing.T) {
	d := mustParse(t, specExample)
	d.Cwd = "/tmp"
	out := string(Serialize(d))
	if strings.Contains(out, "# optional") {
		t.Error("stale raw front-matter kept after a change")
	}
	if !strings.Contains(out, "cwd: /tmp\n") || !strings.Contains(out, "parent: queue-backlog\n") {
		t.Errorf("front-matter:\n%s", out)
	}
	// Steps are untouched.
	if !strings.HasSuffix(out, specExample[strings.Index(specExample, "## 1."):]) {
		t.Error("steps changed")
	}
}

func TestOverridesEqualToDefaultAreNotWritten(t *testing.T) {
	yes, no, sixty := true, false, 60
	d := NewDoc("x", "~")
	d.Defaults.Destructive = true
	d.Steps = []Step{{Title: "a", Kind: "command", Command: "true", Destructive: &yes, ContinueOnFail: &no, TimeoutSec: &sixty}}
	if out := string(Serialize(d)); strings.Contains(out, "yaml meta") {
		t.Errorf("redundant meta written:\n%s", out)
	}
	d.Steps[0].Destructive = &no
	if out := string(Serialize(d)); !strings.Contains(out, "```yaml meta\ndestructive: false\n```") {
		t.Errorf("override missing:\n%s", out)
	}
}

func TestNewDocSerializesAndParsesBack(t *testing.T) {
	d := NewDoc("fresh", "~")
	d.Steps = []Step{
		{Title: "Look", Kind: "command", Command: "ls -la"},
		{Title: "Remember", Kind: "note", Note: "Page someone."},
	}
	out := Serialize(d)
	back := mustParse(t, string(out))
	if back.Name != "fresh" || back.Cwd != "~" || len(back.Steps) != 2 || back.Steps[1].Note != "Page someone." {
		t.Errorf("back: %+v", back)
	}
	if string(Serialize(back)) != string(out) {
		t.Error("new doc does not round-trip")
	}
}

func TestBadFrontMatter(t *testing.T) {
	for _, src := range []string{
		"---\nname: x\n",
		"---\ndefaults:\n  destructive: maybe\n---\n",
		"---\ndefaults:\n  timeoutSec: soon\n---\n",
	} {
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("expected an error for %q", src)
		}
	}
}

func TestNotionLinkRoundTrips(t *testing.T) {
	src := "---\nname: x\ncwd: \"~\"\ndefaults:\n  destructive: false\n  continueOnFail: false\n  timeoutSec: 60\n" +
		"running:\n  pauseAtDestructive: true\n  audit: true\n" +
		"notion:\n  pageId: 1f2e3d4c-5b6a-7980-9102-3344556677ff\n  url: https://www.notion.so/acme/Disk-cleanup-1f2e\n---\n\n" +
		"## 1. Look\n\n```sh\nls\n```\n"
	assertRoundTrip(t, src)
	d := mustParse(t, src)
	if d.Notion.PageID != "1f2e3d4c-5b6a-7980-9102-3344556677ff" || d.Notion.URL == "" {
		t.Errorf("notion: %+v", d.Notion)
	}
	// Setting it on a runbook that had none regenerates the front-matter with it.
	fresh := NewDoc("y", "~")
	fresh.Notion = Notion{PageID: "abc", URL: "https://notion.so/abc"}
	out := string(Serialize(fresh))
	if !strings.Contains(out, "notion:\n  pageId: abc\n  url: https://notion.so/abc\n") {
		t.Errorf("serialised:\n%s", out)
	}
}
