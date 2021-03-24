package operator

import (
	"github.com/scylladb/scylla-operator/pkg/cmdutil"
	"github.com/scylladb/scylla-operator/pkg/genericclioptions"
	"github.com/spf13/cobra"
)

const (
	EnvVarPrefix = "SCYLLA_OPERATOR_"
)

func NewOperatorCommand(streams genericclioptions.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return cmdutil.ReadFlagsFromEnv(EnvVarPrefix, cmd)
		},
	}

	cmd.AddCommand(NewOperatorCmd(streams))
	// cmd.AddCommand(NewSidecarCmd(streams))
	// cmd.AddCommand(NewManagerControllerCmd(streams))

	// TODO: wrap help func for the root command and every subcommand to add a line about automatic env vars and the prefix.

	cmdutil.InstallKlog(cmd)

	return cmd
}
