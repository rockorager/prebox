package prebox

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"git.sr.ht/~rockorager/go-jmap"
	"git.sr.ht/~rockorager/go-jmap/core/push"
	"git.sr.ht/~rockorager/go-jmap/mail"
	"git.sr.ht/~rockorager/go-jmap/mail/email"
	"git.sr.ht/~rockorager/go-jmap/mail/mailbox"
	"github.com/vmihailenco/msgpack/v5"
	"go.etcd.io/bbolt"
)

const stateChangeDebounce = 250 * time.Millisecond

var (
	sessionKey    = []byte("00")
	stateMailbox  = []byte("01.mailbox")
	stateEmail    = []byte("01.email")
	mailboxPrefix = []byte("02.")
	emailPrefix   = []byte("03.")
)

var emailProperties = []string{
	"id", "blobId", "mailboxIds", "keywords", "size",
	"receivedAt", "messageId", "inReplyTo", "references", "sender", "from",
	"to", "cc", "replyTo", "subject", "sentAt", "hasAttachment",
}

type JmapClient struct {
	cl                  *jmap.Client
	url                 *url.URL
	db                  *bbolt.DB
	stateChangeDebounce *time.Timer
	name                string
}

func NewJmapClient(name string, url *url.URL) (*JmapClient, error) {
	client := &JmapClient{
		name: name,
		url:  url,
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	err = os.MkdirAll(path.Join(cacheDir, "prebox"), 0o700)
	if err != nil {
		return nil, err
	}
	dbPath := path.Join(cacheDir, "prebox", name+".db")
	client.Log("Cache db path: %q", dbPath)
	db, err := bbolt.Open(dbPath, 0o666, nil)
	if err != nil {
		return nil, err
	}
	client.db = db
	return client, nil
}

func (c *JmapClient) Log(format string, v ...any) {
	message := format
	if len(v) > 0 {
		message = fmt.Sprintf(format, v...)
	}
	log.Printf("[%s] %s", c.name, message)
}

func (c *JmapClient) Name() string {
	return c.name
}

// Connect to the remote server. If the Session object is available in the
// cache, then this only sets up the Listener for remote changes
func (c *JmapClient) Connect() error {
	u := c.url
	c.cl = &jmap.Client{
		SessionEndpoint: "https://" + u.Host + u.Path,
	}
	if u.User.Username() == "" {
		return fmt.Errorf("no username")
	}
	password, hasPassword := u.User.Password()
	switch hasPassword {
	case true:
		c.cl.WithBasicAuth(u.User.Username(), password)
	case false:
		c.cl.WithAccessToken(u.User.Username())
	}

	// Get the session object
	// val, err := c.db.Get(sessionKey, nil)
	var val []byte
	c.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte("state"))
		if bucket == nil {
			return bbolt.ErrBucketNotFound
		}
		val = bucket.Get(sessionKey)
		return nil
	})
	switch len(val) {
	case 0:
		c.Log("Session not in cache. Retrieving from server...")
		// We don't have one, authenticate to retrieve it
		err := c.refreshSession()
		if err != nil {
			return err
		}
	default:
		err := json.Unmarshal(val, &c.cl.Session)
		if err != nil {
			return err
		}
	}

	es := push.EventSource{
		Client:  c.cl,
		Handler: c.debounceStateChange,
	}
	go es.Listen()

	// j.findEmails()
	return nil
}

func (c *JmapClient) primaryAccount() (jmap.ID, bool) {
	if c.cl.Session == nil {
		return "", false
	}
	acct, ok := c.cl.Session.PrimaryAccounts[mail.URI]
	if !ok {
		return "", false
	}
	return acct, true
}

func (c *JmapClient) doRequest(req *jmap.Request) (*jmap.Response, error) {
	r, err := c.cl.Do(req)
	if err != nil {
		return nil, err
	}
	if err := c.maybeRefreshSession(r.SessionState); err != nil {
		return r, err
	}
	return r, nil
}

func (c *JmapClient) maybeRefreshSession(state string) error {
	if c.cl.Session == nil {
		return c.refreshSession()
	}
	if c.cl.Session.State != state {
		return c.refreshSession()
	}
	return nil
}

// Updates the session and saves it in the cache
func (c *JmapClient) refreshSession() error {
	c.cl.Authenticate()
	b, err := json.Marshal(c.cl.Session)
	if err != nil {
		return err
	}
	// Save it in the cache
	return c.db.Update(func(tx *bbolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte("state"))
		if err != nil {
			return err
		}
		return bucket.Put(sessionKey, b)
	})
}

