package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"runbook/notion"
	"runbook/runbook"
)

// Importing from Notion is read-only: the app reads a page and offers its steps. Nothing
// here writes to Notion.

// NotionPage is a fetched page, with its steps already parsed for the preview.
type NotionPage struct {
	PageID   string         `json:"pageId"`
	Title    string         `json:"title"`
	URL      string         `json:"url"`
	Markdown string         `json:"markdown"`
	Skipped  []string       `json:"skipped"` // Notion blocks with no place in a runbook
	Steps    []runbook.Step `json:"steps"`
	Dropped  []string       `json:"dropped"` // parts of the Markdown the parser ignored
	Suggest  string         `json:"suggest"` // a runbook name based on the page title
}

// HasNotionToken says whether a Notion integration token has been saved.
func (a *App) HasNotionToken() bool {
	_, err := notion.Token()
	return err == nil
}

// NotionTokenHint is the saved token, masked, for the Settings page ("" if none).
func (a *App) NotionTokenHint() string { return notion.Hint() }

// SaveNotionToken stores the integration token in the macOS Keychain.
func (a *App) SaveNotionToken(token string) error { return notion.SaveToken(token) }

// ForgetNotionToken removes the saved token.
func (a *App) ForgetNotionToken() error { return notion.ForgetToken() }

// FetchNotionPage reads a Notion page and previews the steps it would add. It changes nothing.
func (a *App) FetchNotionPage(link string) (NotionPage, error) {
	token, err := notion.Token()
	if err != nil {
		return NotionPage{}, errors.New("connect Notion first: paste an integration token")
	}
	id, err := notion.PageID(link)
	if err != nil {
		return NotionPage{}, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()

	page, err := (&notion.Client{Token: token}).FetchPage(ctx, id)
	if err != nil {
		return NotionPage{}, err
	}
	parsed := runbook.ParsePaste(page.Markdown)
	return NotionPage{
		PageID: page.ID, Title: page.Title, URL: page.URL, Markdown: page.Markdown,
		Skipped: page.Skipped, Steps: parsed.Steps, Dropped: parsed.Dropped,
		Suggest: runbookName(page.Title),
	}, nil
}

// CreateFromNotion makes a runbook at ref from steps the user kept in the preview, and
// records the page link so it can be reopened in Notion.
func (a *App) CreateFromNotion(ref string, steps []runbook.Step, pageID, pageURL string) (runbook.Doc, error) {
	if len(steps) == 0 {
		return runbook.Doc{}, errors.New("no steps selected")
	}
	d := runbook.NewDoc("", "~")
	d.Notion = runbook.Notion{PageID: pageID, URL: pageURL}
	d.Steps = steps
	if err := a.store.Create(ref, d); err != nil {
		return runbook.Doc{}, err
	}
	doc, err := a.store.Load(ref)
	if err != nil {
		return runbook.Doc{}, err
	}
	if doc.Running.Audit {
		a.appendAudit(ref, runbook.AuditEntry{
			Event:   "runbook.import",
			Message: fmt.Sprintf("from Notion: %s (%d steps)", pageURL, len(steps)),
		})
	}
	return doc, nil
}

// OpenInBrowser opens a link (the runbook's Notion page) in the default browser.
func (a *App) OpenInBrowser(link string) {
	wailsruntime.BrowserOpenURL(a.ctx, link)
}

// runbookName turns a page title into a usable directory name.
func runbookName(title string) string {
	out := make([]rune, 0, len(title))
	dash := false
	for _, r := range title {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			out = append(out, r)
			dash = false
		case r >= 'A' && r <= 'Z':
			out = append(out, r+32)
			dash = false
		default:
			if !dash && len(out) > 0 {
				out = append(out, '-')
				dash = true
			}
		}
	}
	name := string(out)
	for len(name) > 0 && name[len(name)-1] == '-' {
		name = name[:len(name)-1]
	}
	if len(name) > 64 {
		name = name[:64]
	}
	if name == "" {
		name = "from-notion"
	}
	return name
}
