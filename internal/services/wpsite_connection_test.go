package services

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

func wpServer(t *testing.T, usersMe, bridge int) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pw, ok := r.BasicAuth()
		if !ok || user != "mcp-bot" || pw != "goed" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/wp-json/wp/v2/users/me":
			w.WriteHeader(usersMe)
		case "/wp-json/nb-mcp/v1/status":
			w.WriteHeader(bridge)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testVerbinding(t *testing.T, srv *httptest.Server, pw string) (ConnectionResult, domain.WPSite) {
	t.Helper()
	svc, _, _ := nieuweWPSiteService(t)
	svc.http.Transport = srv.Client().Transport
	s := domain.WPSite{ID: "klant", Name: "Klant", URL: srv.URL, Username: "mcp-bot"}
	if _, err := svc.Create(s, pw); err != nil {
		t.Fatal(err)
	}
	res, err := svc.TestConnection("klant")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := svc.find("klant")
	return res, got
}

func TestTestConnectionOK(t *testing.T) {
	res, site := testVerbinding(t, wpServer(t, 200, 200), "goed")
	if res.Status != ConnOK || !site.Bridge {
		t.Fatalf("res %+v, bridge %v", res, site.Bridge)
	}
}

func TestTestConnectionGeenBridge(t *testing.T) {
	res, site := testVerbinding(t, wpServer(t, 200, 404), "goed")
	if res.Status != ConnNoBridge || site.Bridge {
		t.Fatalf("res %+v, bridge %v", res, site.Bridge)
	}
}

func TestTestConnectionAuthFout(t *testing.T) {
	res, _ := testVerbinding(t, wpServer(t, 200, 200), "fout-wachtwoord")
	if res.Status != ConnAuthFailed {
		t.Fatalf("res %+v", res)
	}
	if strings.Contains(res.Message, "fout-wachtwoord") {
		t.Fatal("wachtwoord in melding")
	}
}

func TestTestConnectionOnbereikbaar(t *testing.T) {
	srv := wpServer(t, 200, 200)
	url := srv.URL
	svc, _, _ := nieuweWPSiteService(t)
	svc.http.Transport = srv.Client().Transport
	svc.Create(domain.WPSite{ID: "klant", Name: "K", URL: url, Username: "mcp-bot"}, "goed")
	srv.Close()
	res, err := svc.TestConnection("klant")
	if err != nil || res.Status != ConnUnreachable {
		t.Fatalf("res %+v, err %v", res, err)
	}
}

// testVerbindingNaTrue zet de bridge-vlag eerst op true en draait dan de check.
func testVerbindingNaTrue(t *testing.T, srv *httptest.Server) (ConnectionResult, domain.WPSite) {
	t.Helper()
	svc, _, _ := nieuweWPSiteService(t)
	svc.http.Transport = srv.Client().Transport
	s := domain.WPSite{ID: "klant", Name: "Klant", URL: srv.URL, Username: "mcp-bot"}
	if _, err := svc.Create(s, "goed"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.mutate("klant", func(w *domain.WPSite) { w.Bridge = true }); err != nil {
		t.Fatal(err)
	}
	res, err := svc.TestConnection("klant")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := svc.find("klant")
	return res, got
}

func TestTestConnectionBridge404ZetFalse(t *testing.T) {
	res, site := testVerbindingNaTrue(t, wpServer(t, 200, 404))
	if res.Status != ConnNoBridge || site.Bridge {
		t.Fatalf("res %+v, bridge %v", res, site.Bridge)
	}
}

func TestTestConnectionBridge500LaatVlagStaan(t *testing.T) {
	res, site := testVerbindingNaTrue(t, wpServer(t, 200, 500))
	if res.Status != ConnUnreachable || !site.Bridge {
		t.Fatalf("res %+v, bridge %v", res, site.Bridge)
	}
	if !strings.Contains(res.Message, "500") {
		t.Fatalf("melding noemt HTTP-code niet: %q", res.Message)
	}
}

func TestTestConnectionBridge403LaatVlagStaan(t *testing.T) {
	res, site := testVerbindingNaTrue(t, wpServer(t, 200, 403))
	if res.Status != ConnAuthFailed || !site.Bridge {
		t.Fatalf("res %+v, bridge %v", res, site.Bridge)
	}
	if res.Message != "nb-mcp-bridge weigert de gebruiker (rechten)" {
		t.Fatalf("melding %q", res.Message)
	}
}

func TestTestConnectionWeigertRedirectNaarHTTP(t *testing.T) {
	hits := 0
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(plain.Close)
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+r.URL.Path, http.StatusFound)
	}))
	t.Cleanup(tlsSrv.Close)

	svc, _, _ := nieuweWPSiteService(t)
	svc.http.Transport = tlsSrv.Client().Transport
	if _, err := svc.Create(domain.WPSite{ID: "klant", Name: "K", URL: tlsSrv.URL, Username: "mcp-bot"}, "goed"); err != nil {
		t.Fatal(err)
	}
	res, err := svc.TestConnection("klant")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status == ConnOK {
		t.Fatalf("redirect naar http mag geen ok opleveren: %+v", res)
	}
	if hits != 0 {
		t.Fatalf("http-server kreeg %d verzoek(en) met inloggegevens", hits)
	}
}
