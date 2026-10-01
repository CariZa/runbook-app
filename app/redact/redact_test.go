package redact

import (
	"strings"
	"testing"
)

func TestRedactsKnownFormats(t *testing.T) {
	r := New(nil)
	for name, tc := range map[string]struct{ in, keep, gone string }{
		"aws key id":      {"key AKIAIOSFODNN7EXAMPLE found", "key ", "AKIAIOSFODNN7EXAMPLE"},
		"aws secret":      {"aws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "aws_secret_access_key = ", "wJalrXUtnFEMI"},
		"github":          {"token ghp_abcdefghijklmnopqrstuvwxyz0123456789AB", "token ", "ghp_abcdef"},
		"github pat":      {"github_pat_11ABCDEFG0123456789_abcdefghijklmnop", "", "github_pat_11"},
		"gitlab":          {"glpat-abcdefghij0123456789", "", "glpat-"},
		"slack":           {"SLACK=xoxb-123456789012-abcdefghij", "", "xoxb-1234"},
		"jwt":             {"id_token: eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U", "id_token: ", "eyJhbGci"},
		"bearer":          {"Authorization: Bearer abc.def-ghi_jkl", "Authorization: Bearer ", "abc.def"},
		"password pair":   {`psql "password=hunter2hunter2 host=db"`, "password=", "hunter2"},
		"json secret":     {`{"client_secret": "s3cr3t-value-123"}`, `"client_secret": `, "s3cr3t"},
		"url creds":       {"postgres://admin:sup3rs3cret@db:5432/app", "postgres://admin:" + Mask + "@db:5432/app", "sup3rs3cret"},
		"gcp sa json":     {`"private_key_id": "abc123def456", "private_key": "-----BEGIN PRIVATE KEY-----\nMIIE\n-----END PRIVATE KEY-----\n"`, `"private_key": `, "MIIE"},
		"pem":             {"-----BEGIN RSA PRIVATE KEY-----\nMIIEow\nIBAAK\n-----END RSA PRIVATE KEY-----\ntail", "tail", "MIIEow"},
		"k8s secret json": {`{"data": {"password": "aHVudGVyMg==", "user": "YWRtaW4="}, "kind": "Secret"}`, `"kind": "Secret"`, "aHVudGVyMg"},
	} {
		t.Run(name, func(t *testing.T) {
			out := r.Redact(tc.in)
			if strings.Contains(out, tc.gone) {
				t.Errorf("secret survived: %q", out)
			}
			if !strings.Contains(out, tc.keep) || !strings.Contains(out, Mask) {
				t.Errorf("context lost or no mask: %q", out)
			}
		})
	}
}

func TestRedactsK8sSecretYAML(t *testing.T) {
	in := "apiVersion: v1\ndata:\n  password: aHVudGVyMg==\n  username: YWRtaW4=\nkind: Secret\nmetadata:\n  name: db-creds\n"
	out := New(nil).Redact(in)
	for _, gone := range []string{"aHVudGVyMg==", "YWRtaW4="} {
		if strings.Contains(out, gone) {
			t.Errorf("%s survived:\n%s", gone, out)
		}
	}
	for _, keep := range []string{"  password: " + Mask, "kind: Secret", "name: db-creds"} {
		if !strings.Contains(out, keep) {
			t.Errorf("missing %q:\n%s", keep, out)
		}
	}
}

func TestRedactsEnvValuesAndLearnedExports(t *testing.T) {
	r := New([]string{
		"GITHUB_TOKEN=tok_1234567890abcdef",
		"DB_PASSWORD=correct-horse-battery",
		"SSH_AUTH_SOCK=/private/tmp/agent.sock", // path: not a secret
		"PATH=/usr/bin:/bin",
		"HOME=/Users/me",
		"SHORT_KEY=abc", // too short to redact safely
		"KUBE_CLUSTER_ID=example-cluster-1234",
	})
	r.LearnFromCommand(`export VAULT_TOKEN="hvs.CAESIJlongvaultvalue"; API_KEY=k-9876543210 ./deploy.sh`)

	out := r.Redact("tok_1234567890abcdef correct-horse-battery hvs.CAESIJlongvaultvalue k-9876543210 " +
		"/private/tmp/agent.sock example-cluster-1234 abc")
	for _, gone := range []string{"tok_1234567890abcdef", "correct-horse-battery", "hvs.CAESIJlongvaultvalue", "k-9876543210"} {
		if strings.Contains(out, gone) {
			t.Errorf("%s survived: %q", gone, out)
		}
	}
	for _, keep := range []string{"/private/tmp/agent.sock", "example-cluster-1234", " abc"} {
		if !strings.Contains(out, keep) {
			t.Errorf("%s wrongly redacted: %q", keep, out)
		}
	}
}

func TestLeavesOrdinaryOpsOutputAlone(t *testing.T) {
	in := strings.Join([]string{
		"example-api-7f9c8d6b5-x2k4q   0/1   CrashLoopBackOff   4   12m",
		"image: registry.example.com/example-app@sha256:3b4c5d6e7f8091a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d5e6f708192a",
		"request-id 550e8400-e29b-41d4-a716-446655440000",
		"commit 9fceb02d0ae598e95dc970b74767f19372d61af8",
		"ERROR pool timeout after 30s",
		"deployment.apps/example-api restarted",
		"token bucket refilled", // "token" in prose, no assignment
	}, "\n")
	if out := New(nil).Redact(in); out != in {
		t.Errorf("ordinary output changed:\n%s", out)
	}
}

func TestRedactsSensitiveAssignmentsNeverSeenBefore(t *testing.T) {
	out := New(nil).Redact("export API_TOKEN=supersecret-value-123\nDB_PASSWORD='hunter2hunter2' psql\nexport GREETING=hello-there-friend\nTOKEN_FILE=/etc/token\nAPI_KEY=$FROM_VAULT")
	for _, gone := range []string{"supersecret-value-123", "hunter2hunter2"} {
		if strings.Contains(out, gone) {
			t.Errorf("%s survived:\n%s", gone, out)
		}
	}
	for _, keep := range []string{"export API_TOKEN=" + Mask, "DB_PASSWORD=" + Mask + " psql", "GREETING=hello-there-friend", "TOKEN_FILE=/etc/token", "API_KEY=$FROM_VAULT"} {
		if !strings.Contains(out, keep) {
			t.Errorf("missing %q:\n%s", keep, out)
		}
	}
}

func TestSensitiveName(t *testing.T) {
	for name, want := range map[string]bool{
		"api_token": true, "DB_PASSWORD": true, "vault-secret": true, "client_secret": true, "key": true,
		"disk": false, "zone": false, "project": false, "token_file": false, "ssh_auth_sock": false,
	} {
		if got := SensitiveName(name); got != want {
			t.Errorf("SensitiveName(%q) = %v, want %v", name, got, want)
		}
	}
}
