# Runbook

A small macOS desktop app for ops runbooks: write them as ordered shell steps, run them one
at a time or top to bottom, keep what happened, and share them as plain Markdown.

It was built to replace the usual pattern of keeping a runbook in a doc and copy-pasting
commands into a terminal one by one — losing, every time, the record of what was actually run
and what it printed.

## What it does

- **Steps, not scripts.** A runbook is a list of named steps. Each is either a shell command
  or a note. Run one with ▶, or run the whole runbook from the top.
- **A real shell.** Single ▶ clicks share one persistent `bash` session, so `cd`, `export` and
  shell functions carry over between steps the way they do in a terminal. **Run all** starts a
  fresh one. The session picks up your login environment, so a GUI-launched app still finds
  the tools on your `PATH`.
- **Stops on failure.** A run halts at the first failing step unless that step is marked
  *continue on failure*. Steps can also be marked *destructive* (confirm before running),
  *skip in Run all*, or given a timeout.
- **Blanks.** A command can contain `{{disk}}`-style placeholders, filled in per run and
  remembered per step, so one step can be re-run against different values. Values are shell-quoted.
- **It remembers.** Output is kept per step, and every run is appended to an audit log.
- **Markdown in, Markdown out.** Paste a doc, a numbered list or a terminal session and it
  becomes steps. Export back to Markdown, with or without output.
- **Notion import** (optional, read-only) — paste a page link and turn its headings and code
  blocks into steps.

## Install

No notarised release yet. Build it yourself, or unzip a build someone sent you.

The app is signed ad-hoc, not with an Apple Developer ID. A build you downloaded will be
quarantined by Gatekeeper and open with *"runbook.app is damaged and can't be opened"* — which
means unsigned, not broken. Clear it once:

```bash
xattr -dr com.apple.quarantine /Applications/runbook.app
```

### Build from source

Needs Go 1.23+, Node 22+ (Vite 8) and the [Wails v2](https://wails.io) CLI (`go install
github.com/wailsapp/wails/v2/cmd/wails@latest`).

```bash
cd app && wails build
```

The bundle lands in `app/build/bin/runbook.app`.

## Where your data lives

Everything is files under `~/runbooks`, one directory per runbook:

```
~/runbooks/<name>/runbook.md            the runbook — the source of truth
~/runbooks/<name>/runs.log              audit log (JSONL), last 30 days
~/runbooks/<name>/versions/vN.md        snapshots you chose to save
~/runbooks/<name>/.runbook/state.json   step ids and last output per step
```

`runbook.md` is ordinary Markdown and round-trips byte-for-byte, so the library is yours: edit
it in another editor, keep it in git, back it up, or walk away from this app entirely. A
generated `.gitignore` keeps `runs.log` and the output cache out of any git repo you make.
Deleting a runbook moves its directory to the Trash.

## Please read this part

**The app runs shell commands as you.** That is the whole point of it, but it means a runbook
is executable code with your credentials, and a runbook someone sends you deserves the same
reading as a script someone sends you. There is no sandbox and no privilege boundary.

**The audit log records commands and their output.** If a command prints a secret, that secret
reaches `runs.log` and the output cache. Secrets are masked on a **best-effort** basis: values
from your environment, recognisable token shapes (AWS, GitHub, Slack, JWTs, PEM blocks),
`NAME=value` assignments with sensitive-looking names, and Kubernetes Secret payloads. It is a
set of patterns, not a guarantee — assume something will eventually slip through. Steps that
print things you'd rather not keep can be switched to **don't record output**, and the audit
log can be turned off per runbook.

**Everything stays local.** The app makes no network calls of its own. The one exception is
Notion import, and only while you use it: it reads the page you link to and writes nothing
back. The integration token is kept in your login Keychain rather than in a config file, so it
can't end up in a runbook, an export or a backup of `~/runbooks` — but anything running as your
user can still read it, exactly as with any file in your home directory.

## Built with Claude

This app was written with [Claude Code](https://claude.com/claude-code), from a spec and
wireframes in [`handoff/`](handoff/). It isn't AI-assisted autocomplete over a human-written
codebase — the implementation is Claude's, reviewed, tested and directed by a human, over a
series of sessions.

[`handoff/DECISIONS.md`](handoff/DECISIONS.md) is the running record of every decision that
isn't in the original spec, and why. It is the best place to start if you want to know how
something behaves or why it works the way it does.

The risky parts — shell session handling, Markdown round-tripping, secret redaction, the
audit log — are plain Go packages with no Wails dependency and their own tests (`cd app && go
test ./...`).

## Licence

Apache-2.0. See [LICENSE](LICENSE).
