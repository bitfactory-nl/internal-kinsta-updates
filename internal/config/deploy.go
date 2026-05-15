package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const DeployConfFile = "deploy_conf.json"

// DeployConf represents the deploy_conf.json file present in each project.
type DeployConf struct {
	Type  string            `json:"type"`
	Link  DeployLinks       `json:"link"`
	Vars  map[string]string `json:"vars"`
}

type DeployLinks struct {
	Test string `json:"test"`
	Acc  string `json:"acc"`
	Prod string `json:"prod"`
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
