//go:build linux

package lifecycle

import (
	"testing"
)

func TestUnescapeMountField(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`/mnt/plain`, `/mnt/plain`},
		{`/mnt/with\040space`, `/mnt/with space`},
		{`/mnt/with\040two\040spaces`, `/mnt/with two spaces`},
		{`/mnt/tab\011here`, "/mnt/tab\there"},
		{`/mnt/back\134slash`, `/mnt/back\slash`},
	}
	for _, c := range cases {
		if got := unescapeMountField(c.in); got != c.want {
			t.Errorf("unescapeMountField(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsMounted_NotMounted(t *testing.T) {
	mounted, err := IsMounted(t.TempDir())
	if err != nil {
		t.Fatalf("IsMounted: %v", err)
	}
	if mounted {
		t.Error("a freshly created temp dir should not report as mounted")
	}
}
