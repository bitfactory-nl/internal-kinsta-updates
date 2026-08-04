package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const deployConfFixture = `{
	"type": "wordpress_kinsta",
	"jenkins_notification": {"channel": "#deploys"},
	"link": {
		"acc": "https://x.acc.teamexpedition.nl",
		"acc_new": "https://x2.acc.teamexpedition.nl",
		"prod": "https://x.nl",
		"prod_cn": "https://cn.x.nl"
	},
	"vars": {"common_composer_command": "composer22"}
}`

func TestSaveDeployLinkLocalRoundTrip(t *testing.T) {
	dir := t.TempDir()
	confPath := filepath.Join(dir, DeployConfFile)
	if err := os.WriteFile(confPath, []byte(deployConfFixture), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := SaveDeployLinkLocal(dir, "https://xnl.test"); err != nil {
		t.Fatalf("SaveDeployLinkLocal: %v", err)
	}

	raw, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	link, ok := generic["link"].(map[string]any)
	if !ok {
		t.Fatalf("link field missing or wrong type: %+v", generic["link"])
	}
	if link["local"] != "https://xnl.test" {
		t.Errorf("link.local = %v, want https://xnl.test", link["local"])
	}

	// Unknown/untouched fields must survive byte-for-byte semantically.
	jenkins, ok := generic["jenkins_notification"].(map[string]any)
	if !ok || jenkins["channel"] != "#deploys" {
		t.Errorf("jenkins_notification not preserved: %+v", generic["jenkins_notification"])
	}
	if link["acc_new"] != "https://x2.acc.teamexpedition.nl" {
		t.Errorf("link.acc_new not preserved: %v", link["acc_new"])
	}
	if link["prod_cn"] != "https://cn.x.nl" {
		t.Errorf("link.prod_cn not preserved: %v", link["prod_cn"])
	}
	vars, ok := generic["vars"].(map[string]any)
	if !ok || vars["common_composer_command"] != "composer22" {
		t.Errorf("vars.common_composer_command not preserved: %+v", generic["vars"])
	}

	dc, err := LoadDeployConf(dir)
	if err != nil {
		t.Fatalf("LoadDeployConf: %v", err)
	}
	if dc.Link.Local != "https://xnl.test" {
		t.Errorf("LoadDeployConf().Link.Local = %q, want https://xnl.test", dc.Link.Local)
	}
}

func TestSaveDeployLinkLocalMissingFile(t *testing.T) {
	dir := t.TempDir()
	err := SaveDeployLinkLocal(dir, "https://xnl.test")
	if err == nil {
		t.Fatal("expected error when deploy_conf.json is absent, got nil")
	}
}
