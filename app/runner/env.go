package runner

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

const envMarker = "\n__RUNBOOK_ENV__\n"

// LoginEnv returns the environment of the user's interactive login shell, so a GUI-launched
// app still sees PATH additions (Homebrew, kubectl, gcloud) from their dotfiles. Dotfile
// noise and a non-zero exit are tolerated; on failure it falls back to os.Environ().
func LoginEnv(timeout time.Duration) []string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, shell, "-ilc", `printf '`+strings.ReplaceAll(envMarker, "\n", `\n`)+`'; /usr/bin/env -0`)
	cmd.Stdin = nil
	out, _ := cmd.Output()
	i := bytes.LastIndex(out, []byte(envMarker))
	if i < 0 {
		return withRunbookEnv(os.Environ())
	}
	var env []string
	for _, kv := range bytes.Split(out[i+len(envMarker):], []byte{0}) {
		if s := string(kv); strings.Contains(s, "=") {
			env = append(env, s)
		}
	}
	if len(env) == 0 {
		return withRunbookEnv(os.Environ())
	}
	return withRunbookEnv(env)
}

// withRunbookEnv stops pagers and colour-hungry tools from waiting on a terminal.
func withRunbookEnv(env []string) []string {
	set := map[string]string{"PAGER": "cat", "GIT_PAGER": "cat", "TERM": "dumb"}
	out := env[:0:0]
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if _, override := set[k]; !override {
			out = append(out, kv)
		}
	}
	for k, v := range set {
		out = append(out, k+"="+v)
	}
	return out
}
