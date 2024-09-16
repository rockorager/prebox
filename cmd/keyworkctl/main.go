package main

import (
	"log"
	"net"
	"os"

	"github.com/vmihailenco/msgpack/v5"
)

const addr = ":2113"

func main() {
	err := run()
	if err != nil {
		log.Printf("error: %v", err)
		os.Exit(1)
	}
}

func run() error {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	enc := msgpack.NewEncoder(conn)
	return ping(enc)
}

func ping(enc *msgpack.Encoder) error {
	msg := []interface{}{
		0,
		2,
		"connect",
		[]interface{}{
			"abc",
			"name",
		},
	}

	return enc.Encode(msg)
}
