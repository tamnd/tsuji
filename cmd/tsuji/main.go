package main

import (
	"context"
	"fmt"
	"os"

	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"

	"github.com/tamnd/tsuji/pkg/config"
	"github.com/tamnd/tsuji/pkg/server"
)

var version = "dev"

func main() {
	root := &cobra.Command{
		Use:   "tsuji",
		Short: "Self-hostable LLM gateway and model marketplace",
		Long:  "Tsuji (辻) is the crossroads where your requests meet the provider that should serve them.\nOne key, one OpenAI-compatible endpoint, every model behind it.",
	}

	root.AddCommand(serveCmd(), keyCmd())

	if err := fang.Execute(context.Background(), root, fang.WithVersion(version)); err != nil {
		os.Exit(1)
	}
}

func serveCmd() *cobra.Command {
	var addr string
	var dbPath string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the gateway and web UI",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if addr != "" {
				cfg.Addr = addr
			}
			if dbPath != "" {
				cfg.DBPath = dbPath
			}
			srv, err := server.New(cfg)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "tsuji listening on %s\n", cfg.Addr)
			return srv.ListenAndServe(cmd.Context())
		},
	}

	cmd.Flags().StringVar(&addr, "addr", "", "listen address (default :4780)")
	cmd.Flags().StringVar(&dbPath, "db", "", "sqlite database path (default ~/.tsuji/tsuji.db)")
	return cmd
}
