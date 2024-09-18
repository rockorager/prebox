package prebox

import (
	"fmt"
	"net/url"
	"os"
	"path"

	"git.sr.ht/~emersion/go-scfg"
)

type Config struct {
	url  *url.URL
	name string
}

func LoadConfig() ([]Config, error) {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		cfgDir = path.Join(homeDir, ".config")
	}
	p := path.Join(cfgDir, "prebox", "config.scfg")
	block, err := scfg.Load(p)
	if err != nil {
		return nil, err
	}

	configs := []Config{}
	remotes := block.GetAll("remote")
	for _, remote := range remotes {
		var name string
		if err := remote.ParseParams(&name); err != nil {
			return nil, fmt.Errorf("remote must have a name")
		}
		server := remote.Children.Get("server")
		if server == nil {
			return nil, fmt.Errorf("server not found in remote %s", name)
		}
		var serverUrlString string
		if err := server.ParseParams(&serverUrlString); err != nil {
			return nil, fmt.Errorf("server must have a url")
		}
		serverUrl, err := url.Parse(serverUrlString)
		if err != nil {
			return nil, err
		}
		configs = append(configs, Config{
			name: name,
			url:  serverUrl,
		})
	}

	return configs, nil
}
