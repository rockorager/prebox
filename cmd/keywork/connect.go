package main

import (
	"bufio"
	"fmt"
	"net"

	"github.com/vmihailenco/msgpack/v5"
)

func connect(remote string) (*msgpack.Encoder, *msgpack.Decoder, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, nil, err
	}
	rdr := bufio.NewReader(conn)
	enc := msgpack.NewEncoder(conn)
	dec := msgpack.NewDecoder(rdr)
	if remote == "" {
		remotes, err := listRemotes(enc, dec)
		if err != nil {
			return nil, nil, err
		}
		switch len(remotes) {
		case 0:
			return nil, nil, fmt.Errorf("no remotes")
		case 1:
			remote = remotes[0]
		default:
			return nil, nil, fmt.Errorf("must specify a remote")
		}
	}

	msg := []interface{}{
		0,
		0,
		"connect",
		[]interface{}{
			remote,
		},
	}
	if err := enc.Encode(msg); err != nil {
		return nil, nil, err
	}
	resp, err := dec.DecodeSlice()
	if err != nil {
		return nil, nil, err
	}
	if len(resp) != 4 {
		return nil, nil, errInvalidResponse
	}
	method, ok := resp[2].(string)
	if !ok {
		return nil, nil, errInvalidResponse
	}
	if method != "connect" {
		return nil, nil, fmt.Errorf("rpc error: %v", resp[3])
	}
	return enc, dec, nil
}
