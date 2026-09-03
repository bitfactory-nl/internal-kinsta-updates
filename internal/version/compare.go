package version

import (
	"strconv"
	"strings"
)

// parsed is een ontlede versie: de drie nummers plus een eventueel
// pre-release-suffix ("rc1" uit "v0.3.0-rc1").
type parsed struct {
	nums [3]int
	pre  string
}

// parse ontleedt "v0.3.1", "0.3.1" of "0.3.1-rc2". Een v-prefix is optioneel.
// Alles wat niet uit precies drie niet-negatieve getallen bestaat, geldt als
// onparseerbaar.
func parse(v string) (parsed, bool) {
	s := strings.TrimPrefix(strings.TrimSpace(v), "v")
	if s == "" {
		return parsed{}, false
	}

	var p parsed
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		p.pre = s[i+1:]
		s = s[:i]
	}

	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return parsed{}, false
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return parsed{}, false
		}
		p.nums[i] = n
	}
	return p, true
}

// IsNewer meldt of candidate een hogere versie is dan current. Onparseerbare
// invoer aan welke kant dan ook geeft false: een versie die we niet begrijpen —
// waaronder "dev" als huidige versie — is nooit een reden om een update aan te
// bieden. Bij gelijke nummers telt een pre-release lager dan de bijbehorende
// release, dus v0.3.0 is nieuwer dan v0.3.0-rc1 en niet andersom.
func IsNewer(candidate, current string) bool {
	c, ok := parse(candidate)
	if !ok {
		return false
	}
	cur, ok := parse(current)
	if !ok {
		return false
	}

	for i := 0; i < 3; i++ {
		if c.nums[i] != cur.nums[i] {
			return c.nums[i] > cur.nums[i]
		}
	}
	return c.pre == "" && cur.pre != ""
}
