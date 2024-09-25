// thread is an implementaiton of jwz.org/doc/threading.html
package prebox

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rockorager/prebox/log"
)

type ThreadedEmail struct {
	Email   `msgpack:"email"`
	parent  *ThreadedEmail   `msgpack:"parent,omitempty"`
	Replies []*ThreadedEmail `msgpack:"replies"`
}

func thread(emls []Email) []*ThreadedEmail {
	start := time.Now()
	rootSet := buildRootSet(emls)
	pruneSet(rootSet)
	for _, item := range rootSet {
		sortSiblings(item.Replies)
	}
	log.Debug("threaded %d emails in %s ", len(emls), time.Since(start))
	return rootSet
}

func sortSiblings(siblings []*ThreadedEmail) {
	sort.Slice(siblings, func(i, j int) bool {
		return siblings[i].Email.Date < siblings[j].Email.Date
	})
	for _, item := range siblings {
		sortSiblings(item.Replies)
	}
}

func buildRootSet(emls []Email) []*ThreadedEmail {
	idTable := make(map[string]*ThreadedEmail, len(emls))
	idList := make([]*ThreadedEmail, 0, len(emls))
	for _, eml := range emls {
		// See if we have a container already
		c, ok := idTable[eml.MessageId]
		if !ok {
			// If we don't, create a new one
			c = &ThreadedEmail{}
			idTable[eml.MessageId] = c
			idList = append(idList, c)
		}
		// Assign this message to the container
		c.Email = eml

		var (
			parent *ThreadedEmail
			child  *ThreadedEmail
		)
		// Connect the references
		for _, ref := range eml.References {
			child, ok = idTable[ref]
			if !ok {
				child = &ThreadedEmail{}
				idTable[ref] = child
				idList = append(idList, child)
			}
			link(parent, child)
			parent = child
		}
		unlink(c)
		link(parent, c)
	}
	// best guess
	rootSet := make([]*ThreadedEmail, 0, len(emls)/2)
	for _, v := range idList {
		if v.parent == nil {
			rootSet = append(rootSet, v)
		}
	}
	return rootSet
}

func print(c *ThreadedEmail, depth int) {
	if c.Email.Id != "" {
		fmt.Printf("%s%s\r\n", strings.Repeat("  ", depth), c.Email.Subject)
	} else {
		fmt.Printf("%s[dummy]\r\n", strings.Repeat("  ", depth))
	}
	for _, child := range c.Replies {
		print(child, depth+1)
	}
}

func hasChild(parent *ThreadedEmail, maybeChild *ThreadedEmail) bool {
	if parent == nil || maybeChild == nil {
		return false
	}
	for _, child := range parent.Replies {
		if child == maybeChild {
			return true
		}
		if hasChild(child, maybeChild) {
			return true
		}
	}
	return false
}

// Adds child to parent and sets parent as child's parent. If the child already
// has a parent, this is a noop
func link(parent *ThreadedEmail, child *ThreadedEmail) {
	// don't link to self
	if parent == child {
		return
	}
	// don't break existing links
	if child.parent != nil {
		return
	}
	// nothing to link
	if parent == nil {
		return
	}

	// don't create a loop
	if hasChild(parent, child) {
		return
	}
	if hasChild(child, parent) {
		return
	}

	child.parent = parent
	parent.Replies = append(parent.Replies, child)
}

// Removes child from it's parents' tree, and sets parent to nil
func unlink(child *ThreadedEmail) {
	if child.parent == nil {
		return
	}
	parent := child.parent
	for i, c := range parent.Replies {
		if c == child {
			parent.Replies = append(parent.Replies[:i], parent.Replies[i+1:]...)
			child.parent = nil
			return
		}
	}
	panic("child not in list")
}

func pruneSet(set []*ThreadedEmail) []*ThreadedEmail {
	for i, item := range set {
		item.parent = nil
		replacement := prune(item)
		if replacement != nil {
			set[i] = replacement
		}
		if item.Replies == nil {
			continue
		}
		item.Replies = pruneSet(item.Replies)
	}
	return set
}

// removes empty containers. The return value is only valid if the container was
// an empty container with no parent and one child
func prune(c *ThreadedEmail) *ThreadedEmail {
	// we only prune empty containers
	if c.Email.Id != "" {
		return nil
	}
	children := pruneSet(c.Replies)
	// nuke it
	if len(children) == 0 {
		unlink(c)
		return nil
	}
	// prune the children
	// replace it
	if c.parent == nil && len(children) == 1 {
		return children[0]
	}
	if c.parent == nil {
		return nil
	}
	// splice children into parent
	parent := c.parent
	for _, child := range parent.Replies {
		child.parent = parent
	}
	for i, child := range parent.Replies {
		if child == c {
			newChildren := append(parent.Replies[:i], children...)
			parent.Replies = append(newChildren, parent.Replies[i+1:]...)
			c.parent = nil
			return nil
		}
	}
	return nil
}
