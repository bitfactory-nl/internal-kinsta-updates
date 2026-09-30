package services

import (
	"errors"
	"testing"
)

const statusJSON = `{"bridge_version":"1.4.0","wp_version":"7.1.2","php_version":"8.3.0"}`

func TestDepotCoreStatusNietDepot(t *testing.T) {
	b := nieuweBridgeServer(t, 200, statusJSON)
	svc := depotSite(t, b, &fakeDepot{locked: "7.1"}, false)
	want := "alleen beschikbaar voor de depotsite met een gekoppelde projectmap"
	if _, err := svc.DepotCoreStatus("klant"); err == nil || err.Error() != want {
		t.Fatalf("status: fout %v", err)
	}
	if _, err := svc.OpenDepotCorePR("klant"); err == nil || err.Error() != want {
		t.Fatalf("pr: fout %v", err)
	}
	if b.lastPath != "" {
		t.Fatal("er mocht geen netwerkcall zijn")
	}
}

func TestDepotCoreStatusZonderDepotService(t *testing.T) {
	b := nieuweBridgeServer(t, 200, statusJSON)
	svc := depotSite(t, b, &fakeDepot{}, true)
	svc.depot = nil
	if _, err := svc.DepotCoreStatus("klant"); err == nil || err.Error() != "alleen beschikbaar voor de depotsite met een gekoppelde projectmap" {
		t.Fatalf("fout %v", err)
	}
}

func TestDepotCoreStatusNeedsPR(t *testing.T) {
	b := nieuweBridgeServer(t, 200, statusJSON)
	svc := depotSite(t, b, &fakeDepot{locked: "7.1"}, true)
	got, err := svc.DepotCoreStatus("klant")
	if err != nil {
		t.Fatal(err)
	}
	if b.lastPath != "/wp-json/nb-mcp/v1/status" {
		t.Fatalf("pad %q", b.lastPath)
	}
	if got != (DepotCoreStatus{Live: "7.1.2", Locked: "7.1", NeedsPR: true}) {
		t.Fatalf("status %+v", got)
	}
}

func TestDepotCoreStatusGelijk(t *testing.T) {
	b := nieuweBridgeServer(t, 200, statusJSON)
	svc := depotSite(t, b, &fakeDepot{locked: "7.1.2"}, true)
	got, err := svc.DepotCoreStatus("klant")
	if err != nil || got.NeedsPR {
		t.Fatalf("status %+v err %v", got, err)
	}
}

func TestOpenDepotCorePRMaaktPR(t *testing.T) {
	b := nieuweBridgeServer(t, 200, statusJSON)
	d := &fakeDepot{locked: "7.1", prURL: "https://github.com/x/y/pull/7"}
	svc := depotSite(t, b, d, true)
	res, err := svc.OpenDepotCorePR("klant")
	if err != nil {
		t.Fatal(err)
	}
	if d.prVersion != "7.1.2" || d.prPath == "" {
		t.Fatalf("depot %+v", d)
	}
	if res.PullRequest != d.prURL || len(res.Items) != 1 || res.Items[0] != (RunItem{Name: "Composer-PR", From: "7.1", To: "7.1.2", Success: true}) {
		t.Fatalf("res %+v", res)
	}
	if svc.claimRun("klant") {
		svc.releaseRun("klant")
	} else {
		t.Fatal("run niet vrijgegeven")
	}
}

func TestOpenDepotCorePRAlGelijk(t *testing.T) {
	b := nieuweBridgeServer(t, 200, statusJSON)
	d := &fakeDepot{locked: "7.1.2", prURL: "x"}
	svc := depotSite(t, b, d, true)
	if _, err := svc.OpenDepotCorePR("klant"); err == nil || err.Error() != "composer.lock staat al op 7.1.2" {
		t.Fatalf("fout %v", err)
	}
	if d.prVersion != "" {
		t.Fatal("er mocht geen PR gemaakt worden")
	}
}

func TestOpenDepotCorePRDubbeleClaim(t *testing.T) {
	b := nieuweBridgeServer(t, 200, statusJSON)
	d := &fakeDepot{locked: "7.1", prURL: "x"}
	svc := depotSite(t, b, d, true)
	if !svc.claimRun("klant") {
		t.Fatal("eerste claim moet lukken")
	}
	defer svc.releaseRun("klant")
	if _, err := svc.OpenDepotCorePR("klant"); err == nil || err.Error() != "er loopt al een update voor deze site" {
		t.Fatalf("fout %v", err)
	}
	if d.prVersion != "" {
		t.Fatal("er mocht geen PR gemaakt worden")
	}
}

func TestOpenDepotCorePRFoutIsFout(t *testing.T) {
	b := nieuweBridgeServer(t, 200, statusJSON)
	svc := depotSite(t, b, &fakeDepot{locked: "7.1", prErr: errors.New("github token niet geconfigureerd")}, true)
	if _, err := svc.OpenDepotCorePR("klant"); err == nil || err.Error() != "github token niet geconfigureerd" {
		t.Fatalf("fout %v", err)
	}
}

// De PR raakt de live site niet, dus alleen-lezen houdt hem niet tegen.
func TestOpenDepotCorePROokBijAlleenLezen(t *testing.T) {
	b := nieuweBridgeServer(t, 200, statusJSON)
	d := &fakeDepot{locked: "7.1", prURL: "https://github.com/x/y/pull/7"}
	svc := depotSite(t, b, d, true)
	s, err := svc.find("klant")
	if err != nil {
		t.Fatal(err)
	}
	s.ReadOnly = true
	if _, err := svc.Update(s, ""); err != nil {
		t.Fatal(err)
	}
	if res, err := svc.OpenDepotCorePR("klant"); err != nil || res.PullRequest != d.prURL {
		t.Fatalf("res %+v err %v", res, err)
	}
}
