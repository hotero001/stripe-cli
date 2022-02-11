package plugins

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/stripe/stripe-cli/pkg/config"
	"github.com/stripe/stripe-cli/pkg/requests"
	"github.com/stripe/stripe-cli/pkg/stripe"

	hclog "github.com/hashicorp/go-hclog"
	hcplugin "github.com/hashicorp/go-plugin"
	"github.com/spf13/afero"
)

// constants for plugin
const (
	PluginDev   bool   = false
	PluginsPath string = ""
)

// Plugin contains the plugin properties
type Plugin struct {
	Shortname        string
	Binary           string
	Release          []Release
	MagicCookieValue string
}

// PluginList contains a list of plugins
type PluginList struct {
	Plugin []Plugin
}

// Release is the type that holds release data for a specific build of a plugin
type Release struct {
	Arch    string
	OS      string
	Version string
	Sum     string
}

// getPluginInterface computes the correct metadata needed for starting the hcplugin client
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

// getPluginInstallPath computes the absolute path of a specific plugin version's installation dir
func (p *Plugin) getPluginInstallPath(config *config.Config, version string) string {
	pluginsDir := getPluginsDir(config)
	pluginPath := filepath.Join(pluginsDir, p.Shortname, version)

	return pluginPath
}

// getChecksum does what it says on the tin - it returns the checksum for a specific plugin version
func (p *Plugin) getChecksum(version string) ([]byte, error) {
	opsystem := runtime.GOOS
	arch := runtime.GOARCH

	var expectedSum string
	for _, release := range p.Release {
		if release.OS == opsystem && release.Arch == arch && release.Version == version {
			expectedSum = release.Sum
		}
	}

	if expectedSum == "" {
		return nil, fmt.Errorf("could not locate a valid checksum for %s version %s", p.Shortname, version)
	}

	decoded, err := hex.DecodeString(expectedSum)
	if err != nil {
		return nil, fmt.Errorf("could not decode checksum for %s version %s", p.Shortname, version)
	}

	return decoded, nil
}

// LookUpLatestVersion iterates through each version of a plugin and returns the latest
// note: assumes versions are listed in asc order, might need to be more robust in future
func (p *Plugin) LookUpLatestVersion() string {
	opsystem := runtime.GOOS
	arch := runtime.GOARCH

	var version string
	for _, pkg := range p.Release {
		if pkg.OS == opsystem && pkg.Arch == arch {
			version = pkg.Version
		}
	}

	return version
}

// Install installs the plugin of the given version
func (p *Plugin) Install(ctx context.Context, config *config.Config, version string) error {
	pluginDir := p.getPluginInstallPath(config, version)
	pluginFilePath := filepath.Join(pluginDir, p.Binary)

	apiKey, err := config.Profile.GetAPIKey(false)
	if err != nil {
		return err
	}

	pluginData, err := requests.GetPluginData(ctx, stripe.DefaultAPIBaseURL, stripe.DefaultAPIVersion, apiKey, &config.Profile)
	if err != nil {
		return err
	}

	pluginDownloadURL := fmt.Sprintf("%s/%s/%s/%s/%s/%s", pluginData.PluginBaseURL, p.Shortname, version, runtime.GOOS, runtime.GOARCH, p.Binary)

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

// verifyChecksum is to be used during installation only
// hcplugins takes care of the boot time verification for us
func (p *Plugin) verifyChecksum(binary io.Reader, version string) error {
	expectedSum, err := p.getChecksum(version)
	if err != nil {
		return err
	}

	hash := sha256.New()
	_, err = io.Copy(hash, binary)
	if err != nil {
		return err
	}

	actualSum := hash.Sum(nil)
	if !bytes.Equal(actualSum, expectedSum) {
		return fmt.Errorf("installed plugin %s could not be verified, aborting installation", p.Shortname)
	}

	return nil
}

// Run boots up the binary and then sends the command to it via RPC
func (p *Plugin) Run(ctx context.Context, config *config.Config, args []string) error {
	var version string

	if os.Getenv("PLUGINS_PATH") != "" {
		version = "master"
	} else {
		// first perform a naive glob of the plugins/name dir for an existing version
		localPluginDir := filepath.Join(getPluginsDir(config), p.Shortname, "*.*.*")
		existingLocalPlugin, err := filepath.Glob(localPluginDir)
		if err != nil {
			return err
		}

		// if plugin is not installed locally, then we should return an error
		// (installation step coming in phase 2)
		if len(existingLocalPlugin) == 0 {
			// if none exist, then we should install it first (latest version)
			version = p.LookUpLatestVersion()
			err := p.Install(ctx, config, version)
			if err != nil {
				return err
			}
		} else {
			version = filepath.Base(existingLocalPlugin[0])
		}
	}

	pluginDir := p.getPluginInstallPath(config, version)
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

	sum, err := p.getChecksum(version)
	if err != nil {
		return err
	}

	clientConfig.SecureConfig = &hcplugin.SecureConfig{
		Checksum: sum,
		Hash:     sha256.New(),
	}

	// start by launching the plugin process / binary
	client := hcplugin.NewClient(clientConfig)

	// Connect via RPC to the plugin
	rpcClient, err := client.Client()
	if err != nil {
		// TODO: handle this fatal
		log.Fatal(err)
	}

	defer client.Kill()

	// Request the plugin's main interface
	raw, err := rpcClient.Dispense("main")
	if err != nil {
		return err
	}

	// get the native golang interface for the plugin so that we can call it directly
	dispatcher := raw.(Dispatcher)

	// run the command that the user specified via args
	_, err = dispatcher.RunCommand(args)

	if err != nil {
		return err
	}

	return nil
}
