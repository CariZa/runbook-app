package main

import (
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"runbook/runbook"
)

// ExportMarkdown renders the export sheet's Markdown (6a). Outputs come from the on-disk
// cache, so they are the redacted copies, never the raw live output.
func (a *App) ExportMarkdown(name string, doc runbook.Doc, opts runbook.ExportOptions) string {
	var outputs map[string]runbook.Output
	if opts.Outputs {
		outputs = a.store.Outputs(name)
	}
	return runbook.Export(doc, outputs, opts)
}

// SaveExport asks where to save and writes the Markdown there. It returns the path, or ""
// if the user cancelled.
func (a *App) SaveExport(name, markdown string, audit bool) (string, error) {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:            "Save runbook export",
		DefaultDirectory: filepath.Join(a.store.Root, name),
		DefaultFilename:  "export.md",
		Filters:          []runtime.FileFilter{{DisplayName: "Markdown (*.md)", Pattern: "*.md"}},
	})
	if err != nil || path == "" {
		return "", err
	}
	if err := os.WriteFile(path, []byte(markdown), 0o644); err != nil {
		return "", err
	}
	if audit {
		a.appendAudit(name, runbook.AuditEntry{Event: "runbook.export", Message: "saved " + path})
	}
	return path, nil
}

// ParsePaste previews "Paste steps…" (SPEC.md §4) without changing anything.
func (a *App) ParsePaste(text string) runbook.PasteResult {
	return runbook.ParsePaste(text)
}
