package services

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rdm/sites-tool/internal/domain"
)

func TestUpdatePluginsBodyEnResultaat(t *testing.T) {
	b := nieuweBridgeServer(t, 200, `{"results":[
	 {"plugin":"akismet/akismet.php","success":true,"from":"5.3","to":"5.4"},
	 {"plugin":"kapot/kapot.php","success":false,"from":"1.0","to":"1.0","error":"Download failed."}]}`)
	svc := bridgeSite(t, b, "goed", nil)
	res, err := svc.UpdatePlugins("klant", []string{"akismet/akismet.php", "kapot/kapot.php"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if b.lastPath != "/wp-json/nb-mcp/v1/updates/plugins" || b.lastBody != `{"plugins":["akismet/akismet.php","kapot/kapot.php"]}` {
		t.Fatalf("verzoek %q %q", b.lastPath, b.lastBody)
	}
	want := []RunItem{
		{Name: "akismet/akismet.php", From: "5.3", To: "5.4", Success: true},
		{Name: "kapot/kapot.php", From: "1.0", To: "1.0", Error: "Download failed."},
	}
	if len(res.Items) != 2 || res.Items[0] != want[0] || res.Items[1] != want[1] {
		t.Fatalf("items %+v", res.Items)
	}
}

func TestUpdatePluginsAllGebruiktBeschikbareLijst(t *testing.T) {
	calls := 0
	b := nieuweBridgeServer(t, 200, updatesJSON)
	svc := bridgeSite(t, b, "goed", nil)
	// Eerste call (GET /updates) geeft de lijst; daarna antwoorden we met results.
	orig := b.srv.Config.Handler
	b.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method == http.MethodPost {
			b.respBody = `{"results":[{"plugin":"akismet/akismet.php","success":true,"from":"5.3","to":"5.4"}]}`
		}
		orig.ServeHTTP(w, r)
	})
	res, err := svc.UpdatePlugins("klant", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || b.lastBody != `{"plugins":["akismet/akismet.php"]}` || len(res.Items) != 1 {
		t.Fatalf("calls %d body %q items %+v", calls, b.lastBody, res.Items)
	}
}

func TestUpdatePluginsLeegZonderAll(t *testing.T) {
	b := nieuweBridgeServer(t, 200, `{}`)
	svc := bridgeSite(t, b, "goed", nil)
	if _, err := svc.UpdatePlugins("klant", nil, false); err == nil {
		t.Fatal("verwacht fout bij lege selectie")
	}
}

func TestUpdateCoreBodyEnResultaat(t *testing.T) {
	b := nieuweBridgeServer(t, 200, `{"success":true,"from":"7.1.1","to":"7.1.2"}`)
	svc := bridgeSite(t, b, "goed", nil)
	res, err := svc.UpdateCore("klant", true)
	if err != nil {
		t.Fatal(err)
	}
	if b.lastPath != "/wp-json/nb-mcp/v1/updates/core" || b.lastBody != `{"allow_major":true}` {
		t.Fatalf("verzoek %q %q", b.lastPath, b.lastBody)
	}
	if len(res.Items) != 1 || res.Items[0] != (RunItem{Name: "WordPress", From: "7.1.1", To: "7.1.2", Success: true}) {
		t.Fatalf("items %+v", res.Items)
	}
}

func TestUpdateCoreMajorGeblokkeerd409(t *testing.T) {
	b := nieuweBridgeServer(t, 409, `{"code":"nb_mcp_major_blocked","message":"Major update 8.0 blocked; pass allow_major."}`)
	svc := bridgeSite(t, b, "goed", nil)
	_, err := svc.UpdateCore("klant", false)
	if err == nil || !strings.Contains(err.Error(), "allow_major") {
		t.Fatalf("fout %v", err)
	}
}

func TestUpdateTranslations(t *testing.T) {
	b := nieuweBridgeServer(t, 200, `{"success":false,"count":2,"failed":1,"errors":["Vertaling mislukt."]}`)
	svc := bridgeSite(t, b, "goed", nil)
	res, err := svc.UpdateTranslations("klant")
	if err != nil {
		t.Fatal(err)
	}
	if b.lastPath != "/wp-json/nb-mcp/v1/updates/translations" {
		t.Fatalf("pad %q", b.lastPath)
	}
	if len(res.Items) != 1 || res.Items[0].Success || res.Items[0].Name != "Vertalingen (2 bijgewerkt, 1 mislukt)" || res.Items[0].Error != "Vertaling mislukt." {
		t.Fatalf("items %+v", res.Items)
	}
}

func TestUpdateReadOnlyWeigertZonderNetwerk(t *testing.T) {
	b := nieuweBridgeServer(t, 200, `{}`)
	svc := bridgeSite(t, b, "goed", func(s *domain.WPSite) { s.ReadOnly = true })
	for naam, run := range map[string]func() error{
		"plugins": func() error { _, err := svc.UpdatePlugins("klant", []string{"a/a.php"}, false); return err },
		"core":    func() error { _, err := svc.UpdateCore("klant", false); return err },
		"vert":    func() error { _, err := svc.UpdateTranslations("klant"); return err },
	} {
		if err := run(); err == nil || err.Error() != "deze site staat op alleen-lezen" {
			t.Errorf("%s: fout %v", naam, err)
		}
	}
	if b.lastPath != "" {
		t.Fatal("er mocht geen netwerkcall zijn")
	}
}

func TestUpdateDubbeleRunGeweigerd(t *testing.T) {
	b := nieuweBridgeServer(t, 200, `{}`)
	svc := bridgeSite(t, b, "goed", nil)
	if !svc.claimRun("klant") {
		t.Fatal("eerste claim moet lukken")
	}
	defer svc.releaseRun("klant")
	if _, err := svc.UpdateCore("klant", false); err == nil || err.Error() != "er loopt al een update voor deze site" {
		t.Fatalf("fout %v", err)
	}
}

type fakeDepot struct {
	prURL      string
	prErr      error
	prVersion  string
	prPath     string
	actionsURL string
}

func (f *fakeDepot) OpenCorePR(_ context.Context, path, version string) (string, error) {
	f.prPath, f.prVersion = path, version
	return f.prURL, f.prErr
}
func (f *fakeDepot) PluginWorkflowURL(context.Context, string) (string, error) {
	return f.actionsURL, nil
}

func depotSite(t *testing.T, b *bridgeServer, d *fakeDepot, depot bool) *WPSiteService {
	t.Helper()
	svc, _, projDir := nieuweWPSiteService(t)
	svc.depot = d
	svc.http.Transport = b.srv.Client().Transport
	if _, err := svc.Create(domain.WPSite{ID: "klant", Name: "K", URL: b.srv.URL, Username: "mcp-bot", Bridge: true, Depot: depot, ProjectPath: projDir}, "goed"); err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestUpdateCoreDepotMaaktPR(t *testing.T) {
	b := nieuweBridgeServer(t, 200, `{"success":true,"from":"7.1.1","to":"7.2.1"}`)
	d := &fakeDepot{prURL: "https://github.com/x/y/pull/1"}
	svc := depotSite(t, b, d, true)
	res, err := svc.UpdateCore("klant", false)
	if err != nil || res.PullRequest != "https://github.com/x/y/pull/1" || d.prVersion != "7.2.1" || res.Warning != "" {
		t.Fatalf("res %+v err %v depot %+v", res, err, d)
	}
}

func TestUpdateCoreDepotPRFoutWordtWaarschuwing(t *testing.T) {
	b := nieuweBridgeServer(t, 200, `{"success":true,"from":"7.1.1","to":"7.2.1"}`)
	svc := depotSite(t, b, &fakeDepot{prErr: errors.New("github token niet geconfigureerd")}, true)
	res, err := svc.UpdateCore("klant", false)
	if err != nil || res.PullRequest != "" || !strings.Contains(res.Warning, "github token niet geconfigureerd") {
		t.Fatalf("res %+v err %v", res, err)
	}
}

func TestUpdateCoreGeenDepotGeenPR(t *testing.T) {
	b := nieuweBridgeServer(t, 200, `{"success":true,"from":"7.1.1","to":"7.2.1"}`)
	d := &fakeDepot{prURL: "x"}
	svc := depotSite(t, b, d, false)
	res, _ := svc.UpdateCore("klant", false)
	if res.PullRequest != "" || d.prVersion != "" {
		t.Fatalf("geen PR verwacht: %+v", res)
	}
}

func TestUpdateCoreMisluktGeenPR(t *testing.T) {
	b := nieuweBridgeServer(t, 200, `{"success":false,"from":"7.1.1","to":"7.1.1","error":"No core update is available."}`)
	d := &fakeDepot{prURL: "x"}
	svc := depotSite(t, b, d, true)
	res, _ := svc.UpdateCore("klant", false)
	if res.PullRequest != "" || d.prVersion != "" {
		t.Fatalf("geen PR verwacht: %+v", res)
	}
}

func TestUpdatePluginsDepotGeeftActionsURL(t *testing.T) {
	b := nieuweBridgeServer(t, 200, `{"results":[{"plugin":"a/a.php","success":true,"from":"1","to":"2"}]}`)
	d := &fakeDepot{actionsURL: "https://github.com/x/y/actions/workflows/plugin-update.yml"}
	svc := depotSite(t, b, d, true)
	res, err := svc.UpdatePlugins("klant", []string{"a/a.php"}, false)
	if err != nil || res.ActionsURL != d.actionsURL {
		t.Fatalf("res %+v err %v", res, err)
	}
}

// trageBridge antwoordt niet voordat de test klaar is, zodat een kleine
// bridgeRunTimeout een echte client-timeout geeft. De cleanup (die vóór
// srv.Close draait) laat de handler los, zodat Close niet hoeft te wachten.
func trageBridge(t *testing.T) *bridgeServer {
	t.Helper()
	b := nieuweBridgeServer(t, 200, `{}`)
	stop := make(chan struct{})
	b.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-stop:
		case <-time.After(2 * time.Second):
		}
	})
	oud := bridgeRunTimeout
	bridgeRunTimeout = 50 * time.Millisecond
	t.Cleanup(func() {
		close(stop)
		bridgeRunTimeout = oud
	})
	return b
}

func TestUpdateCoreTimeoutApartHerkend(t *testing.T) {
	b := trageBridge(t)
	svc := bridgeSite(t, b, "goed", nil)
	_, err := svc.UpdateCore("klant", false)
	if err == nil || !strings.Contains(err.Error(), "geen antwoord binnen de wachttijd") {
		t.Fatalf("fout %v", err)
	}
	if strings.Contains(err.Error(), "Composer-PR") {
		t.Fatalf("geen depotsite, toch PR-hint: %v", err)
	}
}

func TestUpdateCoreTimeoutDepotNoemtPR(t *testing.T) {
	b := trageBridge(t)
	svc := depotSite(t, b, &fakeDepot{}, true)
	_, err := svc.UpdateCore("klant", false)
	if err == nil || !strings.Contains(err.Error(), "geen antwoord binnen de wachttijd") || !strings.Contains(err.Error(), "maak de Composer-PR zo nodig handmatig") {
		t.Fatalf("fout %v", err)
	}
}
