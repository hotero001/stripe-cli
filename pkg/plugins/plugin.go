package plugins

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/stripe/stripe-cli/pkg/config"

	hclog "github.com/hashicorp/go-hclog"
	hcplugin "github.com/hashicorp/go-plugin"
)

// Config is the plugins package access to the CLI config
var Config config.Config

var PLUGIN_DEV = false

// Plugin is the main type that represents an approved plugin
type Plugin struct {
	Shortname        string
	Binary           string
	Release          []Release
	MagicCookieValue string
}

// PluginList contains a slice of approved plugins
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
func (p *Plugin) getPluginInstallPath(version string) string {
	pluginsDir := getPluginsDir()
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
		return nil, errors.New(fmt.Sprintf("could not locate a valid checksum for %s version %s", p.Shortname, version))
	}

	decoded, err := hex.DecodeString(expectedSum)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("could not decode checksum for %s version %s", p.Shortname, version))
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

// verifyChecksum takes a downloaded plugin binary and verifies it against a trusted checksum source
// this is to be used during installation only
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
	if bytes.Compare(actualSum, expectedSum) != 0 {
		return errors.New(fmt.Sprintf("installed plugin %s could not be verified, aborting installation", p.Shortname))
	}

	return nil
}

// Run boots up the binary and then sends the command to it via RPC
func (p *Plugin) Run(args []string) error {
	var version string

	if os.Getenv("PLUGINS_PATH") != "" {
		version = "master"
	} else {
		// first perform a naive glob of the plugins/name dir for an existing version
		localPluginDir := filepath.Join(getPluginsDir(), p.Shortname, "*.*.*")
		existingLocalPlugin, err := filepath.Glob(localPluginDir)
		if err != nil {
			return err
		}

		// if plugin is not installed locally, then we should return an error
		// (installation step coming in phase 2)
		if len(existingLocalPlugin) == 0 {
			return errors.New("this command is not currently available")
		} else {
			version = filepath.Base(existingLocalPlugin[0])
		}
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

	defer client.Kill()

	// Connect via RPC to the plugin
	rpcClient, err := client.Client()
	if err != nil {
		return err
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

	if err != nil {
		return err
	}

	return nil
}
