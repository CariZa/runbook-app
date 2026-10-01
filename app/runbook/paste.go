package runbook

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// PasteResult is what "Paste steps…" found, for the preview before anything is added.
type PasteResult struct {
	Steps   []Step   `json:"steps"`
	Dropped []string `json:"dropped"` // human-readable summary of what was ignored
}

var (
	pasteHeadingRe = regexp.MustCompile(`^#{2,3}\s+(.*\S)\s*$`)
	pasteTitleRe   = regexp.MustCompile(`^#\s+\S`)
	listItemRe     = regexp.MustCompile(`^\s{0,3}\d+[.)]\s+(.*\S)\s*$`)
	stepPrefixRe   = regexp.MustCompile(`(?i)^(?:step\s+)?\d+\s*[.):]\s*`)
	promptRe       = regexp.MustCompile(`^\s*[$%]\s+(.*\S)\s*$`)
	quotePromptRe  = regexp.MustCompile(`^\s*>\s+(.*\S)\s*$`)
	quoteRe        = regexp.MustCompile(`^>\s?(.*)$`)
)

// ParsePaste turns pasted text into steps using the rules in SPEC.md §4, in order:
//
//  1. a "## " or "### " heading starts a step (title minus "N." / "N)" / "Step N:");
//  2. failing that (no headings at all), a numbered list item starts a step;
//  3. a fenced block is that step's command; a second fence under the same step is
//     example output and is dropped. A "yaml meta" fence carries step settings;
//  4. lines that look like a shell prompt ("$ ", "% ") become command steps, one per line,
//     prompt stripped. "> " counts as a prompt only when the text has no headings — in
//     heading-structured Markdown (like our own export) it is a note;
//  5. prose that stands alone under a step makes it a note step; other prose is dropped.
func ParsePaste(text string) PasteResult {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	hasHeadings := false
	var fence string
	for _, l := range lines {
		if fence == "" && pasteHeadingRe.MatchString(l) {
			hasHeadings = true
			break
		}
		fence = trackFence(fence, l)
	}

	p := &pasteParser{dropped: map[string]int{}}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if m := fenceRe.FindStringSubmatch(line); m != nil {
			indent := len(line) - len(strings.TrimLeft(line, " "))
			var body []string
			for i++; i < len(lines); i++ {
				if trackFence(m[1], lines[i]) == "" {
					break
				}
				body = append(body, dedent(lines[i], indent))
			}
			p.fence(m[2], strings.Join(body, "\n"))
			continue
		}
		switch {
		case strings.TrimSpace(line) == "":
			p.blank()
		case pasteHeadingRe.MatchString(line):
			p.start(cleanTitle(pasteHeadingRe.FindStringSubmatch(line)[1]))
		case pasteTitleRe.MatchString(line):
			p.drop("title line")
		case !hasHeadings && listItemRe.MatchString(line):
			p.start(cleanTitle(listItemRe.FindStringSubmatch(line)[1]))
		case promptRe.MatchString(line):
			p.prompt(promptRe.FindStringSubmatch(line)[1])
		case !hasHeadings && quotePromptRe.MatchString(line):
			p.prompt(quotePromptRe.FindStringSubmatch(line)[1])
		case quoteRe.MatchString(line):
			p.quote(quoteRe.FindStringSubmatch(line)[1])
		default:
			p.prose(line)
		}
	}
	p.finish()
	return PasteResult{Steps: p.steps, Dropped: p.summary()}
}

type pasteStep struct {
	Step
	hasCommand bool
	fromPrompt bool
	text       []string // note text (blockquote or standalone prose)
	proseOnly  bool     // text came from plain prose, not a blockquote
}

type pasteParser struct {
	cur     *pasteStep
	steps   []Step
	dropped map[string]int
	inText  bool // inside a run of prose lines (counted once per paragraph)
}

func (p *pasteParser) start(title string) {
	p.finish()
	p.cur = &pasteStep{Step: Step{Title: title, Kind: "command"}}
}

