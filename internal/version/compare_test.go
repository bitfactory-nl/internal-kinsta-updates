package version

import "testing"

func TestIsNewer(t *testing.T) {
	cases := []struct {
		naam      string
		candidate string
		current   string
		want      bool
	}{
		{"patch hoger", "v0.2.10", "v0.2.9", true},
		{"minor hoger", "v0.3.0", "v0.2.99", true},
		{"major hoger", "v1.0.0", "v0.99.99", true},
		{"gelijk", "v0.2.9", "v0.2.9", false},
		{"ouder", "v0.2.8", "v0.2.9", false},
		{"zonder v-prefix werkt ook", "0.2.10", "0.2.9", true},
		{"prefix gemengd", "v0.2.10", "0.2.9", true},
		{"pre-release is ouder dan release", "v0.3.0-rc1", "v0.3.0", false},
		{"release is nieuwer dan pre-release", "v0.3.0", "v0.3.0-rc1", true},
		{"pre-release van hogere versie is nieuwer", "v0.4.0-rc1", "v0.3.0", true},
		{"dev als huidige versie updatet nooit", "v9.9.9", "dev", false},
		{"onparseerbare kandidaat", "latest", "v0.2.9", false},
		{"te weinig componenten", "v0.3", "v0.2.9", false},
		{"lege kandidaat", "", "v0.2.9", false},
		{"letters in een component", "v0.a.1", "v0.2.9", false},
	}

	for _, c := range cases {
		t.Run(c.naam, func(t *testing.T) {
			if got := IsNewer(c.candidate, c.current); got != c.want {
				t.Errorf("IsNewer(%q, %q) = %v, wil %v", c.candidate, c.current, got, c.want)
			}
		})
	}
}

func TestIsDev(t *testing.T) {
	orig := Version
	t.Cleanup(func() { Version = orig })

	Version = "dev"
	if !IsDev() {
		t.Error("IsDev() = false voor \"dev\", wil true")
	}
	Version = ""
	if !IsDev() {
		t.Error("IsDev() = false voor een lege versie, wil true")
	}
	Version = "v0.2.9"
	if IsDev() {
		t.Error("IsDev() = true voor v0.2.9, wil false")
	}
}
