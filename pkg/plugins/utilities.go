package plugins

import (
  "errors"
  "fmt"
	"net/http"
  "io"
  "os"
  "path/filepath"
  "strings"

	"github.com/spf13/afero"
  "github.com/BurntSushi/toml"
)

func getPluginsDir() string {
  var pluginsDir string

  if PLUGINS_PATH != "" {
    pluginsDir = PLUGINS_PATH
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

func RefreshPluginManifest() error {
  // final URL TBD
  body, err := FetchRemoteResource("")
	if err != nil {
    return err
	}

  configPath := Config.GetConfigFolder(os.Getenv("XDG_CONFIG_HOME"))
  pluginManifestPath := filepath.Join(configPath, "plugins.toml")
  fs := afero.NewOsFs()

  file, err := fs.Create(pluginManifestPath)
	if err != nil {
    return err
	}

	defer file.Close()

  _, err = io.Copy(file, body)
	if err != nil {
    return err
	}

  return nil
}

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
