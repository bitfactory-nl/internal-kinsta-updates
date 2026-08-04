package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tidwall/sjson"
)

const DeployConfFile = "deploy_conf.json"

// DeployConf represents the deploy_conf.json file present in each project.
type DeployConf struct {
	Type string            `json:"type"`
	Link DeployLinks       `json:"link"`
	Vars map[string]string `json:"vars"`
}

type DeployLinks struct {
	Test  string `json:"test"`
	Acc   string `json:"acc"`
	Prod  string `json:"prod"`
	Local string `json:"local,omitempty"`
}

// LoadDeployConf reads deploy_conf.json from the project root.
// Returns an empty DeployConf (not an error) when the file is absent.
func LoadDeployConf(repoPath string) (DeployConf, error) {
	data, err := os.ReadFile(filepath.Join(repoPath, DeployConfFile))
	if os.IsNotExist(err) {
		return DeployConf{}, nil
	}
	if err != nil {
		return DeployConf{}, err
	}
	var dc DeployConf
	if err := json.Unmarshal(data, &dc); err != nil {
		return DeployConf{}, err
	}
	return dc, nil
}

// SaveDeployLinkLocal sets only the link.local path in deploy_conf.json,
// leaving every other field byte-for-byte as it was. It deliberately does not
// round-trip through the DeployConf struct: deploy_conf.json in the wild
// carries unpredictable extra fields (e.g. jenkins_notification, link.acc_new,
// link.prod_cn) that a struct-based rewrite would silently drop.
func SaveDeployLinkLocal(repoPath, url string) error {
	confPath := filepath.Join(repoPath, DeployConfFile)
	data, err := os.ReadFile(confPath)
	if os.IsNotExist(err) {
		return fmt.Errorf("geen deploy_conf.json gevonden in %s; zonder dat bestand kan geen lokale link worden opgeslagen", repoPath)
	}
	if err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if info, statErr := os.Stat(confPath); statErr == nil {
		mode = info.Mode()
	}
	updated, err := sjson.SetBytes(data, "link.local", url)
	if err != nil {
		return fmt.Errorf("link.local instellen in deploy_conf.json: %w", err)
	}
	return os.WriteFile(confPath, updated, mode)
}
