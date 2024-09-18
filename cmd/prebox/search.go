package main

import (
	"fmt"

	"github.com/rockorager/prebox"
	"github.com/spf13/cobra"
)

func searchCmd(cmd *cobra.Command, args []string) error {
	enc, dec, err := connect("")
	if err != nil {
		return err
	}
	msg := []interface{}{
		0,
		1,
		"search",
		args,
	}
	if err := enc.Encode(msg); err != nil {
		return err
	}
	l, err := dec.DecodeArrayLen()
	if err != nil {
		return err
	}
	if l != 4 {
		return errInvalidResponse
	}

	code, err := dec.DecodeUint()
	if err != nil {
		return err
	}
	if code != 1 {
		return errInvalidResponse
	}
	id, err := dec.DecodeUint()
	if err != nil {
		return err
	}
	if id != 1 {
		return errInvalidResponse
	}
	method, err := dec.DecodeString()
	if err != nil {
		return err
	}
	if method != "search" {
		return fmt.Errorf("rpc error: ")
	}

	argLen, err := dec.DecodeArrayLen()
	if err != nil {
		return err
	}

	emls := make([]prebox.Email, 0, argLen)
	for i := 0; i < argLen; i += 1 {
		eml := prebox.Email{}
		err := dec.Decode(&eml)
		if err != nil {
			return err
		}
		emls = append(emls, eml)
	}
	for _, eml := range emls {
		fmt.Printf("(%s) %s\n", eml.Date, eml.Subject)
	}
	return nil
}