func (c *JmapClient) debounceStateChange(state *jmap.StateChange) {
	if c.stateChangeDebounce != nil {
		if c.stateChangeDebounce.Stop() {
			c.Log("debounced state change")
		}
	}
	c.stateChangeDebounce = time.AfterFunc(stateChangeDebounce, func() {
		c.handleStateChange(state)
	})
}

func (c *JmapClient) handleStateChange(state *jmap.StateChange) {
	c.Log("State change received")
	acct, ok := c.primaryAccount()
	if !ok {
		return
	}

	req := jmap.Request{}

	var emlUpdatedCallId string

	for k, v := range state.Changed[acct] {
		switch k {
		case "Mailbox":
			var val []byte
			c.db.View(func(tx *bbolt.Tx) error {
				bucket := tx.Bucket([]byte("state"))
				if bucket == nil {
					return bbolt.ErrBucketNotFound
				}
				val = bucket.Get(stateMailbox)
				return nil
			})
			mboxState := string(val)
			switch {
			case mboxState == "":
				c.Log("No mailbox state. Fetching all mailboxes...")
				req.Invoke(&mailbox.Get{
					Account: acct,
				})
			case mboxState != v:
				c.Log("Mailbox state change: %q to %q", mboxState, v)
				ch := req.Invoke(&mailbox.Changes{
					Account:    acct,
					SinceState: mboxState,
				})
				req.Invoke(&mailbox.Get{
					Account: acct,
					ReferenceIDs: &jmap.ResultReference{
						ResultOf: ch,
						Name:     "Mailbox/changes",
						Path:     "/created",
					},
				})
				req.Invoke(&mailbox.Get{
					Account: acct,
					ReferenceIDs: &jmap.ResultReference{
						ResultOf: ch,
						Name:     "Mailbox/changes",
						Path:     "/updated",
					},
				})
			case mboxState == v:
				c.Log("No mailbox changes")
			}
		case "Email":
			var val []byte
			c.db.View(func(tx *bbolt.Tx) error {
				bucket := tx.Bucket([]byte("state"))
				if bucket == nil {
					return bbolt.ErrBucketNotFound
				}
				val = bucket.Get(stateEmail)
				return nil
			})
			emlState := string(val)
			switch {
			case emlState == "":
				c.Log("No email state. Fetching all emails...")
				// Query them all
				req.Invoke(&email.Query{
					Account: acct,
				})
			case emlState != v:
				c.Log("Email state change: %q to %q", emlState, v)
				ch := req.Invoke(&email.Changes{
					Account:    acct,
					SinceState: emlState,
				})
				req.Invoke(&email.Get{
					Account: acct,
					ReferenceIDs: &jmap.ResultReference{
						ResultOf: ch,
						Name:     "Email/changes",
						Path:     "/created",
					},
					Properties: emailProperties,
				})
				emlUpdatedCallId = req.Invoke(&email.Get{
					Account: acct,
					ReferenceIDs: &jmap.ResultReference{
						ResultOf: ch,
						Name:     "Email/changes",
						Path:     "/updated",
					},
					Properties: []string{"mailboxIds", "keywords"},
				})
			case emlState == v:
				c.Log("No email changes")
			}
		case "Thread": // Ignore
		case "EmailDelivery": // Ignore
		default:
			c.Log("Unhandled state change: %q", k)
		}
	}

	if len(req.Calls) == 0 {
		return
	}

	r, err := c.doRequest(&req)
	if err != nil {
		c.Log("Client error: %v", err)
		return
	}
	toFetch := []jmap.ID{}
	c.db.Update(func(tx *bbolt.Tx) error {
		mboxBucket, err := tx.CreateBucketIfNotExists(mailboxPrefix)
		if err != nil {
			return err
		}
		emlBucket, err := tx.CreateBucketIfNotExists(emailPrefix)
		if err != nil {
			return err
		}
		stateBucket, err := tx.CreateBucketIfNotExists([]byte("state"))
		if err != nil {
			return err
		}

		for _, resp := range r.Responses {
			switch arg := resp.Args.(type) {
			case *mailbox.ChangesResponse:
				for _, v := range arg.Destroyed {
					err := mboxBucket.Delete([]byte(v))
					if err != nil {
						c.Log("Couldn't delete mailbox: %v", err)
						continue
					}
				}
			case *mailbox.GetResponse:
				if len(arg.List) == 0 {
					continue
				}
				for _, v := range arg.List {
					mbox := jmapToMsgpackMailbox(v)
					b, err := msgpack.Marshal(mbox)
					if err != nil {
						c.Log("Couldn't encode mailbox: %v", err)
						continue
					}
					err = mboxBucket.Put([]byte(v.ID), b)
					if err != nil {
						c.Log("Couldn't cache mailbox: %v", err)
						continue
					}
					// TODO: send mailbox to connections
					_ = mbox
				}
				c.Log("Mailbox state updated to: %q", arg.State)
				stateBucket.Put(stateMailbox, []byte(arg.State))
			case *email.ChangesResponse:
				for _, v := range arg.Destroyed {
					key := []byte(v)
					err := emlBucket.Delete(key)
					if err != nil {
						c.Log("Couldn't delete email: %v", err)
						continue
					}
				}
			case *email.GetResponse:
				if len(arg.List) == 0 {
					continue
				}
				updated := resp.CallID == emlUpdatedCallId
				if updated {
					c.Log("Updated %d emails", len(arg.List))
				} else {
					c.Log("Fetched %d new emails", len(arg.List))
				}
				for _, v := range arg.List {
					var eml Email
					if updated {
						// get the message from the db and
						// update it
						val := emlBucket.Get([]byte(v.ID))
						if len(val) == 0 {
							c.Log("Couldn't find id %q", v.ID)
							continue
						}
						err = msgpack.Unmarshal(val, &eml)
						if err != nil {
							c.Log("Couldn't decode: %s", v.ID)
							continue
						}
						eml.Mailboxes = []string{}
						for id := range v.MailboxIDs {
							eml.Mailboxes = append(eml.Mailboxes, string(id))
						}
						eml.Keywords = []string{}
						for id := range v.Keywords {
							eml.Keywords = append(eml.Keywords, string(id))
						}
					}
					b, err := msgpack.Marshal(eml)
					if err != nil {
						c.Log("Couldn't encode email: %v", err)
						continue
					}
					err = emlBucket.Put([]byte(v.ID), b)
					if err != nil {
						c.Log("Couldn't cache email: %v", err)
						continue
					}
					// TODO: send email to connections
				}
				c.Log("Email state updated to: %q", arg.State)
				stateBucket.Put(stateEmail, []byte(arg.State))
			case *email.QueryResponse:
				toFetch = arg.IDs
			}
		}
		return nil
	})
	if len(toFetch) > 0 {
		err := c.fetchEmails(acct, toFetch)
		if err != nil {
			c.Log("error: %v", err)
		}
	}
}

