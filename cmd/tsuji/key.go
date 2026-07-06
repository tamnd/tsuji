package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/tamnd/tsuji/pkg/config"
	"github.com/tamnd/tsuji/pkg/store"
)

func keyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "key",
		Short: "Manage API keys",
	}
	cmd.AddCommand(keyCreateCmd())
	return cmd
}

func keyCreateCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an inference API key",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			st, err := store.Open(cfg.DBPath)
			if err != nil {
				return err
			}
			defer st.Close()
			plaintext, k, err := st.CreateKey(name)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\n", plaintext)
			fmt.Fprintf(cmd.ErrOrStderr(), "key %q created (id %d); the secret above is shown only once\n", k.Name, k.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "default", "key name")
	return cmd
}
