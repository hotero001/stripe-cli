package plugins

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/spf13/afero"

	"github.com/stripe/stripe-cli/pkg/config"
)

// getPluginsDir computes where plugins are installed locally
func getPluginsDir(config *config.Config) string {
	var pluginsDir string

	if os.Getenv("PLUGINS_PATH") != "" {
		pluginsDir = os.Getenv("PLUGINS_PATH")
	} else {
		configPath := config.GetConfigFolder(os.Getenv("XDG_CONFIG_HOME"))
		pluginsDir = filepath.Join(configPath, "plugins")
	}

	return pluginsDir
}

// GetPluginsList builds a list of allowed plugins to be installed and run by the CLI
func GetPluginList(config *config.Config) (PluginList, error) {
	var pluginList PluginList
	configPath := config.GetConfigFolder(os.Getenv("XDG_CONFIG_HOME"))
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

// LookUpPlugin returns the matching plugin object
func LookUpPlugin(config *config.Config, pluginName string) (Plugin, error) {
	var plugin Plugin
	pluginList, err := GetPluginList(config)
	if err != nil {
		return plugin, err
	}

	for _, p := range pluginList.Plugin {
		if pluginName == strings.ToLower(p.Shortname) {
			return p, nil
		}
	}

	return plugin, fmt.Errorf("could not find a plugin named %s", pluginName)
}

// RefreshPluginManifest refreshes the plugin manifest
func RefreshPluginManifest(config *config.Config) error {
	// final URL TBD
	body, err := FetchRemoteResource("")

	if err != nil {
		return err
	}

	configPath := config.GetConfigFolder(os.Getenv("XDG_CONFIG_HOME"))
	pluginManifestPath := filepath.Join(configPath, "plugins.toml")
	fs := afero.NewOsFs()

	err = afero.WriteFile(fs, pluginManifestPath, body, 0755)

	if err != nil {
		return err
	}

	return nil
}

// FetchRemoteResource returns the remote resource body
func FetchRemoteResource(url string) ([]byte, error) {
	client := http.Client{
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			r.URL.Opaque = r.URL.Path
			return nil
		},
	}

	req, err := http.NewRequest(http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		return nil, err
	}

	return body, nil
}