func (c *JmapClient) fetchEmails(acct jmap.ID, ids []jmap.ID) error {
	toFetch := make([]jmap.ID, 0, len(ids))
	err := c.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(emailPrefix)
		if bucket == nil {
			return bbolt.ErrBucketNotFound
		}
		for _, id := range ids {
			val := bucket.Get([]byte(id))
			if len(val) > 0 {
				continue
			}
			toFetch = append(toFetch, id)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(toFetch) == 0 {
		return nil
	}
	c.Log("Fetching %d emails", len(toFetch))

	// Holds the first state we see
	firstState := ""

	i := 0
	// Fastmail limit. We could look this up in the Session object but lets
	// just hardcode it for now
	maxPerBatch := 4096
	for i < len(toFetch) {
		end := min(i+maxPerBatch, len(toFetch))
		batch := ids[i:end]
		i = end
		req := jmap.Request{}
		req.Invoke(&email.Get{
			Account:    acct,
			IDs:        batch,
			Properties: emailProperties,
		})
		r, err := c.doRequest(&req)
		if err != nil {
			return err
		}
		err = c.db.Update(func(tx *bbolt.Tx) error {
			emlBucket, err := tx.CreateBucketIfNotExists(emailPrefix)
			if err != nil {
				return err
			}
			stateBucket, err := tx.CreateBucketIfNotExists([]byte("state"))
			if err != nil {
				return err
			}
			for _, resp := range r.Responses {
				switch arg := resp.Args.(type) {
				case *email.GetResponse:
					c.Log("Fetched emails: %d of %d", end, len(toFetch))
					for _, v := range arg.List {
						eml := jmapToMsgpackEmail(v)
						b, err := msgpack.Marshal(eml)
						if err != nil {
							c.Log("Couldn't encode email: %v", err)
							continue
						}
						err = emlBucket.Put([]byte(v.ID), b)
						if err != nil {
							c.Log("Couldn't cache email: %v", err)
							continue
						}
					}
					if firstState == "" {
						firstState = arg.State
					}
				}
			}
			if end >= len(toFetch) {
				// Save state in the last request
				c.Log("Email state updated to: %q", firstState)
				stateBucket.Put(stateEmail, []byte(firstState))
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *JmapClient) ListMailboxes() ([]Mailbox, error) {
	result := []Mailbox{}
	err := c.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(mailboxPrefix)
		if bucket == nil {
			return bbolt.ErrBucketNotFound
		}
		bucket.ForEach(func(k, v []byte) error {
			mbox := Mailbox{}
			err := msgpack.Unmarshal(v, &mbox)
			if err != nil {
				return err
			}
			result = append(result, mbox)
			return nil
		})
		return nil
	})
	if err != nil {
		return []Mailbox{}, err
	}

	// Sort by sort by sort order and name
	sort.Slice(result, func(i, j int) bool {
		if result[i].SortOrder != result[j].SortOrder {
			return result[i].SortOrder < result[j].SortOrder
		}
		return result[i].Name < result[j].Name
	})

	r2 := make([]Mailbox, 0, len(result))
	// Sort as a tree
	result = sortMailboxTree("", result, r2)

	return result, nil
}

func sortMailboxTree(parent string, mboxes []Mailbox, result []Mailbox) []Mailbox {
	for _, mbox := range mboxes {
		if mbox.ParentId == parent {
			result = append(result, mbox)
			result = sortMailboxTree(mbox.Id, mboxes, result)
		}
	}
	return result
}

func jmapToMsgpackMailbox(jmbox *mailbox.Mailbox) Mailbox {
	return Mailbox{
		Name:      jmbox.Name,
		Id:        string(jmbox.ID),
		ParentId:  string(jmbox.ParentID),
		Role:      string(jmbox.Role),
		Total:     uint(jmbox.TotalEmails),
		Unread:    uint(jmbox.UnreadEmails),
		SortOrder: uint(jmbox.SortOrder),
	}
}

func jmapToMsgpackEmail(v *email.Email) Email {
	mboxes := make([]string, 0, len(v.MailboxIDs))
	for k := range v.MailboxIDs {
		mboxes = append(mboxes, string(k))
	}
	keywords := make([]string, 0, len(v.Keywords))
	for k := range v.Keywords {
		keywords = append(keywords, k)
	}
	inReplyTo := ""
	if len(v.InReplyTo) > 0 {
		inReplyTo = v.InReplyTo[0]
	}
	messageId := ""
	if len(v.MessageID) > 0 {
		messageId = v.MessageID[0]
	}
	date := v.ReceivedAt
	if v.SentAt != nil {
		date = v.SentAt
	}
	eml := Email{
		Id:         string(v.ID),
		From:       jmapToMsgpackAddressList(v.From),
		To:         jmapToMsgpackAddressList(v.To),
		Cc:         jmapToMsgpackAddressList(v.CC),
		Bcc:        jmapToMsgpackAddressList(v.BCC),
		ReplyTo:    jmapToMsgpackAddressList(v.ReplyTo),
		Subject:    v.Subject,
		Mailboxes:  mboxes,
		Date:       date.Format(time.RFC3339),
		Keywords:   keywords,
		References: v.References,
		InReplyTo:  inReplyTo,
		MessageId:  messageId,
		Size:       uint(v.Size),
	}
	return eml
}

func jmapToMsgpackAddressList(jlist []*mail.Address) []Address {
	result := make([]Address, 0, len(jlist))
	for _, jaddr := range jlist {
		addr := Address{
			Name:  jaddr.Name,
			Email: jaddr.Email,
		}
		result = append(result, addr)
	}
	return result
}

func (c *JmapClient) Search(query []string) ([]Email, error) {
	start := time.Now()
	s, err := c.parseSearch(query)
	if err != nil {
		return []Email{}, err
	}
	count := 0
	result := []Email{}
	err = c.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(emailPrefix)
		if bucket == nil {
			return bbolt.ErrBucketNotFound
		}
		if err != nil {
			return err
		}
		bucket.ForEach(func(k, v []byte) error {
			count += 1
			eml := Email{}
			err := msgpack.Unmarshal(v, &eml)
			if err != nil {
				return err
			}
			if s.Matches(&eml) {
				result = append(result, eml)
			}
			return nil
		})
		return nil
	})
	if err != nil {
		return []Email{}, err
	}
	c.Log("Search: elapsed=%s, candidates=%d, results=%d", time.Since(start), count, len(result))

	return result, nil
}

func (c *JmapClient) parseSearch(args []string) (SearchCriteria, error) {
	s := SearchCriteria{}
	for _, arg := range args {
		prefix, term, found := strings.Cut(arg, ":")
		if !found {
			continue
		}
		switch prefix {
		case "in":
			mboxes, err := c.ListMailboxes()
			if err != nil {
				return s, err
			}
			for _, mbox := range mboxes {
				if mbox.Name == term {
					s.InMailbox = mbox.Id
					break
				}
			}
		}
	}
	return s, nil
}
