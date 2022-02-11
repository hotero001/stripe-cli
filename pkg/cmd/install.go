package cmd

import (
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/stripe/stripe-cli/pkg/plugins"
	"github.com/stripe/stripe-cli/pkg/validators"
)

type installCmd struct {
	cmd *cobra.Command
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
	plugin, err := plugins.LookUpPlugin(&Config, args[0])
	if err != nil {
		return err
	}

	version := plugin.LookUpLatestVersion()

	ctx := withSIGTERMCancel(cmd.Context(), func() {
		log.WithFields(log.Fields{
			"prefix": "cmd.installCmd.runInstallCmd",
		}).Debug("Ctrl+C received, cleaning up...")
	})

	err = plugin.Install(ctx, &Config, version)

	return err
}
