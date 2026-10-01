package runbook

import (
	"regexp"
	"strings"
)

// Arguments are named blanks in a command, written {{name}}. The app fills them from
// values the user types; a value is shell-quoted as it goes in, so it is always one word
// and can never become part of the command's syntax.
var argRe = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_-]+)\s*\}\}`)

// safeArgRe matches values that need no quoting (kept bare so commands stay readable).
var safeArgRe = regexp.MustCompile(`^[A-Za-z0-9_./:=@%+,-]+$`)

// ArgNames lists the distinct argument names in command, in order of first use.
func ArgNames(command string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range argRe.FindAllStringSubmatch(command, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

// Substitute fills every {{name}} in command with its shell-quoted value. It returns the
// names that have no (or an empty) value; the command is only safe to run when that is empty.
func Substitute(command string, values map[string]string) (string, []string) {
	var missing []string
	seen := map[string]bool{}
	out := argRe.ReplaceAllStringFunc(command, func(m string) string {
		name := argRe.FindStringSubmatch(m)[1]
		v := strings.TrimSpace(values[name])
		if v == "" {
			if !seen[name] {
				seen[name] = true
				missing = append(missing, name)
			}
			return m
		}
		return QuoteArg(v)
	})
	return out, missing
}

// QuoteArg makes v a single shell word: bare if it is plainly safe, otherwise single-quoted.
func QuoteArg(v string) string {
	if safeArgRe.MatchString(v) {
		return v
	}
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}
