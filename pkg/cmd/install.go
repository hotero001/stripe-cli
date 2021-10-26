package cmd

import (
  "fmt"

	"github.com/spf13/cobra"

	"github.com/stripe/stripe-cli/pkg/plugins"
	"github.com/stripe/stripe-cli/pkg/validators"
)

type installCmd struct {
	cmd              *cobra.Command
}

func newInstallCmd() *installCmd {
	ic := &installCmd{}

	ic.cmd = &cobra.Command{
		Use:   "install",
		Args:  validators.ExactArgs(1),
		Short: "Install a Stripe CLI plugin",
		Long:  `Install a Stripe CLI plugin`,
		RunE:  ic.runInstallCmd,
	}

	return ic
}

func (ic *installCmd) runInstallCmd(cmd *cobra.Command, args []string) error {
  plugin, err := plugins.LookUpPlugin(args[0])
  if err != nil {
    return err
  }

  version := plugin.LookUpLatestVersion()
  err = plugin.Install(version)

  return err
}
