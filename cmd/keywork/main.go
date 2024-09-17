package main

import (
	"fmt"
	"log"
	"net"
	"os"

	"github.com/rockorager/keywork"
	"github.com/spf13/cobra"
	"github.com/vmihailenco/msgpack/v5"
)

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

func listRemotesCmd(cmd *cobra.Command, args []string) error {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	enc := msgpack.NewEncoder(conn)
	dec := msgpack.NewDecoder(conn)
	remotes, err := listRemotes(enc, dec)
	if err != nil {
		return err
	}
	for _, remote := range remotes {
		fmt.Println(remote)
	}
	return nil
}

func listRemotes(enc *msgpack.Encoder, dec *msgpack.Decoder) ([]string, error) {
	msg := []interface{}{
		0,
		0,
		"list_remotes",
		[]interface{}{},
	}
	if err := enc.Encode(msg); err != nil {
		return []string{}, err
	}

	resp, err := dec.DecodeSlice()
	if err != nil {
		return []string{}, err
	}
	m, ok := resp[3].([]interface{})
	if !ok {
		return []string{}, fmt.Errorf("invalid response")
	}
	result := make([]string, 0, len(m))
	for _, v := range m {

		v, ok := v.(string)
		if !ok {
			return []string{}, fmt.Errorf("invalid response")
		}
		result = append(result, v)
	}
	return result, nil
}

func listMailboxesCmd(cmd *cobra.Command, args []string) error {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	enc := msgpack.NewEncoder(conn)
	dec := msgpack.NewDecoder(conn)
	remotes, err := listRemotes(enc, dec)
	if err != nil {
		return err
	}
	var remote string
	switch len(remotes) {
	case 0:
		return fmt.Errorf("no remotes")
	case 1:
		remote = remotes[0]
	default:
		return fmt.Errorf("must specify a remote")
	}

	msg := []interface{}{
		0,
		2,
		"connect",
		[]interface{}{
			remote,
		},
	}
	if err := enc.Encode(msg); err != nil {
		return err
	}
	resp, err := dec.DecodeSlice()
	if err != nil {
		return err
	}
	if len(resp) != 4 {
		return fmt.Errorf("invalid response")
	}
	method, ok := resp[2].(string)
	if !ok {
		return fmt.Errorf("invalid response")
	}
	if method != "connect" {
		return fmt.Errorf("rpc error: %v", resp[3])
	}
	msg = []interface{}{
		0,
		2,
		"list_mailboxes",
		[]interface{}{},
	}
	if err := enc.Encode(msg); err != nil {
		return err
	}
	resp, err = dec.DecodeSlice()
	if err != nil {
		return err
	}
	if len(resp) != 4 {
		return fmt.Errorf("invalid response")
	}
	method, ok = resp[2].(string)
	if !ok {
		return fmt.Errorf("invalid response")
	}
	if method != "list_mailboxes" {
		return fmt.Errorf("rpc error: %v", resp[3])
	}

	m, ok := resp[3].([]interface{})
	if !ok {
		return fmt.Errorf("invalid response")
	}
	for _, v := range m {
		v, ok := v.(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid repsonse")
		}
		fmt.Println(v["name"])
	}
	return nil
}
