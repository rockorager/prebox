package main

import (
	"errors"
	"log"
	"os"

	"github.com/rockorager/keywork"
	"github.com/spf13/cobra"
)

var errInvalidResponse = errors.New("invalid response")

const addr = ":2113"

func main() {
	log.SetFlags(log.Flags() | log.Lmicroseconds)
	rootCmd := &cobra.Command{
		Use:   "keywork",
		Short: "Keywork is an email synchronization tool",
		RunE:  run,
	}

	rootCmd.AddCommand(&cobra.Command{
		Use:  "list-remotes",
		RunE: listRemotesCmd,
	})
	rootCmd.AddCommand(&cobra.Command{
		Use:  "list-mailboxes",
		RunE: listMailboxesCmd,
	})
	rootCmd.AddCommand(&cobra.Command{
		Use:  "search",
		RunE: searchCmd,
	})

	err := rootCmd.Execute()
	if err != nil {
		log.Printf("error: %v", err)
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	cfgs, err := keywork.LoadConfig()
	if err != nil {
		return err
	}
	s := keywork.NewServer(cfgs)
	return s.ListenAndServe()
}