func (p *pasteParser) finish() {
	p.inText = false
	c := p.cur
	p.cur = nil
	if c == nil {
		return
	}
	switch {
	case c.hasCommand:
		c.Kind = "command"
	case len(c.text) > 0:
		c.Kind, c.Note = "note", strings.TrimSpace(strings.Join(c.text, "\n"))
	default:
		c.Kind = "note" // a heading or list item with nothing under it: a manual step
	}
	if c.Title == "" {
		c.Title = titleFrom(c.Command + c.Note)
	}
	p.steps = append(p.steps, c.Step)
}

func (p *pasteParser) fence(info, body string) {
	p.inText = false
	if info == metaInfo {
		if p.cur != nil && !p.cur.hasCommand {
			if err := applyMeta(&p.cur.Step, body); err != nil {
				p.drop("unreadable settings block")
			}
			return
		}
		p.drop("settings block without a step")
		return
	}
	if p.cur != nil && p.cur.hasCommand {
		p.drop("example output block")
		return
	}
	if p.cur == nil {
		p.cur = &pasteStep{Step: Step{Kind: "command"}}
	}
	if len(p.cur.text) > 0 {
		p.drop("paragraph")
		p.cur.text = nil
	}
	p.cur.Command, p.cur.hasCommand = body, true
}

func (p *pasteParser) prompt(cmd string) {
	p.inText = false
	// Under a heading or list item that has no command yet, the prompt line is its command.
	if p.cur != nil && !p.cur.hasCommand && !p.cur.fromPrompt {
		if len(p.cur.text) > 0 {
			p.drop("paragraph")
			p.cur.text = nil
		}
		p.cur.Command, p.cur.hasCommand, p.cur.fromPrompt = cmd, true, true
		return
	}
	p.finish()
	p.cur = &pasteStep{Step: Step{Kind: "command", Command: cmd}, hasCommand: true, fromPrompt: true}
}

func (p *pasteParser) quote(text string) {
	p.inText = false
	if p.cur == nil || p.cur.hasCommand {
		p.drop("quoted line")
		return
	}
	if p.cur.proseOnly {
		p.cur.text, p.cur.proseOnly = nil, false
	}
	p.cur.text = append(p.cur.text, text)
}

func (p *pasteParser) prose(line string) {
	switch {
	case p.cur == nil:
		if !p.inText {
			p.drop("paragraph")
		}
	case p.cur.fromPrompt:
		p.drop("output line")
		return
	case p.cur.hasCommand:
		if !p.inText {
			p.drop("paragraph")
		}
	default:
		if len(p.cur.text) == 0 {
			p.cur.proseOnly = true
		}
		if p.cur.proseOnly {
			p.cur.text = append(p.cur.text, strings.TrimSpace(line))
		}
	}
	p.inText = true
}

func (p *pasteParser) blank() {
	p.inText = false
	if p.cur != nil && len(p.cur.text) > 0 {
		p.cur.text = append(p.cur.text, "")
	}
}

func (p *pasteParser) drop(what string) { p.dropped[what]++ }

func (p *pasteParser) summary() []string {
	var out []string
	for what, n := range p.dropped {
		if n == 1 {
			out = append(out, "1 "+what)
		} else {
			out = append(out, fmt.Sprintf("%d %ss", n, what))
		}
	}
	sort.Strings(out)
	return out
}

func cleanTitle(s string) string {
	return strings.TrimSpace(stepPrefixRe.ReplaceAllString(strings.TrimSpace(s), ""))
}

func titleFrom(body string) string {
	first := strings.TrimSpace(strings.SplitN(strings.TrimSpace(body), "\n", 2)[0])
	if r := []rune(first); len(r) > 60 {
		return string(r[:57]) + "…"
	}
	if first == "" {
		return "Untitled step"
	}
	return first
}

func dedent(line string, n int) string {
	for i := 0; i < n && strings.HasPrefix(line, " "); i++ {
		line = line[1:]
	}
	return line
}
