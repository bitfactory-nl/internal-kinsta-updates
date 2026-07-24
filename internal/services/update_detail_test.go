package services

import "testing"

func TestParseUpdateManifest(t *testing.T) {
	data := []byte(`{
	  "generatedAt": "2026-07-20T09:00:57Z",
	  "wordpress": {
	    "core": [{"version":"7.0.2","updateType":"major"}],
	    "plugins": [{"name":"acfml","from":"2.2.3","to":"2.2.4"}],
	    "themes": []
	  },
	  "npm": {
	    "applied": [{"name":"sass","from":"1.99.0","to":"1.101.7","type":"minor"}],
	    "availableMajors": [{"name":"eslint","from":"9.39.2","to":"10.7.0"}]
	  }
	}`)
	d, err := parseUpdateManifest(data)
	if err != nil {
		t.Fatalf("parseUpdateManifest: %v", err)
	}
	if d.Source != "manifest" {
		t.Errorf("Source = %q, want manifest", d.Source)
	}
	if len(d.WPCore) != 1 || d.WPCore[0].UpdateType != "major" {
		t.Errorf("WPCore = %+v", d.WPCore)
	}
	if len(d.WPPlugins) != 1 || d.WPPlugins[0].Name != "acfml" {
		t.Errorf("WPPlugins = %+v", d.WPPlugins)
	}
	if len(d.NpmApplied) != 1 || d.NpmApplied[0].Type != "minor" {
		t.Errorf("NpmApplied = %+v", d.NpmApplied)
	}
	if len(d.NpmAvailableMajors) != 1 || d.NpmAvailableMajors[0].Name != "eslint" {
		t.Errorf("NpmAvailableMajors = %+v", d.NpmAvailableMajors)
	}
}

func TestParseWpUpdateLog(t *testing.T) {
	log := "WordPress update check uitgevoerd op: x\n\n=== WORDPRESS CORE ===\n" +
		"version\tupdate_type\tpackage_url\n7.0.2\tmajor\thttps://x\n" +
		"=== PLUGINS ===\nname\tversion\tupdate_version\nacfml\t2.2.3\t2.2.4\n" +
		"=== THEMES ===\nname\tversion\tupdate_version\n"
	core, plugins, themes := parseWpUpdateLog(log)
	if len(core) != 1 || core[0].UpdateType != "major" {
		t.Errorf("core = %+v", core)
	}
	if len(plugins) != 1 || plugins[0].To != "2.2.4" {
		t.Errorf("plugins = %+v", plugins)
	}
	if len(themes) != 0 {
		t.Errorf("themes = %+v", themes)
	}
}

func TestNpmAppliedFromPackageJSON(t *testing.T) {
	before := `{"dependencies":{"sass":"^1.99.0"},"devDependencies":{"webpack":"5.106.2"}}`
	after := `{"dependencies":{"sass":"^1.101.7"},"devDependencies":{"webpack":"5.109.0"}}`
	got := npmAppliedFromPackageJSON(before, after)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (%+v)", len(got), got)
	}
	m := map[string]PackageUpdate{}
	for _, u := range got {
		m[u.Name] = u
	}
	if m["sass"].From != "^1.99.0" || m["sass"].To != "^1.101.7" {
		t.Errorf("sass = %+v", m["sass"])
	}
}
