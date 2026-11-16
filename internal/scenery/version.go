package scenery

import "strings"

// compareVersions does a lenient dotted-numeric comparison, e.g.
// "0.0.52" vs "0.0.18". It's deliberately simpler than full semver/PEP440
// parsing (golang.org/x/mod/semver demands a strict "vMAJOR.MINOR.PATCH"
// and would misrank the loose version strings actually used by the
// autoortho-scenery releases, e.g. plain "0.0.0"). Returns -1, 0, or 1.
func compareVersions(a, b string) int {
	as := strings.Split(strings.TrimPrefix(a, "v"), ".")
	bs := strings.Split(strings.TrimPrefix(b, "v"), ".")

	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var an, bn int
		if i < len(as) {
			an = leadingInt(as[i])
		}
		if i < len(bs) {
			bn = leadingInt(bs[i])
		}
		if an != bn {
			if an < bn {
				return -1
			}
			return 1
		}
	}
	return 0
}

// leadingInt parses the leading digit run of s (e.g. "52rc1" -> 52),
// returning 0 for anything with no leading digits.
func leadingInt(s string) int {
	n := 0
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		n = n*10 + int(s[i]-'0')
		i++
	}
	return n
}
