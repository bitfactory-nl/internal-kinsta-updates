package services

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

const updatesJSON = `{
 "checked_at":"2026-09-30T08:00:00Z",
 "core":[{"current":"7.1.3","version":"7.1.2","type":"minor","locale":"nl_NL","response":"upgrade"}],
 "plugins":[{"plugin":"akismet/akismet.php","name":"Akismet","current_version":"5.3","new_version":"5.4","package_available":true}],
 "themes":[{"stylesheet":"twentytwentyfive","name":"TT5","current_version":"1.0","new_version":"1.1","package_available":true}],
 "translations":{"count":3}
}`

// bridgeServer beantwoordt alleen goed geauthenticeerde verzoeken en onthoudt
// het laatste pad (met query) en de body.
type bridgeServer struct {
	srv      *httptest.Server
	lastPath string
	lastBody string
	status   int
	respBody string
}

func nieuweBridgeServer(t *testing.T, status int, body string) *bridgeServer {
	t.Helper()
	b := &bridgeServer{status: status, respBody: body}
	b.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pw, ok := r.BasicAuth()
		if !ok || user != "mcp-bot" || pw != "goed" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		b.lastPath = r.URL.RequestURI()
		buf := new(strings.Builder)
		_, _ = io.Copy(buf, r.Body)
		b.lastBody = buf.String()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(b.status)
		_, _ = w.Write([]byte(b.respBody))
	}))
	t.Cleanup(b.srv.Close)
	return b
}

// bridgeSite maakt een site met bridge aan die naar de testserver wijst.
func bridgeSite(t *testing.T, b *bridgeServer, pw string, mut func(*domain.WPSite)) *WPSiteService {
	t.Helper()
	svc, _, _ := nieuweWPSiteService(t)
	svc.http.Transport = b.srv.Client().Transport
	s := domain.WPSite{ID: "klant", Name: "Klant", URL: b.srv.URL + "/", Username: "mcp-bot", Bridge: true}
	if mut != nil {
		mut(&s)
	}
	if _, err := svc.Create(s, pw); err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestAvailableUpdatesMapping(t *testing.T) {
	b := nieuweBridgeServer(t, 200, updatesJSON)
	svc := bridgeSite(t, b, "goed", nil)
	got, err := svc.AvailableUpdates("klant", false)
	if err != nil {
		t.Fatal(err)
	}
	if b.lastPath != "/wp-json/nb-mcp/v1/updates" {
		t.Fatalf("pad %q", b.lastPath)
	}
	if got.SiteID != "klant" || got.CheckedAt != "2026-09-30T08:00:00Z" || got.Translations != 3 {
		t.Fatalf("kop: %+v", got)
	}
	if len(got.Core) != 1 || got.Core[0].Version != "7.1.2" || got.Core[0].Type != "minor" {
		t.Fatalf("core: %+v", got.Core)
	}
	if len(got.Plugins) != 1 || got.Plugins[0] != (ItemUpdate{ID: "akismet/akismet.php", Name: "Akismet", CurrentVersion: "5.3", NewVersion: "5.4"}) {
		t.Fatalf("plugins: %+v", got.Plugins)
	}
	if len(got.Themes) != 1 || got.Themes[0].ID != "twentytwentyfive" || got.Themes[0].NewVersion != "1.1" {
		t.Fatalf("themes: %+v", got.Themes)
	}
}

func TestAvailableUpdatesRefreshParam(t *testing.T) {
	b := nieuweBridgeServer(t, 200, updatesJSON)
	svc := bridgeSite(t, b, "goed", nil)
	if _, err := svc.AvailableUpdates("klant", true); err != nil {
		t.Fatal(err)
	}
	if b.lastPath != "/wp-json/nb-mcp/v1/updates?refresh=1" {
		t.Fatalf("pad %q", b.lastPath)
	}
}

func TestAvailableUpdatesFouten(t *testing.T) {
	cases := []struct {
		naam   string
		status int
		body   string
		pw     string
		mut    func(*domain.WPSite)
		want   string
	}{
		{"401", 200, updatesJSON, "fout-geheim", nil, "gebruiker of Application Password klopt niet"},
		{"403 met body", 403, `{"code":"nb_mcp_file_mods_disabled","message":"File modifications are disabled on this site (DISALLOW_FILE_MODS is true)."}`, "goed", nil, "DISALLOW_FILE_MODS"},
		{"403 zonder body", 403, ``, "goed", nil, "WordPress weigerde het verzoek (HTTP 403)"},
		{"404", 404, `{"code":"rest_no_route"}`, "goed", nil, "nb-mcp-bridge ontbreekt op deze site"},
		{"500", 500, ``, "goed", nil, "site niet bereikbaar (HTTP 500)"},
		{"bridge uit", 200, updatesJSON, "goed", func(s *domain.WPSite) { s.Bridge = false }, "nb-mcp-bridge is niet actief"},
	}
	for _, c := range cases {
		b := nieuweBridgeServer(t, c.status, c.body)
		svc := bridgeSite(t, b, c.pw, c.mut)
		_, err := svc.AvailableUpdates("klant", false)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: fout %v, want %q", c.naam, err, c.want)
		}
		if err != nil && strings.Contains(err.Error(), c.pw) {
			t.Errorf("%s: wachtwoord in fout", c.naam)
		}
	}
}

func TestAvailableUpdatesZonderWachtwoord(t *testing.T) {
	b := nieuweBridgeServer(t, 200, updatesJSON)
	svc, sec, _ := nieuweWPSiteService(t)
	svc.http.Transport = b.srv.Client().Transport
	if _, err := svc.Create(domain.WPSite{ID: "klant", Name: "K", URL: b.srv.URL, Username: "mcp-bot", Bridge: true}, "goed"); err != nil {
		t.Fatal(err)
	}
	delete(sec.m, "wp:klant")
	_, err := svc.AvailableUpdates("klant", false)
	if err == nil || err.Error() != "geen Application Password opgeslagen" {
		t.Fatalf("fout %v", err)
	}
	if b.lastPath != "" {
		t.Fatal("er mocht geen netwerkcall zijn")
	}
}
