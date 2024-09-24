package main

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/rockorager/prebox"
	"github.com/spf13/cobra"
)

func newSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:  "search",
		RunE: searchCmd,
	}
	cmd.Flags().BoolP("reverse", "r", false, "reverse sort order (descending)")
	cmd.Flags().Bool("threads", false, "print search result as threads")
	cmd.Flags().Bool("count", false, "print the number of matches")
	cmd.Flags().IntP("limit", "l", -1, "limit number of results returned")
	cmd.Flags().Uint("offset", 0, "offset returned results by this amount")
	return cmd
}

func searchCmd(cmd *cobra.Command, args []string) error {
	reverse, err := cmd.Flags().GetBool("reverse")
	if err != nil {
		return err
	}
	limit, err := cmd.Flags().GetInt("limit")
	if err != nil {
		return err
	}
	offset, err := cmd.Flags().GetUint("offset")
	if err != nil {
		return err
	}
	threads, err := cmd.Flags().GetBool("threads")
	if err != nil {
		return err
	}
	count, err := cmd.Flags().GetBool("count")
	if err != nil {
		return err
	}

	if count {
		limit = 0
	}

	enc, dec, err := connect("")
	if err != nil {
		return err
	}

	methodName := "search"
	if threads {
		methodName = "threads"
	}

	msg := []interface{}{
		0,
		1,
		methodName,
		[]interface{}{
			limit,
			offset,
			args,
		},
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
	if method != methodName {
		return fmt.Errorf("rpc error: invalid method. Expected %s. Got %s", methodName, method)
	}

	argLen, err := dec.DecodeArrayLen()
	if err != nil {
		return err
	}

	if argLen != 2 {
		return fmt.Errorf("invalid response: expected 2 arguments. Got %d", argLen)
	}

	total, err := dec.DecodeUint()
	if err != nil {
		return err
	}

	resultLen, err := dec.DecodeArrayLen()
	if err != nil {
		return err
	}

	emls := make([]*prebox.ThreadedEmail, 0, resultLen)
	for i := 0; i < resultLen; i += 1 {
		eml := &prebox.ThreadedEmail{}
		err := dec.Decode(eml)
		if err != nil {
			return err
		}
		emls = append(emls, eml)
	}
	if count {
		fmt.Println(total)
		return nil
	}
	if reverse {
		slices.Reverse(emls)
	}
	printThread(emls, 0)
	return nil
}

func printThread(emls []*prebox.ThreadedEmail, depth int) {
	for _, eml := range emls {
		fmt.Print(strings.Repeat("  ", depth))
		if eml.Email == nil {
			fmt.Println("[dummy]")
			printThread(eml.Replies, depth+1)
			continue
		}
		date := time.Unix(eml.Date, 0)
		now := time.Now()
		var dateStr string
		switch {
		case now.Before(date.Add(7 * 24 * time.Hour)):
			dateStr = date.Local().Format("Mon 3:04PM")
		case now.Before(date.Add(6 * 7 * 24 * time.Hour)):
			dateStr = date.Local().Format("Jan 2 3:04PM")
		default:
			dateStr = date.Local().Format(time.DateOnly)
		}
		name := ""
		if len(eml.From) > 0 {
			name = eml.From[0].Name
			if name == "" {
				name = eml.From[0].Email
			}
		}
		flag := ""
		if !slices.Contains(eml.Keywords, "$seen") {
			flag = "🔵"
		}
		fmt.Printf("\x1b[35m%s %s\x1b[34m%s\x1b[0m %s\n", dateStr, flag, name, eml.Subject)
		printThread(eml.Replies, depth+1)
	}
}
