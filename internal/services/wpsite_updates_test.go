package services

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

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
		{"409 file-mods", 409, `{"code":"nb_mcp_file_mods_disabled","message":"File modifications are disabled on this site (DISALLOW_FILE_MODS is true)."}`, "goed", nil, "DISALLOW_FILE_MODS"},
		{"403 met body", 403, `{"code":"nb_mcp_forbidden","message":"This action requires one of the following capabilities: update_plugins."}`, "goed", nil, "capabilities: update_plugins"},
		{"403 zonder body", 403, ``, "goed", nil, "WordPress weigerde het verzoek (HTTP 403)"},
		{"404", 404, `{"code":"rest_no_route","message":"No route was found matching the URL and request method."}`, "goed", nil, "nb-mcp-bridge ontbreekt op deze site"},
		{"500 met JSON", 500, `{"code":"nb_mcp_filesystem","message":"Could not access filesystem."}`, "goed", nil, "Could not access filesystem."},
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

func TestFleetUpdatesMixEnProjectID(t *testing.T) {
	ok := nieuweBridgeServer(t, 200, updatesJSON)
	kapot := nieuweBridgeServer(t, 500, ``)
	svc, _, projDir := nieuweWPSiteService(t)
	// Beide TLS-testservers gebruiken hetzelfde testcertificaat; één Transport volstaat.
	svc.http.Transport = ok.srv.Client().Transport
	if _, err := svc.Create(domain.WPSite{ID: "een", Name: "Een", URL: ok.srv.URL, Username: "mcp-bot", Bridge: true, ProjectPath: projDir}, "goed"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(domain.WPSite{ID: "twee", Name: "Twee", URL: kapot.srv.URL, Username: "mcp-bot", Bridge: true}, "goed"); err != nil {
		t.Fatal(err)
	}
	got, err := svc.FleetUpdates(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len %d", len(got))
	}
	if got[0].Site.Site.ID != "een" || got[0].Updates == nil || got[0].Error != "" || got[0].ProjectID != "p1" {
		t.Fatalf("een: %+v", got[0])
	}
	if got[1].Site.Site.ID != "twee" || got[1].Updates != nil || !strings.Contains(got[1].Error, "HTTP 500") || got[1].ProjectID != "" {
		t.Fatalf("twee: %+v", got[1])
	}
}

func TestFleetUpdatesConcurrencyMaxVier(t *testing.T) {
	var mu sync.Mutex
	bezig, max := 0, 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		bezig++
		if bezig > max {
			max = bezig
		}
		mu.Unlock()
		time.Sleep(50 * time.Millisecond)
		mu.Lock()
		bezig--
		mu.Unlock()
		w.Write([]byte(updatesJSON))
	}))
	t.Cleanup(srv.Close)
	svc, _, _ := nieuweWPSiteService(t)
	svc.http.Transport = srv.Client().Transport
	for i := 0; i < 9; i++ {
		id := fmt.Sprintf("site-%d", i)
		if _, err := svc.Create(domain.WPSite{ID: id, Name: id, URL: srv.URL, Username: "mcp-bot", Bridge: true}, "goed"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.FleetUpdates(false); err != nil {
		t.Fatal(err)
	}
	if max > fleetConcurrency || max < 2 {
		t.Fatalf("max gelijktijdig %d, want 2..%d", max, fleetConcurrency)
	}
}

func TestBridgeFoutKaptOpRuneGrens(t *testing.T) {
	lang := strings.Repeat("€", 400)
	err := bridgeFout(500, []byte(`{"code":"x","message":"`+lang+`"}`))
	if err == nil {
		t.Fatal("verwacht fout")
	}
	msg := err.Error()
	if !utf8.ValidString(msg) || utf8.RuneCountInString(msg) != 300 {
		t.Fatalf("geldig %v, runes %d", utf8.ValidString(msg), utf8.RuneCountInString(msg))
	}
}
