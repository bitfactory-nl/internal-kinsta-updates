package services

import (
	"strconv"
	"strings"
)

// compareVersions compares two dotted version strings PHP-style.
// Numeric segments compare numerically; if a segment is non-numeric it
// compares lexically. A missing segment counts as lower than a present one,
// except a present numeric 0 equals a missing segment ("1.2" == "1.2.0").
func compareVersions(a, b string) int {
	as := splitVersion(a)
	bs := splitVersion(b)
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var x, y string
		if i < len(as) {
			x = as[i]
		}
		if i < len(bs) {
			y = bs[i]
		}
		if c := compareSegment(x, y); c != 0 {
			return c
		}
	}
	return 0
}

func splitVersion(v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	// Treat . - _ + as separators.
	repl := strings.NewReplacer("-", ".", "_", ".", "+", ".")
	return strings.Split(repl.Replace(v), ".")
}

func compareSegment(x, y string) int {
	if x == y {
		return 0
	}
	xn, xerr := strconv.Atoi(x)
	yn, yerr := strconv.Atoi(y)
	switch {
	case xerr == nil && yerr == nil:
		if xn < yn {
			return -1
		}
		if xn > yn {
			return 1
		}
		return 0
	case xerr == nil && yerr != nil:
		// numeric segment (e.g. "0") ranks above a non-numeric/pre-release (e.g. "beta")
		if y == "" {
			if xn == 0 {
				return 0
			}
			return 1
		}
		return 1
	case xerr != nil && yerr == nil:
		if x == "" {
			if yn == 0 {
				return 0
			}
			return -1
		}
		return -1
	default:
		// Both are non-numeric
		if y == "" {
			// Non-empty non-numeric (pre-release) < missing segment
			return -1
		}
		if x == "" {
			// Missing < non-empty non-numeric
			return 1
		}
		if x < y {
			return -1
		}
		return 1
	}
}
