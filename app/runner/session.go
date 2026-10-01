// Package runner executes runbook steps in one persistent bash session per run.
// It has no Wails dependency so it can be tested with plain `go test`.
package runner

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// DefaultKillGrace is how long SIGTERM gets before SIGKILL (SPEC.md §2).
const DefaultKillGrace = 3 * time.Second

// SessionOptions configures a shell session.
type SessionOptions struct {
	Dir       string        // starting directory; "~" is expanded
	Env       []string      // full environment; nil means os.Environ()
	KillGrace time.Duration // SIGTERM → SIGKILL delay; 0 means DefaultKillGrace
}

// Session is one long-lived `bash --noprofile --norc`. Commands arrive on its stdin; each
// step runs as `source <file> </dev/null` so a step can never read the command stream.
// stdout and stderr share one pipe. A per-session nonce marks where each step ends.
type Session struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	chunks  chan []byte // output read from the shell; closed on EOF
	done    chan struct{}
	tmpDir  string
	nonce   string
	grace   time.Duration
	seq     int
	pending []byte // output read past the last marker
	cwd     string // the shell's working directory after the last step
	started time.Time
	dropNL  bool // the last marker's trailing newline hasn't been read yet

	closeOnce sync.Once
}

// StartSession launches bash in its own process group with job control on, so each
// foreground command gets its own process group that can be killed without the shell.
func StartSession(opts SessionOptions) (*Session, error) {
	dir, err := ExpandHome(opts.Dir)
	if err != nil {
		return nil, err
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("runbook path %q is not a directory", opts.Dir)
	}
	tmpDir, err := os.MkdirTemp("", "runbook-session-")
	if err != nil {
		return nil, err
	}

	cmd := exec.Command("/bin/bash", "--noprofile", "--norc")
	cmd.Dir = dir
	cmd.Env = opts.Env
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		os.RemoveAll(tmpDir)
		return nil, err
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		os.RemoveAll(tmpDir)
		return nil, err
	}
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		os.RemoveAll(tmpDir)
		return nil, err
	}
	pw.Close() // the child holds the write end now

	s := &Session{
		cmd:     cmd,
		stdin:   stdin,
		chunks:  make(chan []byte, 64),
		done:    make(chan struct{}),
		tmpDir:  tmpDir,
		nonce:   newNonce(),
		grace:   opts.KillGrace,
		cwd:     dir,
		started: time.Now(),
	}
	if s.grace == 0 {
		s.grace = DefaultKillGrace
	}

	go func() {
		defer close(s.chunks)
		buf := make([]byte, 32*1024)
		for {
			n, err := pr.Read(buf)
			if n > 0 {
				s.chunks <- append([]byte(nil), buf[:n]...)
			}
			if err != nil {
				pr.Close()
				return
			}
		}
	}()
	go func() {
		cmd.Wait()
		close(s.done)
	}()

	if _, err := io.WriteString(stdin, "set -m\n"); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// ExecRequest is one step as the session sees it: defaults already resolved.
type ExecRequest struct {
	Command        string
	Cwd            string // "" = the session's current directory
	Timeout        time.Duration
	ContinueOnFail bool // on timeout, try to keep the shell alive
}

// ExecResult describes how a step ended.
type ExecResult struct {
	ExitCode    int
	Cwd         string // the shell's working directory afterwards (unchanged if unknown)
	Reason      EndReason
	Duration    time.Duration
	SessionDead bool // the shell is gone; no further steps can run in it
}

