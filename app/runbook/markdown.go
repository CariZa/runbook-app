package runbook

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Canonical form (what Serialize writes):
//
//	---
//	<front-matter>
//	---
//
//	<preamble, if any>
//
//	## 1. Title
//
//	```yaml meta          (only when a field differs from the inherited value)
//	destructive: true
//	```
//
//	```sh
//	command
//	```
//
//	## 2. A note
//
//	> note text
//
// Parse(Serialize(d)) == d, and Serialize(Parse(b)) == b whenever b is canonical.
// Non-canonical input (wrong numbering, redundant meta, extra blank lines, CRLF) is
// normalised on the first save.

var (
	fenceRe       = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})\\s*(.*?)\\s*$")
	headingNumRe  = regexp.MustCompile(`^\d+[.)]\s*`)
	commandLangs  = map[string]bool{"": true, "sh": true, "bash": true, "shell": true, "zsh": true, "console": true}
	metaInfo      = "yaml meta"
	defaultPreset = NewDoc("", "")
)

// Parse reads runbook.md. Step IDs are left empty; the store assigns them.
func Parse(src []byte) (Doc, error) {
	text := strings.ReplaceAll(string(src), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	doc := defaultPreset
	i := 0
	if len(lines) > 0 && lines[0] == "---" {
		end := -1
		for j := 1; j < len(lines); j++ {
			if lines[j] == "---" {
				end = j
				break
			}
		}
		if end < 0 {
			return Doc{}, fmt.Errorf("front-matter: missing closing ---")
		}
		raw := strings.Join(lines[1:end], "\n") + "\n"
		if end == 1 {
			raw = ""
		}
		if err := applyFrontMatter(&doc, raw); err != nil {
			return Doc{}, err
		}
		doc.FrontMatterRaw = raw
		i = end + 1
	}

	// Split the rest into preamble + step sections at "## " headings outside fences.
	var sections [][]string
	var preamble []string
	var fence string
	for ; i < len(lines); i++ {
		line := lines[i]
		if fence == "" && strings.HasPrefix(line, "## ") {
			sections = append(sections, []string{line})
			continue
		}
		fence = trackFence(fence, line)
		if len(sections) == 0 {
			preamble = append(preamble, line)
		} else {
			sections[len(sections)-1] = append(sections[len(sections)-1], line)
		}
	}
	doc.Preamble = strings.Join(trimBlank(preamble), "\n")

	doc.Steps = nil
	for _, sec := range sections {
		st, err := parseStep(sec)
		if err != nil {
			return Doc{}, err
		}
		doc.Steps = append(doc.Steps, st)
	}
	return doc, nil
}

// trackFence returns the open fence marker after line ("" when outside a fence).
func trackFence(open, line string) string {
	m := fenceRe.FindStringSubmatch(line)
	if m == nil {
		return open
	}
	if open == "" {
		return m[1]
	}
	if m[1][0] == open[0] && len(m[1]) >= len(open) && m[2] == "" {
		return ""
	}
	return open
}

type block struct {
	kind  string // "fence" | "quote" | "text"
	info  string // fence info string
	body  []string
	lines []string // raw lines, for Extra
}

func splitBlocks(lines []string) []block {
	var out []block
	for i := 0; i < len(lines); {
		line := lines[i]
		switch {
		case strings.TrimSpace(line) == "":
			i++
		case fenceRe.MatchString(line):
			m := fenceRe.FindStringSubmatch(line)
			b := block{kind: "fence", info: m[2], lines: []string{line}}
			i++
			for ; i < len(lines); i++ {
				b.lines = append(b.lines, lines[i])
				if trackFence(m[1], lines[i]) == "" {
					i++
					break
				}
				b.body = append(b.body, lines[i])
			}
			out = append(out, b)
		case strings.HasPrefix(line, ">"):
			b := block{kind: "quote"}
			for ; i < len(lines) && strings.HasPrefix(lines[i], ">"); i++ {
				b.lines = append(b.lines, lines[i])
				b.body = append(b.body, strings.TrimPrefix(strings.TrimPrefix(lines[i], ">"), " "))
			}
			out = append(out, b)
		default:
			b := block{kind: "text"}
			for ; i < len(lines) && strings.TrimSpace(lines[i]) != "" && !fenceRe.MatchString(lines[i]); i++ {
				b.lines = append(b.lines, lines[i])
			}
			out = append(out, b)
		}
	}
	return out
}

func parseStep(sec []string) (Step, error) {
	title := strings.TrimSpace(strings.TrimPrefix(sec[0], "## "))
	st := Step{Title: headingNumRe.ReplaceAllString(title, ""), Kind: "command"}

	blocks := splitBlocks(sec[1:])
	hasCommand := false
	for _, b := range blocks {
		if b.kind == "fence" && commandLangs[b.info] {
			hasCommand = true
			break
		}
	}

	var extra []string
	metaDone, cmdDone, noteDone := false, false, false
	for _, b := range blocks {
		switch {
		case b.kind == "fence" && b.info == metaInfo && !metaDone && !cmdDone:
			if err := applyMeta(&st, strings.Join(b.body, "\n")); err != nil {
				return Step{}, fmt.Errorf("step %q: %w", st.Title, err)
			}
			metaDone = true
		case b.kind == "fence" && commandLangs[b.info] && !cmdDone:
			st.Command = strings.Join(b.body, "\n")
			cmdDone = true
		case b.kind == "quote" && !hasCommand && !noteDone:
			st.Kind = "note"
			st.Note = strings.Join(b.body, "\n")
			noteDone = true
		default:
			extra = append(extra, strings.Join(b.lines, "\n"))
		}
	}
	st.Extra = strings.Join(extra, "\n\n")
	return st, nil
}

func trimBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// --- front-matter ---

type frontMatter struct {
	Name     string         `yaml:"name"`
	Cwd      string         `yaml:"cwd"`
	Version  int            `yaml:"version"`
	Parent   string         `yaml:"parent"`
	Defaults map[string]any `yaml:"defaults"`
	Running  map[string]any `yaml:"running"`
	Notion   map[string]any `yaml:"notion"`
}

func applyFrontMatter(doc *Doc, raw string) error {
	var fm frontMatter
	if err := yaml.Unmarshal([]byte(raw), &fm); err != nil {
		return fmt.Errorf("front-matter: %w", err)
	}
	doc.Name, doc.Cwd, doc.Version, doc.Parent = fm.Name, fm.Cwd, fm.Version, fm.Parent
	var err error
	if doc.Defaults.Destructive, err = boolField(fm.Defaults, "destructive", false); err != nil {
		return err
	}
	if doc.Defaults.ContinueOnFail, err = boolField(fm.Defaults, "continueOnFail", false); err != nil {
		return err
	}
	if doc.Defaults.TimeoutSec, err = timeoutField(fm.Defaults, BuiltinTimeoutSec); err != nil {
		return err
	}
	if doc.Running.PauseAtDestructive, err = boolField(fm.Running, "pauseAtDestructive", true); err != nil {
		return err
	}
	if doc.Running.Audit, err = boolField(fm.Running, "audit", true); err != nil {
		return err
	}
	doc.Notion = Notion{PageID: stringField(fm.Notion, "pageId"), URL: stringField(fm.Notion, "url")}
	return nil
}

func stringField(m map[string]any, key string) string {
	if v, ok := m[key]; ok && v != nil {
		return fmt.Sprint(v)
	}
	return ""
}

func boolField(m map[string]any, key string, def bool) (bool, error) {
	v, ok := m[key]
	if !ok || v == nil {
		return def, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("%s: want true or false, got %v", key, v)
	}
	return b, nil
}

// timeoutField reads timeoutSec: missing → def, null → 0 (no timeout), number → number.
func timeoutField(m map[string]any, def int) (int, error) {
	v, ok := m["timeoutSec"]
	if !ok {
		return def, nil
	}
	if v == nil {
		return 0, nil
	}
	n, ok := v.(int)
	if !ok || n < 0 {
		return 0, fmt.Errorf("timeoutSec: want a whole number of seconds or null, got %v", v)
	}
	return n, nil
}

// fmFields is the comparable subset of Doc that lives in the front-matter.
type fmFields struct {
	Name, Cwd, Parent string
	Version           int
	Defaults          Defaults
	Running           Running
	Notion            Notion
}

func frontMatterFields(d Doc) fmFields {
	return fmFields{
		Name: d.Name, Cwd: d.Cwd, Parent: d.Parent, Version: d.Version,
		Defaults: d.Defaults, Running: d.Running, Notion: d.Notion,
	}
}

func canonicalFrontMatter(d Doc) string {
	var b strings.Builder
	fmt.Fprintf(&b, "name: %s\n", scalar(d.Name))
	fmt.Fprintf(&b, "cwd: %s\n", scalar(d.Cwd))
	if d.Version != 0 {
		fmt.Fprintf(&b, "version: %d\n", d.Version)
	}
	if d.Parent != "" {
		fmt.Fprintf(&b, "parent: %s\n", scalar(d.Parent))
	}
	b.WriteString("defaults:\n")
	fmt.Fprintf(&b, "  destructive: %t\n", d.Defaults.Destructive)
	fmt.Fprintf(&b, "  continueOnFail: %t\n", d.Defaults.ContinueOnFail)
	fmt.Fprintf(&b, "  timeoutSec: %s\n", timeoutScalar(d.Defaults.TimeoutSec))
	b.WriteString("running:\n")
	fmt.Fprintf(&b, "  pauseAtDestructive: %t\n", d.Running.PauseAtDestructive)
	fmt.Fprintf(&b, "  audit: %t\n", d.Running.Audit)
	if d.Notion.PageID != "" {
		b.WriteString("notion:\n")
		fmt.Fprintf(&b, "  pageId: %s\n", scalar(d.Notion.PageID))
		if d.Notion.URL != "" {
			fmt.Fprintf(&b, "  url: %s\n", scalar(d.Notion.URL))
		}
	}
	return b.String()
}

func scalar(s string) string {
	out, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Sprintf("%q", s)
	}
	return strings.TrimSuffix(string(out), "\n")
}

