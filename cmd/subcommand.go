package cmd

import (
	"errors"
	"math/rand/v2"

	"github.com/spf13/cobra"
)

var flagValue uint64

func init() {
	// Init gets run on application startup. Each subcommand adds itself to the parent
	// root command. This prevents a circular dependency as the root command does not
	// need to know about the subcommands under it.
	rootCmd.AddCommand(subCmd)
	subCmd.PersistentFlags().Uint64VarP(&flagValue, "flag", "f", 0,
		`Flags of any type can be easily defined to allow
very configurable user input. Both long flag names and short
character switches are fully supported along with default
values if the flags are not specified.`)
}

var subCmd = &cobra.Command{
	Use:   "subcommand [flags] <input-text>",
	Short: "Example argument handling",
	Long: `Any number of positional parameters can be required
and validated beyond the defined flags.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) < 1 {
			return errors.New("requires positional parameter")
		}
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		// By setting the default value of a flag to the type's default, it becomes
		// trivial to check if the flag was provided or not and then you can provide
		// a more complex default value without it appearing in the help text.
		if !cmd.Flags().Changed("flag") {
			flagValue = rand.Uint64()
		}

		err := execute_subcommand(args[0])
		if err != nil {
			panic(err)
		}
	},
}

func execute_subcommand(input_text string) error {
	print(input_text)
	return nil
}