// Exec runs one step and streams its output to onOutput. Cancelling ctx is Stop: the
// whole session is killed.
func (s *Session) Exec(ctx context.Context, req ExecRequest, onOutput func([]byte)) ExecResult {
	start := time.Now()
	res := func(code int, reason EndReason, dead bool) ExecResult {
		return ExecResult{ExitCode: code, Cwd: s.cwd, Reason: reason, Duration: time.Since(start), SessionDead: dead}
	}

	s.seq++
	file := filepath.Join(s.tmpDir, fmt.Sprintf("step-%d.sh", s.seq))
	if err := os.WriteFile(file, []byte(req.Command+"\n"), 0o600); err != nil {
		onOutput([]byte("runbook: " + err.Error() + "\n"))
		return res(-1, ReasonError, false)
	}
	// bash prefixes its own messages (e.g. "Terminated") with the temp script's path; drop it.
	raw := onOutput
	scriptPrefix := []byte(file + ": ")
	onOutput = func(b []byte) { raw(bytes.ReplaceAll(b, scriptPrefix, nil)) }

	line, err := s.wrapper(file, req.Cwd)
	if err != nil {
		onOutput([]byte("runbook: " + err.Error() + "\n"))
		return res(-1, ReasonError, false)
	}
	if _, err := io.WriteString(s.stdin, line); err != nil {
		onOutput([]byte("runbook: shell is not running\n"))
		return res(-1, ReasonShellExited, true)
	}

	var timeout <-chan time.Time
	if req.Timeout > 0 {
		t := time.NewTimer(req.Timeout)
		defer t.Stop()
		timeout = t.C
	}

	for {
		select {
		case chunk, ok := <-s.chunks:
			if !ok {
				s.flushPending(onOutput)
				return res(s.exitStatus(), ReasonShellExited, true)
			}
			if code, found := s.consume(chunk, onOutput); found {
				return res(code, ReasonExit, false)
			}
		case <-ctx.Done():
			s.Close()
			s.drain(onOutput)
			return res(-1, ReasonStopped, true)
		case <-timeout:
			if !req.ContinueOnFail {
				s.Close()
				s.drain(onOutput)
				return res(-1, ReasonTimeout, true)
			}
			code, ok := s.killStep(ctx, onOutput)
			if !ok {
				s.Close()
				s.drain(onOutput)
				return res(-1, ReasonTimeout, true)
			}
			return res(code, ReasonTimeout, false)
		}
	}
}

// killStep terminates the step's processes but not the shell, then waits for the step's
// marker. It reports false when the shell itself is stuck (e.g. a builtin loop).
func (s *Session) killStep(ctx context.Context, onOutput func([]byte)) (int, bool) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL} {
		s.signalChildren(sig)
		deadline := time.NewTimer(s.grace)
		for waiting := true; waiting; {
			select {
			case chunk, ok := <-s.chunks:
				if !ok {
					deadline.Stop()
					return -1, false
				}
				if code, found := s.consume(chunk, onOutput); found {
					deadline.Stop()
					return code, true
				}
			case <-ctx.Done():
				deadline.Stop()
				return -1, false
			case <-deadline.C:
				waiting = false
			}
		}
	}
	return -1, false
}

// consume appends chunk to pending output, forwards everything before this step's marker
// (holding back a possible partial marker), and reports the exit code once the marker is seen.
func (s *Session) consume(chunk []byte, onOutput func([]byte)) (int, bool) {
	if s.dropNL && len(chunk) > 0 {
		if chunk[0] == '\n' {
			chunk = chunk[1:]
		}
		s.dropNL = false
	}
	s.pending = append(s.pending, chunk...)
	prefix := []byte(s.markerPrefix())
	if i := bytes.Index(s.pending, prefix); i >= 0 {
		rest := s.pending[i+len(prefix):]
		if j := bytes.IndexByte(rest, '\x1e'); j >= 0 {
			if i > 0 {
				onOutput(append([]byte(nil), s.pending[:i]...))
			}
			// Marker payload is "<exit code>:<cwd>".
			codeStr, cwd, _ := strings.Cut(string(rest[:j]), ":")
			code, err := strconv.Atoi(codeStr)
			if err != nil {
				code = -1
			}
			if cwd != "" {
				s.cwd = cwd
			}
			after := rest[j+1:]
			if len(after) > 0 && after[0] == '\n' {
				after = after[1:]
			} else if len(after) == 0 {
				s.dropNL = true
			}
			s.pending = append([]byte(nil), after...)
			return code, true
		}
		// Marker started but not finished: forward what precedes it and keep waiting.
		if i > 0 {
			onOutput(append([]byte(nil), s.pending[:i]...))
			s.pending = append([]byte(nil), s.pending[i:]...)
		}
		return 0, false
	}
	// Hold back only a tail that could be the start of a marker split across reads.
	keep := partialPrefixLen(s.pending, prefix)
	if cut := len(s.pending) - keep; cut > 0 {
		onOutput(append([]byte(nil), s.pending[:cut]...))
		s.pending = append([]byte(nil), s.pending[cut:]...)
	}
	return 0, false
}

// partialPrefixLen returns the length of the longest suffix of b that is a proper prefix
// of prefix.
func partialPrefixLen(b, prefix []byte) int {
	for k := min(len(prefix)-1, len(b)); k > 0; k-- {
		if bytes.Equal(b[len(b)-k:], prefix[:k]) {
			return k
		}
	}
	return 0
}

func (s *Session) flushPending(onOutput func([]byte)) {
	if len(s.pending) > 0 {
		onOutput(s.pending)
		s.pending = nil
	}
}

