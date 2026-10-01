package runbook

import (
	"fmt"
	"strings"
	"time"
)

// ExportOptions are the toggles on the export sheet (screen 6a).
type ExportOptions struct {
	StepIDs    []string `json:"stepIds"`    // nil or empty = every step
	Outputs    bool     `json:"outputs"`    // include each step's last (redacted) output
	Settings   bool     `json:"settings"`   // include a yaml meta block with non-default settings
	Timestamps bool     `json:"timestamps"` // add when each output was captured
}

// Export writes the shareable Markdown dialect (SPEC.md §4):
//
//	# name
//	Path: `cwd`
//
//	## 1. Title
//
//	```yaml meta            (Settings: fields that differ from the built-in defaults)
//	```sh                   the command
//	```                     (Outputs: plain block with output and an "exit N · 1.2s" trailer)
//
// Notes export as blockquotes. ParsePaste reads it back.
func Export(d Doc, outputs map[string]Output, o ExportOptions) string {
	want := map[string]bool{}
	for _, id := range o.StepIDs {
		want[id] = true
	}
	var parts []string
	parts = append(parts, fmt.Sprintf("# %s\nPath: `%s`\n", d.Name, d.Cwd))

	n := 0
	for _, st := range d.Steps {
		if len(want) > 0 && !want[st.ID] {
			continue
		}
		n++
		var b strings.Builder
		fmt.Fprintf(&b, "## %d. %s\n", n, strings.TrimSpace(st.Title))
		if st.Kind == "note" {
			b.WriteString("\n" + quote(st.Note))
			parts = append(parts, b.String())
			continue
		}
		if o.Settings {
			if meta := portableMeta(d, st); meta != "" {
				b.WriteString("\n```" + metaInfo + "\n" + meta + "```\n")
			}
		}
		fence := fenceFor(st.Command)
		b.WriteString("\n" + fence + "sh\n")
		if st.Command != "" {
			b.WriteString(st.Command + "\n")
		}
		b.WriteString(fence + "\n")
		if out, ok := outputs[st.ID]; ok && o.Outputs {
			b.WriteString("\n" + outputBlock(out, o.Timestamps))
		}
		parts = append(parts, b.String())
	}
	return strings.Join(parts, "\n")
}

func quote(text string) string {
	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			b.WriteString(">\n")
		} else {
			b.WriteString("> " + line + "\n")
		}
	}
	return b.String()
}

// portableMeta lists resolved settings that differ from the built-in defaults, so the
// export means the same thing when pasted into a runbook with other defaults.
func portableMeta(d Doc, st Step) string {
	r := d.Resolve(st)
	var b strings.Builder
	if r.Destructive {
		b.WriteString("destructive: true\n")
	}
	if r.ContinueOnFail {
		b.WriteString("continueOnFail: true\n")
	}
	if r.SkipInRunAll {
		b.WriteString("skipInRunAll: true\n")
	}
	if r.Cwd != "" {
		fmt.Fprintf(&b, "cwd: %s\n", scalar(r.Cwd))
	}
	if r.TimeoutSec != BuiltinTimeoutSec {
		fmt.Fprintf(&b, "timeoutSec: %s\n", timeoutScalar(r.TimeoutSec))
	}
	if !r.RecordOutput {
		b.WriteString("recordOutput: false\n")
	}
	return b.String()
}

func outputBlock(o Output, timestamps bool) string {
	body := strings.TrimRight(o.Output, "\n")
	trailer := fmt.Sprintf("exit %d · %.1fs", o.ExitCode, float64(o.DurationMs)/1000)
	switch o.Reason {
	case "timeout":
		trailer = fmt.Sprintf("timed out · %.1fs", float64(o.DurationMs)/1000)
	case "stopped":
		trailer = fmt.Sprintf("stopped · %.1fs", float64(o.DurationMs)/1000)
	}
	if timestamps && !o.StartedAt.IsZero() {
		trailer += " · " + o.StartedAt.Local().Format(time.DateTime)
	}
	content := trailer
	if body != "" {
		content = body + "\n" + trailer
	}
	fence := fenceFor(content)
	return fence + "\n" + content + "\n" + fence + "\n"
}
