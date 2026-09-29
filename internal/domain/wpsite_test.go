package domain

import "testing"

func geldigeSite() WPSite {
	return WPSite{ID: "klant-a", Name: "Klant A", URL: "https://www.klant-a.nl", Username: "mcp-bot"}
}

func TestWPSiteValidateGeldig(t *testing.T) {
	if err := geldigeSite().Validate(); err != nil {
		t.Fatalf("verwacht geldig: %v", err)
	}
}

func TestWPSiteValidateFouten(t *testing.T) {
	cases := map[string]func(*WPSite){
		"id leeg":             func(s *WPSite) { s.ID = "" },
		"id hoofdletter":      func(s *WPSite) { s.ID = "Klant" },
		"id één teken":        func(s *WPSite) { s.ID = "a" },
		"id begint met -":     func(s *WPSite) { s.ID = "-klant" },
		"id te lang":          func(s *WPSite) { s.ID = "a123456789012345678901234567890123456789012345678x" },
		"naam leeg":           func(s *WPSite) { s.Name = " " },
		"user leeg":           func(s *WPSite) { s.Username = "" },
		"url leeg":            func(s *WPSite) { s.URL = "" },
		"url http extern":     func(s *WPSite) { s.URL = "http://www.klant-a.nl" },
		"url ftp":             func(s *WPSite) { s.URL = "ftp://klant-a.nl" },
		"url zonder host":     func(s *WPSite) { s.URL = "https://" },
	}
	for naam, mut := range cases {
		s := geldigeSite()
		mut(&s)
		if err := s.Validate(); err == nil {
			t.Errorf("%s: verwacht fout", naam)
		}
	}
}

func TestWPSiteAllowHTTP(t *testing.T) {
	for url, want := range map[string]bool{
		"http://localhost:8080":   true,
		"http://klant-a.test":     true,
		"https://klant-a.test":    false,
		"http://www.klant-a.nl":   false,
		"https://www.klant-a.nl":  false,
	} {
		s := geldigeSite()
		s.URL = url
		if got := s.AllowHTTP(); got != want {
			t.Errorf("%s: AllowHTTP=%v, want %v", url, got, want)
		}
		if want {
			if err := s.Validate(); err != nil {
				t.Errorf("%s: lokale http moet geldig zijn: %v", url, err)
			}
		}
	}
}

func TestSuggestWPSiteID(t *testing.T) {
	for in, want := range map[string]string{
		"web-ekcoatingsnl":  "web-ekcoatingsnl",
		"EK Coating B.V.":   "ek-coating-b-v",
		"__Kennisbank__":    "kennisbank",
		"Café Één":          "caf-n",
	} {
		if got := SuggestWPSiteID(in); got != want {
			t.Errorf("SuggestWPSiteID(%q) = %q, want %q", in, got, want)
		}
	}
	lang := SuggestWPSiteID("abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz")
	if len(lang) != 49 {
		t.Errorf("lengte %d, want 49", len(lang))
	}
}
