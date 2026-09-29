package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/lucarickli/flatpak-oci-notary/internal/indexer"
	"github.com/lucarickli/flatpak-oci-notary/internal/version"
)

// syncOptions are the command-line only selectors of the sync command.
type syncOptions struct {
	registries []string
	due, watch bool
}

func newSyncCmd(v *viper.Viper) *cobra.Command {
	var opts syncOptions
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync registries without running the server",
		Long: `Sync registries against the same database as the server, without running the
server itself. Use it with a server started with sync.enabled=false, so
syncing runs in a separate process (a Kubernetes CronJob or a worker).

By default it syncs the registries once and exits: with a non-zero status
when any of them failed. Registries being synced by another process are
skipped. --due only syncs registries whose interval elapsed, for which a
sync was requested through the server ("Request sync", or a registry that
was created or changed), or whose last sync was interrupted. --watch keeps running the scheduler, like the
server's embedded syncer, until SIGINT or SIGTERM.

The database and sync settings are the ones of "notary serve" (same flags,
config keys and NOTARY_* environment variables).`,
		Example: `  notary sync                       # sync every registry once
  notary sync --due                 # only the due ones (for a CronJob)
  notary sync --registry fedora     # one registry, by name or id
  notary sync --watch               # run the scheduler as a worker`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			bindFlags(v, cmd.Flags(), databaseFlags, syncFlags, []flagSpec{checkIntervalFlag})
			return runSync(cmd.Context(), v, opts)
		},
	}
	f := cmd.Flags()
	defineFlags(f, databaseFlags, syncFlags, []flagSpec{checkIntervalFlag})
	f.StringArrayVar(&opts.registries, "registry", nil, "registry to sync, by name or id (repeatable; default: all)")
	f.BoolVar(&opts.due, "due", false, "only sync registries that are due: interval elapsed, pending sync request or interrupted sync")
	f.BoolVar(&opts.watch, "watch", false, "keep running the sync scheduler until SIGINT/SIGTERM instead of syncing once")
	cmd.MarkFlagsMutuallyExclusive("watch", "due")
	cmd.MarkFlagsMutuallyExclusive("watch", "registry")
	return cmd
}

func runSync(ctx context.Context, v *viper.Viper, opts syncOptions) error {
	log, err := newLogger(v)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, dbCfg, err := openStore(ctx, v, log)
	if err != nil {
		return err
	}
	defer st.Close()

	// Every sync process gets its own identity, unless one is configured
	// (e.g. the pod name of a worker, so a restarted worker releases the
	// syncs it left running at once). The pid alone is not unique: in a
	// container it is usually 1, and containers sharing the host's network
	// namespace also share its hostname, so a random part is added.
	idx := newIndexer(v, st, log, instanceID(v.GetString("sync.instance"), syncSuffix()))
	log.Info().Str("version", version.Version).Str("database_driver", dbCfg.Driver).Str("database", dbCfg.Redacted()).
		Str("instance", idx.Owner()).Bool("watch", opts.watch).Msg("notary sync")
	// Release syncs a previous process with this identity left running (a
	// configured stable identity); with the default identity, which is new
	// on every start, a no-op.
	idx.ResetInterrupted(ctx)

	if opts.watch {
		idx.Run(ctx)
		log.Info().Msg("shutting down")
		return nil
	}

	sum, err := idx.SyncAll(ctx, indexer.Selection{Registries: opts.registries, Due: opts.due})
	if err != nil {
		return err
	}
	ev := log.Info()
	if sum.Failed > 0 {
		ev = log.Error()
	}
	ev.Int("synced", sum.Synced).Int("failed", sum.Failed).Int("skipped", sum.Skipped).Msg("sync run finished")
	if sum.Failed > 0 {
		return fmt.Errorf("%d of %d registries failed to sync", sum.Failed, sum.Synced+sum.Failed)
	}
	return nil
}

// syncSuffix is the instance id suffix of a sync process: its pid and a
// random part, so the default identity is unique per process start.
func syncSuffix() string {
	return fmt.Sprintf(":sync-%d-%s", os.Getpid(), rand.Text()[:6])
}
