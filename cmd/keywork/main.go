package main

import (
	"log"
	"os"

	keywork "github.com/rockorager/keywork"
)

func main() {
	log.SetFlags(log.Flags() | log.Lshortfile | log.Lmicroseconds)
	err := run()
	if err != nil {
		log.Printf("error: %v", err)
		os.Exit(1)
	}
}

func run() error {
	cfgs, err := keywork.LoadConfig()
	if err != nil {
		return err
	}
	s := keywork.NewServer(cfgs)
	return s.ListenAndServe()
}
