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
	svc.http = srv.Client()
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
	svc.http = srv.Client()
	svc.Create(domain.WPSite{ID: "klant", Name: "K", URL: url, Username: "mcp-bot"}, "goed")
	srv.Close()
	res, err := svc.TestConnection("klant")
	if err != nil || res.Status != ConnUnreachable {
		t.Fatalf("res %+v, err %v", res, err)
	}
}
