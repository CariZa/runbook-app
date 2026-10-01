// Package runbook reads and writes runbook.md (SPEC.md §1) and manages the on-disk
// library: ~/runbooks/<name>/{runbook.md, runs.log, versions/, .runbook/}.
// It has no Wails dependency.
package runbook

// Built-in step defaults (SPEC.md §1 "Step fields").
const (
	BuiltinTimeoutSec = 60
)

// Doc is one runbook as the app edits it.
type Doc struct {
	Name     string   `json:"name"`
	Cwd      string   `json:"cwd"`
	Version  int      `json:"version"`
	Parent   string   `json:"parent"`
	Defaults Defaults `json:"defaults"`
	Running  Running  `json:"running"`
	Notion   Notion   `json:"notion"`
	Steps    []Step   `json:"steps"`

	// Preamble is any text between the front-matter and the first step, kept verbatim.
	Preamble string `json:"preamble"`
	// FrontMatterRaw is the front-matter as read from disk. It is written back unchanged
	// (comments and all) unless a runbook-level field differs from what it says.
	FrontMatterRaw string `json:"frontMatterRaw"`
}

// Defaults are runbook-level step defaults.
type Defaults struct {
	Destructive    bool `json:"destructive" yaml:"destructive"`
	ContinueOnFail bool `json:"continueOnFail" yaml:"continueOnFail"`
	TimeoutSec     int  `json:"timeoutSec" yaml:"timeoutSec"` // 0 = no timeout
}

// Running holds the run-level toggles.
type Running struct {
	PauseAtDestructive bool `json:"pauseAtDestructive" yaml:"pauseAtDestructive"`
	Audit              bool `json:"audit" yaml:"audit"`
}

// Notion records where a runbook was imported from, so it can be reopened there.
type Notion struct {
	PageID string `json:"pageId" yaml:"pageId"`
	URL    string `json:"url" yaml:"url"`
}

// Step is one step. Pointer fields are overrides; nil means "inherit".
type Step struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Kind           string `json:"kind"` // "command" | "note"
	Command        string `json:"command"`
	Note           string `json:"note"`
	Destructive    *bool  `json:"destructive,omitempty"`
	ContinueOnFail *bool  `json:"continueOnFail,omitempty"`
	SkipInRunAll   *bool  `json:"skipInRunAll,omitempty"`
	Cwd            string `json:"cwd,omitempty"`        // "" = inherit
	TimeoutSec     *int   `json:"timeoutSec,omitempty"` // 0 = no timeout
	RecordOutput   *bool  `json:"recordOutput,omitempty"`

	// Extra is content under the step that isn't meta, command or note; kept verbatim.
	Extra string `json:"extra,omitempty"`
}

// NewDoc returns an empty runbook with the spec's defaults.
func NewDoc(name, cwd string) Doc {
	return Doc{
		Name:     name,
		Cwd:      cwd,
		Defaults: Defaults{TimeoutSec: BuiltinTimeoutSec},
		Running:  Running{PauseAtDestructive: true, Audit: true},
	}
}

// Resolved is a step with every field decided (step → runbook defaults → built-in).
type Resolved struct {
	Destructive    bool
	ContinueOnFail bool
	SkipInRunAll   bool
	Cwd            string // "" = the runbook cwd
	TimeoutSec     int
	RecordOutput   bool
}

// Resolve applies the resolution order from SPEC.md §1.
func (d Doc) Resolve(s Step) Resolved {
	r := Resolved{
		Destructive:    d.Defaults.Destructive,
		ContinueOnFail: d.Defaults.ContinueOnFail,
		TimeoutSec:     d.Defaults.TimeoutSec,
		RecordOutput:   true,
		Cwd:            s.Cwd,
	}
	if s.Destructive != nil {
		r.Destructive = *s.Destructive
	}
	if s.ContinueOnFail != nil {
		r.ContinueOnFail = *s.ContinueOnFail
	}
	if s.SkipInRunAll != nil {
		r.SkipInRunAll = *s.SkipInRunAll
	}
	if s.TimeoutSec != nil {
		r.TimeoutSec = *s.TimeoutSec
	}
	if s.RecordOutput != nil {
		r.RecordOutput = *s.RecordOutput
	}
	return r
}

// StepByID finds a step.
func (d Doc) StepByID(id string) (Step, int, bool) {
	for i, s := range d.Steps {
		if s.ID == id {
			return s, i, true
		}
	}
	return Step{}, -1, false
}
