package prebox

import (
	"time"
)

type SearchCriteria struct {
	InMailbox          string
	InMailboxOtherThan []string
	Before             time.Time
	After              time.Time
	HasKeyword         string
	And                []SearchCriteria
	Or                 []SearchCriteria
	Not                []SearchCriteria
}

func (s SearchCriteria) Matches(msg *Email) bool {
	if !s.inMailbox(msg) {
		return s.or(msg)
	}
	if !s.inMailboxOtherThan(msg) {
		return s.or(msg)
	}
	if !s.before(msg) {
		return s.or(msg)
	}
	if !s.after(msg) {
		return s.or(msg)
	}
	if !s.hasKeyword(msg) {
		return s.or(msg)
	}
	for _, and := range s.And {
		if !and.Matches(msg) {
			return s.or(msg)
		}
	}
	for _, not := range s.Not {
		if not.Matches(msg) {
			return s.or(msg)
		}
	}
	return true
}

func (s SearchCriteria) or(msg *Email) bool {
	for _, or := range s.Or {
		if or.Matches(msg) {
			return true
		}
	}
	return false
}

// Returns true if the message is in the mailbox, or if the mailbox criteria is
// empty
func (s SearchCriteria) inMailbox(msg *Email) bool {
	if s.InMailbox == "" {
		return true
	}
	for _, mbox := range msg.Mailboxes {
		if s.InMailbox == mbox {
			return true
		}
	}
	return false
}

// Returns true if the message is in a mailbox other than the mailboxes listed
// in InMailboxOtherThan
func (s SearchCriteria) inMailboxOtherThan(msg *Email) bool {
	if len(s.InMailboxOtherThan) == 0 {
		return true
	}
	if len(msg.Mailboxes) > len(s.InMailboxOtherThan) {
		// We definitely are true
		return true
	}
	// Are all of the mailboxes listed in inMailboxOtherThan?
	for _, mbox := range msg.Mailboxes {
		for _, other := range s.InMailboxOtherThan {
			if mbox != other {
				// We are in a different mailbox
				return true
			}
		}
	}
	return false
}

func (s SearchCriteria) before(msg *Email) bool {
	if s.Before.IsZero() {
		return true
	}
	t, _ := time.Parse(time.RFC3339, msg.Date)
	return t.Before(s.Before)
}

func (s SearchCriteria) after(msg *Email) bool {
	if s.After.IsZero() {
		return true
	}
	t, _ := time.Parse(time.RFC3339, msg.Date)
	return t.After(s.After)
}

func (s SearchCriteria) hasKeyword(msg *Email) bool {
	if s.HasKeyword == "" {
		return true
	}
	for _, keyword := range msg.Keywords {
		if s.HasKeyword == keyword {
			return true
		}
	}
	return false
}
