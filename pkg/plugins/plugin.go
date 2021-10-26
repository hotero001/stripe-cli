package plugins

import (
  "bytes"
  "encoding/hex"
  "errors"
  "log"
  "fmt"
  "io"
  "os"
  "os/exec"
  "path/filepath"
  "crypto/sha256"
  "runtime"

	"github.com/stripe/stripe-cli/pkg/config"

	"github.com/spf13/afero"
  hclog "github.com/hashicorp/go-hclog"
  hcplugin "github.com/hashicorp/go-plugin"
)

var Config config.Config

var PLUGIN_DEV = false
var PLUGINS_PATH = ""

type Plugin struct {
  Shortname string
  Binary    string
  ChecksumList  []Checksum
  MagicCookieValue string
}

type PluginList struct {
  Plugin []Plugin
}

type Checksum struct {
  Arch    string
  OS      string
  Version string
  Sum     string
}

func (p *Plugin) getPluginInterface() (hcplugin.HandshakeConfig, map[string]hcplugin.Plugin) {
  handshakeConfig := hcplugin.HandshakeConfig{
    ProtocolVersion:  1,
    MagicCookieKey:   fmt.Sprintf("plugin_%s", p.Shortname),
    MagicCookieValue: p.MagicCookieValue,
  }

  // pluginMap is the map of interfaces we can dispense from the plugin itself
  // we just have one called "main" for each of our plugins for now
  pluginMap := map[string]hcplugin.Plugin{
    "main": &CLIPlugin{},
  }

  return handshakeConfig, pluginMap
}

func (p *Plugin) getPluginInstallPath(version string) string {
  pluginsDir := getPluginsDir()
  pluginPath := filepath.Join(pluginsDir, p.Shortname, version)

  return pluginPath
}

func (p *Plugin) getChecksum(version string) ([]byte, error) {
  opsystem := runtime.GOOS
  arch := runtime.GOARCH

  var expectedSum string
  for _, pkg := range p.ChecksumList {
    if pkg.OS == opsystem && pkg.Arch == arch && pkg.Version == version {
      expectedSum = pkg.Sum
    }
  }

  if expectedSum == "" {
    return nil, errors.New(fmt.Sprintf("could not locate a valid checksum for %s version %s", p.Shortname, version))
  }

  decoded, err := hex.DecodeString(expectedSum)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("could not decode checksum for %s version %s", p.Shortname, version))
	}

  return decoded, nil
}

// note: assumes versions are listed in asc order
func (p *Plugin) LookUpLatestVersion() string {
  opsystem := runtime.GOOS
  arch := runtime.GOARCH

  var version string
  for _, pkg := range p.ChecksumList {
    if pkg.OS == opsystem && pkg.Arch == arch {
      version = pkg.Version
    }
  }

  return version
}

func (p *Plugin) Install(version string) error {
  return errors.New("this command is not yet supported")

  pluginDir := p.getPluginInstallPath(version)
  pluginFilePath := filepath.Join(pluginDir, p.Binary)

  // URL TBD
	repoBaseURL := ""
	pluginDownloadURL := fmt.Sprintf("%s/%s/%s/%s/%s/%s", repoBaseURL, p.Shortname, version, runtime.GOOS, runtime.GOARCH, p.Binary)

  binary, err := FetchRemoteResource(pluginDownloadURL)
	if err != nil {
    return err
	}

  err = p.verifyChecksum(binary, version)
	if err != nil {
    return err
	}

	fs := afero.NewOsFs()

  err = fs.MkdirAll(pluginDir, 0755)
	if err != nil {
    return err
	}

  file, err := fs.Create(pluginFilePath)
	if err != nil {
    return err
	}

  err = fs.Chmod(pluginFilePath, 0755)
	if err != nil {
    return err
	}

  _, err = io.Copy(file, binary)
	if err != nil {
    return err
	}

	defer file.Close()

  return nil
}

// this is to be used during installation only
// hcplugins takes care of the boot time verification for us
func (p *Plugin) verifyChecksum(binary io.Reader, version string) error {
  expectedSum, err := p.getChecksum(version)
	if err != nil {
    return err
	}

  hash := sha256.New()
  _, err = io.Copy(hash, binary);
  if err != nil {
    return err
  }

  actualSum := hash.Sum(nil)
  if bytes.Compare(actualSum, expectedSum) != 0 {
    return errors.New(fmt.Sprintf("installed plugin %s could not be verified, aborting installation", p.Shortname))
	}

  return nil
}

// RunPlugin boots up the binary and then sends the command to it via RPC
func (p *Plugin) Run(args []string) error {
  var version string
  // first perform a naive glob of the plugins/name dir for an existing version
  localPluginDir := filepath.Join(getPluginsDir(), p.Shortname, "*.*.*")
  existingLocalPlugin, err := filepath.Glob(localPluginDir)
  if err != nil {
    return err
  }

  if len(existingLocalPlugin) == 0 {
    // if none exist, then we should install it first (latest version)
    version = p.LookUpLatestVersion()
    err := p.Install(version)
    if err != nil {
      return err
    }
  } else {
    version = filepath.Base(existingLocalPlugin[0])
  }

  pluginDir := p.getPluginInstallPath(version)
  pluginBinaryPath := filepath.Join(pluginDir, p.Binary)

  cmd := exec.Command(pluginBinaryPath)
  handshakeConfig, pluginMap := p.getPluginInterface()

  pluginLogger := hclog.New(&hclog.LoggerOptions{
    Name:  fmt.Sprintf("[plugin:%s]", p.Shortname),
    Level: hclog.LevelFromString("INFO"),
  })

  clientConfig := &hcplugin.ClientConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         pluginMap,
		Cmd:             cmd,
    SyncStdout:      os.Stdout,
    SyncStderr:      os.Stderr,
    Logger:          pluginLogger,
	}

  if !PLUGIN_DEV {
    sum, err := p.getChecksum(version)
    if err != nil {
      return err
    }

    clientConfig.SecureConfig = &hcplugin.SecureConfig{
      Checksum: sum,
      Hash: sha256.New(),
    }
  }

  // start by launching the plugin process / binary
	client := hcplugin.NewClient(clientConfig)

	defer client.Kill()

	// Connect via RPC to the plugin
	rpcClient, err := client.Client()
	if err != nil {
    // TODO: handle this fatal
		log.Fatal(err)
	}

	// Request the plugin's main interface
	raw, err := rpcClient.Dispense("main")
	if err != nil {
    return err
	}

  // get the native golang interface for the plugin so that we can call it directly
	dispatcher := raw.(Dispatcher)

  // run the command that the user specified via args
  _, err = dispatcher.RunCommand(args)

  if (err != nil) {
    return err
  }

  return nil
}
