// Command finance reports exchange rates and equity quotes from the terminal.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	_ "modernc.org/sqlite" // the CGO-free SQLite driver; storage/sqldb takes a *sql.DB.

	"github.com/Toshik1978/finance"
	"github.com/Toshik1978/finance/internal/cli"
	"github.com/Toshik1978/finance/provider/alpha"
	"github.com/Toshik1978/finance/provider/yahoo"
	"github.com/Toshik1978/finance/storage/sqldb"
)

// Set by the linker; see Taskfile's build target.
//
//nolint:gochecknoglobals // ldflags -X can only target package-level vars.
var (
	Buildstamp = "undefined"
	Commit     = "undefined"
)

// Exit codes: 2 for misuse, 1 for a runtime failure. The old CLI discarded
// cobra's error and always exited 0, which made it impossible to script.
const (
	exitFailure = 1
	exitUsage   = 2
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "finance:", err)

		if errors.Is(err, cli.ErrUsage) {
			os.Exit(exitUsage)
		}
		os.Exit(exitFailure)
	}
}

func run() error {
	cfg, err := cli.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	db, err := openDB(cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	// One HTTP client for every provider, with a timeout. The old CLI used
	// http.DefaultClient, which has none — a hung socket hung the tool forever.
	hc := &http.Client{Timeout: cfg.HTTPTimeout}

	client := finance.NewClient(log, providers(log, hc, cfg), sqldb.New(db))

	if err := root(client, log).Execute(); err != nil {
		return fmt.Errorf("command failed: %w", err)
	}

	return nil
}

// providers builds the fallback chain, best source first. AlphaVantage joins
// it only when a key is configured, since it cannot answer without one.
func providers(log *slog.Logger, hc *http.Client, cfg cli.Config) []finance.Provider {
	ps := []finance.Provider{yahoo.New(log, yahoo.NewClient(log, hc))}

	if cfg.AlphaKey != "" {
		ps = append(ps, alpha.New(log, alpha.NewClient(log, hc, cfg.AlphaKey)))
	}

	return ps
}

// openDB opens the cache database, creating its directory and schema if needed.
func openDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("failed to create the database directory: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open the database: %w", err)
	}

	if _, err := db.ExecContext(context.Background(), sqldb.SchemaSQLite); err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("failed to apply the database schema: %w", err)
	}

	return db, nil
}

// root assembles the cobra command tree.
func root(client cli.Client, log *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "finance",
		Short:         "Look up exchange rates and equity quotes",
		Long:          "finance reports current exchange rates and equity quotes from the terminal.",
		SilenceUsage:  true, // run() prints the error itself
		SilenceErrors: true,
		// Args must be non-nil so cobra defers unknown-subcommand checking to
		// ValidateArgs below, rather than raising its own unwrapped error.
		Args: func(c *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("unknown command %q for %q: %w", args[0], c.CommandPath(), cli.ErrUsage)
			}
			return nil
		},
		RunE: func(c *cobra.Command, _ []string) error {
			return c.Help()
		},
	}
	// Set once here: FlagErrorFunc walks up to a parent's when unset on the
	// command whose own flags failed, so this also covers every subcommand.
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return fmt.Errorf("%w: %w", err, cli.ErrUsage)
	})

	cmd.AddCommand(&cobra.Command{
		Use:                   "version",
		Short:                 "Print version information",
		DisableFlagsInUseLine: true,
		Run: func(c *cobra.Command, _ []string) {
			fmt.Fprintf(c.OutOrStdout(), "finance - commit: %s, built: %s\n", Commit, Buildstamp)
		},
	})

	for _, sub := range []Command{
		cli.NewCurrency(log, client),
		cli.NewQuote(log, client),
	} {
		cmd.AddCommand(subcommand(sub))
	}

	return cmd
}

// Command is one subcommand of the finance CLI.
type Command interface {
	// Name is the subcommand as typed, e.g. "quote".
	Name() string
	// Short is the one-line description shown in the command list.
	Short() string
	// Long is the help text shown for this command alone.
	Long() string
	// Run executes the command, writing its output to out.
	Run(ctx context.Context, args []string, out io.Writer) error
}

// subcommand adapts a Command to cobra, giving it the writer to print to.
func subcommand(c Command) *cobra.Command {
	return &cobra.Command{
		Use:   c.Name(),
		Short: c.Short(),
		Long:  c.Long(),
		RunE: func(cc *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cc.Context(), time.Minute)
			defer cancel()

			return c.Run(ctx, args, cc.OutOrStdout())
		},
	}
}
