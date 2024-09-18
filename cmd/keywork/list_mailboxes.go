package main

import (
	"fmt"

	"github.com/rockorager/keywork"
	"github.com/spf13/cobra"
)

func listMailboxesCmd(cmd *cobra.Command, args []string) error {
	enc, dec, err := connect("")
	if err != nil {
		return err
	}
	msg := []interface{}{
		0,
		1,
		"list_mailboxes",
		[]interface{}{},
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
	if method != "list_mailboxes" {
		return fmt.Errorf("rpc error: ")
	}

	argLen, err := dec.DecodeArrayLen()
	if err != nil {
		return err
	}

	for i := 0; i < argLen; i += 1 {
		mbox := keywork.Mailbox{}
		err := dec.Decode(&mbox)
		if err != nil {
			return err
		}
		fmt.Println(mbox.Name)
	}

	return nil
}

func printMailboxes(parent string, mboxes []keywork.Mailbox) {
	for _, mbox := range mboxes {
		if mbox.ParentId == parent {
			fmt.Println(mbox.Name)
			printMailboxes(mbox.Id, mboxes)
		}
	}
}