func timeoutScalar(n int) string {
	if n == 0 {
		return "null"
	}
	return fmt.Sprint(n)
}

// --- step meta ---

func applyMeta(st *Step, raw string) error {
	var m map[string]any
	if err := yaml.Unmarshal([]byte(raw), &m); err != nil {
		return fmt.Errorf("meta: %w", err)
	}
	for key, dst := range map[string]**bool{
		"destructive":    &st.Destructive,
		"continueOnFail": &st.ContinueOnFail,
		"skipInRunAll":   &st.SkipInRunAll,
		"recordOutput":   &st.RecordOutput,
	} {
		if _, ok := m[key]; !ok {
			continue
		}
		b, err := boolField(m, key, false)
		if err != nil {
			return err
		}
		*dst = &b
	}
	if _, ok := m["timeoutSec"]; ok {
		n, err := timeoutField(m, 0)
		if err != nil {
			return err
		}
		st.TimeoutSec = &n
	}
	if v, ok := m["cwd"]; ok && v != nil {
		s := fmt.Sprint(v)
		if s != "inherit" {
			st.Cwd = s
		}
	}
	return nil
}

// metaYAML lists only fields that differ from what the step would inherit.
func metaYAML(d Doc, st Step) string {
	var b strings.Builder
	if st.Destructive != nil && *st.Destructive != d.Defaults.Destructive {
		fmt.Fprintf(&b, "destructive: %t\n", *st.Destructive)
	}
	if st.ContinueOnFail != nil && *st.ContinueOnFail != d.Defaults.ContinueOnFail {
		fmt.Fprintf(&b, "continueOnFail: %t\n", *st.ContinueOnFail)
	}
	if st.SkipInRunAll != nil && *st.SkipInRunAll {
		b.WriteString("skipInRunAll: true\n")
	}
	if st.Cwd != "" {
		fmt.Fprintf(&b, "cwd: %s\n", scalar(st.Cwd))
	}
	if st.TimeoutSec != nil && *st.TimeoutSec != d.Defaults.TimeoutSec {
		fmt.Fprintf(&b, "timeoutSec: %s\n", timeoutScalar(*st.TimeoutSec))
	}
	if st.RecordOutput != nil && !*st.RecordOutput {
		b.WriteString("recordOutput: false\n")
	}
	return b.String()
}

