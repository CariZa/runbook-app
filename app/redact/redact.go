// Package redact removes secrets from text before it is written to disk (runs.log and the
// output cache). It is best-effort: it catches known values and known formats, and makes
// no attempt to guess at "random-looking" strings, which would also eat pod names,
// digests and commit hashes.
package redact

import (
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Mask replaces every redacted value.
const Mask = "[REDACTED]"

const minLiteral = 8 // shorter env values are too likely to be ordinary words

var (
	sensitiveName = regexp.MustCompile(`(?i)(TOKEN|SECRET|PASSW(OR)?D|PASSPHRASE|CREDENTIAL|PRIVATE|APIKEY|API_KEY|ACCESS_KEY|(^|_)KEY$|(^|_)AUTH$|_PAT$)`)
	notSecretName = regexp.MustCompile(`(?i)(_SOCK|_PATH|_FILE|_DIR|_URL|_HOST|_ID)$`)

	// assignment finds NAME=value in commands (export FOO=bar, FOO=bar cmd).
	assignment = regexp.MustCompile(`(?:^|[\s;&|])(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)=("[^"]*"|'[^']*'|[^\s;&|]+)`)

	// Patterns whose whole match is the secret.
	whole = []*regexp.Regexp{
		regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`),                                             // AWS access key id
		regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}\b`),                                            // GitHub token
		regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{22,}\b`),                                          // GitHub fine-grained PAT
		regexp.MustCompile(`\bglpat-[A-Za-z0-9_\-]{20,}\b`),                                             // GitLab PAT
		regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}\b`),                                          // Slack token
		regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`),              // JWT
		regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`), // PEM
	}

	// Patterns where group 1 is kept and group 2 is the secret.
	keyed = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(\bbearer\s+)([A-Za-z0-9._~+/=-]{8,})`),
		regexp.MustCompile(`(?i)(\b(?:password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key|client[_-]?secret|aws_secret_access_key)["']?\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s"',;&]+)`),
		regexp.MustCompile(`("private_key(?:_id)?"\s*:\s*)("(?:[^"\\]|\\.)*")`), // GCP service-account JSON
		regexp.MustCompile(`(://[^/\s:@]+:)([^@\s/]+)(@)`),                      // user:password@ in URLs
	}

	// Kubernetes Secret data as JSON: "data": { ... }.
	k8sJSONData = regexp.MustCompile(`("(?:data|stringData)"\s*:\s*\{)([^}]*)(\})`)
	jsonValue   = regexp.MustCompile(`(:\s*)"[^"]*"`)
	yamlDataKey = regexp.MustCompile(`^(\s*)(data|stringData):\s*$`)
	yamlKV      = regexp.MustCompile(`^(\s+[^\s:#]+:\s*)(\S.*)$`)
)

// SensitiveName reports whether a variable or argument name looks like it holds a secret.
func SensitiveName(name string) bool {
	return sensitiveName.MatchString(name) && !notSecretName.MatchString(name)
}

// Redactor redacts known values plus known formats. Safe for concurrent use.
type Redactor struct {
	mu       sync.RWMutex
	literals []string // longest first
}

// New seeds the redactor with values of sensitive-looking variables in env ("K=V").
func New(env []string) *Redactor {
	r := &Redactor{}
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if ok {
			r.addIfSensitive(k, v)
		}
	}
	return r
}

// LearnFromCommand adds values assigned to sensitive-looking names in a command,
// e.g. `export API_TOKEN=abc123...`, so later output containing them is redacted.
func (r *Redactor) LearnFromCommand(cmd string) {
	for _, m := range assignment.FindAllStringSubmatch(cmd, -1) {
		r.addIfSensitive(m[1], strings.Trim(m[2], `"'`))
	}
}

func (r *Redactor) addIfSensitive(name, value string) {
	if len(value) < minLiteral || strings.HasPrefix(value, "/") || strings.HasPrefix(value, "$") {
		return
	}
	if !sensitiveName.MatchString(name) || notSecretName.MatchString(name) {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, l := range r.literals {
		if l == value {
			return
		}
	}
	r.literals = append(r.literals, value)
	sort.Slice(r.literals, func(i, j int) bool { return len(r.literals[i]) > len(r.literals[j]) })
}

// Redact returns s with secrets replaced by Mask.
func (r *Redactor) Redact(s string) string {
	if s == "" {
		return s
	}
	r.mu.RLock()
	for _, l := range r.literals {
		s = strings.ReplaceAll(s, l, Mask)
	}
	r.mu.RUnlock()

	// NAME=value where NAME looks sensitive, even if we never saw it exported.
	s = assignment.ReplaceAllStringFunc(s, func(m string) string {
		g := assignment.FindStringSubmatch(m)
		if !sensitiveName.MatchString(g[1]) || notSecretName.MatchString(g[1]) || len(strings.Trim(g[2], `"'`)) < minLiteral ||
			strings.HasPrefix(strings.Trim(g[2], `"'`), "$") {
			return m
		}
		return strings.TrimSuffix(m, g[2]) + Mask
	})
	for _, re := range whole {
		s = re.ReplaceAllString(s, Mask)
	}
	for _, re := range keyed {
		s = re.ReplaceAllStringFunc(s, func(m string) string {
			g := re.FindStringSubmatch(m)
			if strings.HasPrefix(strings.Trim(g[2], `"'`), "$") {
				return m // a variable reference, not a value
			}
			out := g[1] + Mask
			if len(g) > 3 {
				out += g[3]
			}
			return out
		})
	}
	s = k8sJSONData.ReplaceAllStringFunc(s, func(m string) string {
		g := k8sJSONData.FindStringSubmatch(m)
		return g[1] + jsonValue.ReplaceAllString(g[2], `$1"`+Mask+`"`) + g[3]
	})
	return redactYAMLSecretData(s)
}

// redactYAMLSecretData masks values nested under `data:` / `stringData:` (kubectl -o yaml).
func redactYAMLSecretData(s string) string {
	if !strings.Contains(s, "ata:") {
		return s
	}
	lines := strings.Split(s, "\n")
	indent := -1
	for i, line := range lines {
		if m := yamlDataKey.FindStringSubmatch(line); m != nil {
			indent = len(m[1])
			continue
		}
		if indent < 0 {
			continue
		}
		lead := len(line) - len(strings.TrimLeft(line, " "))
		if strings.TrimSpace(line) == "" || lead <= indent {
			indent = -1
			continue
		}
		if m := yamlKV.FindStringSubmatch(line); m != nil {
			lines[i] = m[1] + Mask
		}
	}
	return strings.Join(lines, "\n")
}
