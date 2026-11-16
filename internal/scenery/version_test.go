package scenery

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.0.52", "0.0.18", 1},
		{"0.0.18", "0.0.52", -1},
		{"0.0.52", "0.0.52", 0},
		{"0.0.45-1", "0.0.44-1", 1},
		{"v1.2.3", "1.2.3", 0},
		{"1.2", "1.2.0", 0},
		{"0.0.0", "0.0.1", -1},
	}
	for _, c := range cases {
		got := compareVersions(c.a, c.b)
		if got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
