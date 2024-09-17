package keywork

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path"
	"time"

	"git.sr.ht/~rockorager/go-jmap"
	"git.sr.ht/~rockorager/go-jmap/core/push"
	"git.sr.ht/~rockorager/go-jmap/mail"
	"git.sr.ht/~rockorager/go-jmap/mail/email"
	"git.sr.ht/~rockorager/go-jmap/mail/mailbox"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/util"
	"github.com/vmihailenco/msgpack/v5"
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
	db                  *leveldb.DB
	stateChangeDebounce *time.Timer
	emails              map[string]*Email
	name                string
}

func NewJmapClient(name string, url *url.URL) (*JmapClient, error) {
	client := &JmapClient{
		name:   name,
		emails: make(map[string]*Email),
		url:    url,
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	dbPath := path.Join(cacheDir, "keywork", name+".db")
	client.Log("Cache db path: %q", dbPath)
	db, err := leveldb.OpenFile(dbPath, nil)
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
	val, err := c.db.Get(sessionKey, nil)
	switch err {
	case leveldb.ErrNotFound:
		c.Log("Session not in cache. Retrieving from server...")
		// We don't have one, authenticate to retrieve it
		err := c.refreshSession()
		if err != nil {
			return err
		}
	case nil:
		err := json.Unmarshal(val, &c.cl.Session)
		if err != nil {
			return err
		}
	default:
		return err
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
	return c.db.Put(sessionKey, b, nil)
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
			val, _ := c.db.Get(stateMailbox, nil)
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
			val, _ := c.db.Get(stateEmail, nil)
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
	for _, resp := range r.Responses {
		switch arg := resp.Args.(type) {
		case *mailbox.ChangesResponse:
			for _, v := range arg.Destroyed {
				key := append(mailboxPrefix, []byte(v)...)
				err := c.db.Delete(key, nil)
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
				key := append(mailboxPrefix, []byte(v.ID)...)
				mbox := jmapToMsgpackMailbox(v)
				b, err := msgpack.Marshal(mbox)
				if err != nil {
					c.Log("Couldn't encode mailbox: %v", err)
					continue
				}
				err = c.db.Put(key, b, nil)
				if err != nil {
					c.Log("Couldn't cache mailbox: %v", err)
					continue
				}
				// TODO: send mailbox to connections
				_ = mbox
			}
			c.Log("Mailbox state updated to: %q", arg.State)
			c.db.Put(stateMailbox, []byte(arg.State), nil)
		case *email.ChangesResponse:
			for _, v := range arg.Destroyed {
				key := append(emailPrefix, []byte(v)...)
				err := c.db.Delete(key, nil)
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
				c.Log("Updated %d email", len(arg.List))
			} else {
				c.Log("Fetched %d new emails", len(arg.List))
			}
			for _, v := range arg.List {
				var eml Email
				key := append(emailPrefix, []byte(v.ID)...)
				if updated {
					// get the message from the db and
					// update it
					val, err := c.db.Get(key, nil)
					if err != nil {
						c.Log("Couldn't find id %q", string(key))
						continue
					}
					err = msgpack.Unmarshal(val, &eml)
					if err != nil {
						c.Log("Couldn't decode: %s", string(key))
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
				err = c.db.Put(key, b, nil)
				if err != nil {
					c.Log("Couldn't cache email: %v", err)
					continue
				}
				// TODO: send email to connections
			}
			c.Log("Email state updated to: %q", arg.State)
			c.db.Put(stateEmail, []byte(arg.State), nil)
		case *email.QueryResponse:
			c.fetchEmails(acct, arg.IDs)
		}
	}
}

func (c *JmapClient) fetchEmails(acct jmap.ID, ids []jmap.ID) error {
	toFetch := make([]jmap.ID, 0, len(ids))
	opt := &opt.ReadOptions{DontFillCache: true}
	for _, id := range ids {
		key := append(emailPrefix, []byte(id)...)
		val, _ := c.db.Get(key, opt)
		if len(val) > 0 {
			continue
		}
		toFetch = append(toFetch, id)
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
		for _, resp := range r.Responses {
			switch arg := resp.Args.(type) {
			case *email.GetResponse:
				c.Log("Fetched emails: %d of %d", end, len(toFetch))
				for _, v := range arg.List {
					key := append(emailPrefix, []byte(v.ID)...)
					eml := jmapToMsgpackEmail(v)
					b, err := msgpack.Marshal(eml)
					if err != nil {
						c.Log("Couldn't encode email: %v", err)
						continue
					}
					err = c.db.Put(key, b, nil)
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
	}
	if firstState != "" {
		c.Log("Email state updated to: %q", firstState)
		c.db.Put(stateEmail, []byte(firstState), nil)
	}
	return nil
}

func (c *JmapClient) ListMailboxes() ([]Mailbox, error) {
	slice := util.BytesPrefix(mailboxPrefix)
	iter := c.db.NewIterator(slice, nil)
	defer iter.Release()

	result := []Mailbox{}
	for iter.Next() {
		mbox := Mailbox{}
		err := msgpack.Unmarshal(iter.Value(), &mbox)
		if err != nil {
			return result, err
		}
		result = append(result, mbox)
	}
	return result, nil
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
	for k := range v.MailboxIDs {
		keywords = append(keywords, string(k))
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
		Date:       date.Format(time.RFC822Z),
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
