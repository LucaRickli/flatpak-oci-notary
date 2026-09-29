package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/lucarickli/flatpak-oci-notary/internal/version"
)

func newRootCmd() *cobra.Command {
	v := viper.New()
	var cfgFile string

	root := &cobra.Command{
		Use:   "notary",
		Short: "Serve Flatpak OCI remote indexes for images on OCI registries",
		Long: `Flatpak OCI Notary indexes flatpak images on OCI registries and serves
flatpak remote indexes for them.

"notary serve" runs the server, by default with the sync scheduler embedded.
"notary sync" syncs registries from a separate process against the same
database, for deployments where the server runs with sync.enabled=false.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return loadConfig(v, cfgFile)
		},
	}
	root.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file (YAML/TOML/JSON); default: ./notary.yaml or /etc/notary/notary.yaml if present")
	root.PersistentFlags().String("log-level", "info", "log level: trace, debug, info, warn, error")
	root.PersistentFlags().String("log-format", "json", "log format: json or console")
	mustBind(v, "log.level", root.PersistentFlags().Lookup("log-level"))
	mustBind(v, "log.format", root.PersistentFlags().Lookup("log-format"))

	root.AddCommand(newServeCmd(v), newSyncCmd(v), newVersionCmd())
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "notary %s (%s)\n", version.Version, version.Commit)
		},
	}
}

func loadConfig(v *viper.Viper, cfgFile string) error {
	v.SetEnvPrefix("NOTARY")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()
	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
	} else {
		v.SetConfigName("notary")
		v.AddConfigPath(".")
		v.AddConfigPath("/etc/notary")
	}
	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if cfgFile != "" || !errors.As(err, &notFound) {
			return fmt.Errorf("read config: %w", err)
		}
	}
	return nil
}

func newLogger(v *viper.Viper) (zerolog.Logger, error) {
	level, err := zerolog.ParseLevel(strings.ToLower(v.GetString("log.level")))
	if err != nil || level == zerolog.NoLevel {
		return zerolog.Logger{}, fmt.Errorf("invalid log level %q", v.GetString("log.level"))
	}
	zerolog.TimeFieldFormat = time.RFC3339Nano
	var log zerolog.Logger
	switch v.GetString("log.format") {
	case "console":
		log = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.DateTime})
	case "json":
		log = zerolog.New(os.Stderr)
	default:
		return zerolog.Logger{}, fmt.Errorf("invalid log format %q (json or console)", v.GetString("log.format"))
	}
	log = log.Level(level).With().Timestamp().Logger()
	zerolog.DefaultContextLogger = &log
	return log, nil
}
