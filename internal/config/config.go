package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// Set merges command line flags and env variables, giving preference to the
// command line argument when specified. A flag not passed on the command line
// resolves from "<PREFIX>_<FLAG>", upper-cased with dashes as underscores. It
// returns an error naming every env variable whose value its flag rejected.
func Set(cmd *cobra.Command, prefix string) error {
	prefix = strings.TrimSpace(prefix)

	v := viper.New()

	v.SetEnvPrefix(prefix)
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()

	var errs []error

	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		// If the user doesn't pass a flag but viper has an env variable,
		// the flag value is set to viper's env variable.
		if !f.Changed && v.IsSet(f.Name) {
			val := v.Get(f.Name)
			if err := cmd.Flags().Set(f.Name, fmt.Sprintf("%v", val)); err != nil {
				name := strings.ReplaceAll(strings.ToUpper(prefix+"_"+f.Name), "-", "_")
				errs = append(errs, fmt.Errorf("%s: %w", name, err))
			}
		}
	})

	return errors.Join(errs...)
}