// drain forwards output still buffered after the session was killed.
func (s *Session) drain(onOutput func([]byte)) {
	timeout := time.After(time.Second)
	for {
		select {
		case chunk, ok := <-s.chunks:
			if !ok {
				s.flushPending(onOutput)
				return
			}
			s.pending = append(s.pending, chunk...)
		case <-timeout:
			s.flushPending(onOutput)
			return
		}
	}
}

func (s *Session) markerPrefix() string {
	return "\x1eRB" + s.nonce + ":"
}

// wrapper is the single line sent to bash for one step.
func (s *Session) wrapper(file, cwd string) (string, error) {
	body := "source " + shellQuote(file)
	if cwd != "" {
		dir, err := ExpandHome(cwd)
		if err != nil {
			return "", err
		}
		body = "__rb_prev=$PWD; cd -- " + shellQuote(dir) + " && " + body
	}
	line := "{ " + body + "; } </dev/null; __rb_rc=$?; "
	if cwd != "" {
		line += `cd -- "$__rb_prev"; `
	}
	line += fmt.Sprintf(`printf '\036RB%s:%%d:%%s\036\n' "$__rb_rc" "$PWD"`, s.nonce) + "\n"
	return line, nil
}

// Close kills the shell and everything it started: SIGTERM, then SIGKILL after the grace
// period. Safe to call more than once.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		s.stdin.Close()
		// Snapshot the tree first: once bash dies its children are re-parented to launchd
		// and can no longer be found by walking down from the shell.
		procs := descendants(s.cmd.Process.Pid)
		s.signalAll(procs, syscall.SIGTERM)
		deadline := time.After(s.grace)
		select {
		case <-s.done:
			// Children that ignore SIGTERM may outlive the shell; give them the same grace.
			if anyAlive(procs) {
				<-deadline
			}
		case <-deadline:
		}
		s.signalAll(append(procs, descendants(s.cmd.Process.Pid)...), syscall.SIGKILL)
		<-s.done
		os.RemoveAll(s.tmpDir)
	})
	return nil
}

func anyAlive(procs []proc) bool {
	for _, p := range procs {
		if syscall.Kill(p.pid, 0) == nil {
			return true
		}
	}
	return false
}

// Cwd is the shell's working directory as of the last completed step.
func (s *Session) Cwd() string { return s.cwd }

// Started is when the shell was launched.
func (s *Session) Started() time.Time { return s.started }

// Alive reports whether the shell process is still running.
func (s *Session) Alive() bool {
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}

func (s *Session) exitStatus() int {
	<-s.done
	if st := s.cmd.ProcessState; st != nil {
		return st.ExitCode()
	}
	return -1
}

// signalChildren signals every process group started by the shell, but not the shell.
func (s *Session) signalChildren(sig syscall.Signal) {
	shellPid := s.cmd.Process.Pid
	for _, p := range descendants(shellPid) {
		if p.pgid != shellPid && p.pgid > 1 {
			syscall.Kill(-p.pgid, sig)
		} else {
			syscall.Kill(p.pid, sig) // job control off for this one: signal the process alone
		}
	}
}

// signalAll signals the shell's process group and the given descendants and their groups.
func (s *Session) signalAll(procs []proc, sig syscall.Signal) {
	shellPid := s.cmd.Process.Pid
	for _, p := range procs {
		if p.pgid > 1 {
			syscall.Kill(-p.pgid, sig)
		}
		syscall.Kill(p.pid, sig)
	}
	syscall.Kill(-shellPid, sig)
}

type proc struct{ pid, ppid, pgid int }

// descendants lists all processes below root, via ps (no /proc on macOS).
func descendants(root int) []proc {
	out, err := exec.Command("ps", "-A", "-o", "pid=,ppid=,pgid=").Output()
	if err != nil {
		return nil
	}
	children := map[int][]proc{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) != 3 {
			continue
		}
		pid, _ := strconv.Atoi(f[0])
		ppid, _ := strconv.Atoi(f[1])
		pgid, _ := strconv.Atoi(f[2])
		children[ppid] = append(children[ppid], proc{pid, ppid, pgid})
	}
	var res []proc
	queue := []int{root}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		for _, c := range children[p] {
			res = append(res, c)
			queue = append(queue, c.pid)
		}
	}
	return res
}

func newNonce() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ExpandHome expands a leading "~" to the user's home directory.
func ExpandHome(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, strings.TrimPrefix(p, "~")), nil
	}
	if p == "" {
		return "", errors.New("empty path")
	}
	return p, nil
}
