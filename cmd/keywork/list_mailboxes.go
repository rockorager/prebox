package main

import (
	"fmt"
	"sort"

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

	mboxes := make([]keywork.Mailbox, 0, argLen)
	for i := 0; i < argLen; i += 1 {
		mbox := keywork.Mailbox{}
		err := dec.Decode(&mbox)
		if err != nil {
			return err
		}
		mboxes = append(mboxes, mbox)
	}

	sort.Slice(mboxes, func(i, j int) bool {
		if mboxes[i].SortOrder != mboxes[j].SortOrder {
			return mboxes[i].SortOrder < mboxes[j].SortOrder
		}
		return mboxes[i].Name < mboxes[j].Name
	})
	printMailboxes("", mboxes)

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