// --- serialise ---

// Serialize writes canonical runbook.md.
func Serialize(d Doc) []byte {
	var parts []string

	fm := d.FrontMatterRaw
	if !sameFrontMatter(d) {
		fm = canonicalFrontMatter(d)
	}
	parts = append(parts, "---\n"+fm+"---\n")

	if d.Preamble != "" {
		parts = append(parts, d.Preamble+"\n")
	}
	for i, st := range d.Steps {
		parts = append(parts, serializeStep(d, i+1, st))
	}
	return []byte(strings.Join(parts, "\n"))
}

// sameFrontMatter reports whether FrontMatterRaw still describes d's runbook-level fields.
func sameFrontMatter(d Doc) bool {
	if d.FrontMatterRaw == "" {
		return false
	}
	parsed := defaultPreset
	if err := applyFrontMatter(&parsed, d.FrontMatterRaw); err != nil {
		return false
	}
	return frontMatterFields(parsed) == frontMatterFields(d)
}

func serializeStep(d Doc, n int, st Step) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %d. %s\n", n, strings.TrimSpace(st.Title))
	if meta := metaYAML(d, st); meta != "" {
		b.WriteString("\n```" + metaInfo + "\n" + meta + "```\n")
	}
	if st.Kind == "note" {
		b.WriteString("\n")
		for _, line := range strings.Split(st.Note, "\n") {
			if line == "" {
				b.WriteString(">\n")
			} else {
				b.WriteString("> " + line + "\n")
			}
		}
	} else {
		fence := fenceFor(st.Command)
		b.WriteString("\n" + fence + "sh\n")
		if st.Command != "" {
			b.WriteString(st.Command + "\n")
		}
		b.WriteString(fence + "\n")
	}
	if st.Extra != "" {
		b.WriteString("\n" + st.Extra + "\n")
	}
	return b.String()
}

// fenceFor picks a backtick fence longer than any backtick run that starts a line in s.
func fenceFor(s string) string {
	longest := 0
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimLeft(line, " ")
		n := len(t) - len(strings.TrimLeft(t, "`"))
		if n > longest {
			longest = n
		}
	}
	return strings.Repeat("`", max(3, longest+1))
}
