package notion

import "testing"

func TestHintMasksTheToken(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"ntn_1234567890abcdWXYZ", "ntn_••••••WXYZ"},
		{"short", "•••••"},
		{"", ""},
	} {
		if got := hint(c.in); got != c.want {
			t.Errorf("hint(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
