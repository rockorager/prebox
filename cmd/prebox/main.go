package main

import (
	"errors"
	"log"
	"os"

	"github.com/rockorager/prebox"
	"github.com/spf13/cobra"
)

var errInvalidResponse = errors.New("invalid response")

const addr = ":2113"

func main() {
	log.SetFlags(log.Flags() | log.Lmicroseconds | log.Lshortfile)
	rootCmd := &cobra.Command{
		Use:   "prebox",
		Short: "prebox is an email synchronization tool",
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
	rootCmd.AddCommand(newSearchCmd())

	err := rootCmd.Execute()
	if err != nil {
		log.Printf("error: %v", err)
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	cfgs, err := prebox.LoadConfig()
	if err != nil {
		return err
	}
	s := prebox.NewServer(cfgs)
	return s.ListenAndServe()
}
