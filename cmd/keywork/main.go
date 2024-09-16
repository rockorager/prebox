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
	s := keywork.NewServer()
	return s.ListenAndServe()
}
