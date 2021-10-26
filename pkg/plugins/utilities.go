package plugins

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// getPluginsDir computes where plugins are installed locally
func getPluginsDir() string {
	var pluginsDir string

	if os.Getenv("PLUGINS_PATH") != "" {
		pluginsDir = os.Getenv("PLUGINS_PATH")
	} else {
		configPath := Config.GetConfigFolder(os.Getenv("XDG_CONFIG_HOME"))
		pluginsDir = filepath.Join(configPath, "plugins")
	}

	return pluginsDir
}

// GetPluginsList builds a list of allowed plugins to be installed and run by the CLI
func GetPluginList() (PluginList, error) {
	var pluginList PluginList
	configPath := Config.GetConfigFolder(os.Getenv("XDG_CONFIG_HOME"))
	pluginManifestPath := filepath.Join(configPath, "plugins.toml")

	file, err := os.ReadFile(pluginManifestPath)
	if err != nil {
		return pluginList, err
	}

	_, err = toml.Decode(string(file), &pluginList)
	if err != nil {
		return pluginList, err
	}

	return pluginList, nil
}

// LookUpPlugin takes a plugin command string and attempts to find it in the approved plugin list
func LookUpPlugin(pluginName string) (Plugin, error) {
	var plugin Plugin
	pluginList, err := GetPluginList()
	if err != nil {
		return plugin, err
	}

	for _, p := range pluginList.Plugin {
		if pluginName == strings.ToLower(p.Shortname) {
			return p, nil
		}
	}

	return plugin, errors.New(fmt.Sprintf("could not find a plugin named %s", pluginName))
}

// FetchRemoteResource is a convenience function for downloading plugin packages and manifests in future
func FetchRemoteResource(URL string) (io.Reader, error) {
	client := http.Client{
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			r.URL.Opaque = r.URL.Path
			return nil
		},
	}

	req, err := http.NewRequest(http.MethodGet, URL, http.NoBody)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	return resp.Body, nil
}
