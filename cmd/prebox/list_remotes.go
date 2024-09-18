package main

import (
	"fmt"
	"net"

	"github.com/spf13/cobra"
	"github.com/vmihailenco/msgpack/v5"
)

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
