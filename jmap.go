package prebox

import (
	"encoding/json"
	"fmt"
	"math"
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
	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/rockorager/prebox/log"
	"github.com/rockorager/zig4go/assert"
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
	"textBody", "bodyValues",
}

var emailBodyProperties = []string{"partId", "type"}

type indexedEmail struct {
	Type string
	Body string
}

type JmapClient struct {
	cl                  *jmap.Client
	url                 *url.URL
	db                  *bbolt.DB
	stateChangeDebounce *time.Timer
	index               bleve.Index
	name                string
}

func NewJmapClient(name string, url *url.URL) (*JmapClient, error) {
	assert.True(url != nil, "url was nil")
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
	log.Trace("[%s] Cache db path: %q", client.name, dbPath)
	db, err := bbolt.Open(dbPath, 0o666, nil)
	if err != nil {
		return nil, err
	}

	indexPath := path.Join(cacheDir, "prebox", name+"_index.db")
	client.index, err = bleve.Open(indexPath)
	if err == bleve.ErrorIndexPathDoesNotExist {
		mapping, err := newIndexMapping()
		if err != nil {
			return nil, err
		}
		client.index, err = bleve.New(indexPath, mapping)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	client.db = db
	return client, nil
}

func newIndexMapping() (mapping.IndexMapping, error) {
	englishTextFieldMapping := bleve.NewTextFieldMapping()
	englishTextFieldMapping.Analyzer = "en"
	englishTextFieldMapping.Store = false

	keywordFieldMapping := bleve.NewKeywordFieldMapping()
	keywordFieldMapping.Store = false

	dateFieldMapping := bleve.NewDateTimeFieldMapping()
	dateFieldMapping.DateFormat = optional.Name
	dateFieldMapping.Store = false

	numericMapping := bleve.NewNumericFieldMapping()
	numericMapping.Store = false

	emailMapping := bleve.NewDocumentMapping()
	emailMapping.StructTagKey = "msgpack"
	emailMapping.AddFieldMappingsAt("subject", englishTextFieldMapping)
	emailMapping.AddFieldMappingsAt("from.name", englishTextFieldMapping)
	emailMapping.AddFieldMappingsAt("from.email", englishTextFieldMapping)
	emailMapping.AddFieldMappingsAt("to.name", englishTextFieldMapping)
	emailMapping.AddFieldMappingsAt("to.email", englishTextFieldMapping)
	emailMapping.AddFieldMappingsAt("cc.name", englishTextFieldMapping)
	emailMapping.AddFieldMappingsAt("cc.email", englishTextFieldMapping)
	emailMapping.AddFieldMappingsAt("bcc.name", englishTextFieldMapping)
	emailMapping.AddFieldMappingsAt("bcc.email", englishTextFieldMapping)
	emailMapping.AddFieldMappingsAt("reply_to.name", englishTextFieldMapping)
	emailMapping.AddFieldMappingsAt("reply_to.email", englishTextFieldMapping)
	emailMapping.AddFieldMappingsAt("body", englishTextFieldMapping)

	emailMapping.AddFieldMappingsAt("date", dateFieldMapping)
	emailMapping.AddFieldMappingsAt("size", numericMapping)

	emailMapping.AddFieldMappingsAt("type", keywordFieldMapping)
	emailMapping.AddFieldMappingsAt("message_id", keywordFieldMapping)
	emailMapping.AddFieldMappingsAt("references", keywordFieldMapping)

	emailMapping.AddFieldMappingsAt("mailbox_ids", keywordFieldMapping)
	emailMapping.AddFieldMappingsAt("keywords", keywordFieldMapping)

	mapping := bleve.NewIndexMapping()
	mapping.TypeField = "type"
	mapping.DefaultAnalyzer = "en"

	mapping.AddDocumentMapping("email", emailMapping)

	return mapping, nil
}

func (c *JmapClient) Name() string {
	return c.name
}

// Connect to the remote server. If the Session object is available in the
// cache, then this only sets up the Listener for remote changes
func (c *JmapClient) Connect() error {
	assert.True(c.url != nil, "url is nil")
	assert.True(c.url.Host != "", "host is empty")
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
			return nil
		}
		val = bucket.Get(sessionKey)
		return nil
	})
	switch len(val) {
	case 0:
		log.Info("[%s] Session not in cache. Retrieving from server...", c.name)
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

// returns the primary email account. Asserts that the account exists
func (c *JmapClient) primaryAccount() jmap.ID {
	assert.True(c.cl != nil, "client is nil")
	acct := c.cl.Session.PrimaryAccounts[mail.URI]
	assert.True(acct != "", "no mail account")
	return acct
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
	assert.True(c.cl != nil, "client is nil")
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
			log.Trace("[%s] Debounced state change", c.name)
		}
	}
	c.stateChangeDebounce = time.AfterFunc(stateChangeDebounce, func() {
		c.handleStateChange(state)
	})
}

func (c *JmapClient) handleStateChange(state *jmap.StateChange) {
	log.Trace("[%s] State change received", c.name)
	acct := c.primaryAccount()

	req := jmap.Request{}

	var emlUpdatedCallId string

	for k, v := range state.Changed[acct] {
		switch k {
		case "Mailbox":
			var val []byte
			c.db.View(func(tx *bbolt.Tx) error {
				bucket := tx.Bucket([]byte("state"))
				assert.True(bucket != nil, "state bucket is nil")
				val = bucket.Get(stateMailbox)
				return nil
			})
			mboxState := string(val)
			switch {
			case mboxState == "":
				log.Trace("[%s] No mailbox state. Fetching all mailboxes...", c.name)
				req.Invoke(&mailbox.Get{
					Account: acct,
				})
			case mboxState != v:
				log.Trace("[%s] Mailbox state change: %q to %q", c.name, mboxState, v)
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
				log.Trace("[%s] No Mailbox changes", c.name)
			}
		case "Email":
			var val []byte
			c.db.View(func(tx *bbolt.Tx) error {
				bucket := tx.Bucket([]byte("state"))
				assert.True(bucket != nil, "state bucket is nil")
				val = bucket.Get(stateEmail)
				return nil
			})
			emlState := string(val)
			switch {
			case emlState == "":
				log.Trace("[%s] No email state. Fetching all emails...", c.name)
				// Query them all
				req.Invoke(&email.Query{
					Account: acct,
				})
			case emlState != v:
				log.Trace("[%s] Email state change: %q to %q", c.name, emlState, v)
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
				log.Trace("[%s] No email changes", c.name)
			}
		case "Thread": // Ignore
		case "EmailDelivery": // Ignore
		default:
			log.Info("[%s] Unhandled state change: %q", c.name, k)
		}
	}

	if len(req.Calls) == 0 {
		return
	}

	r, err := c.doRequest(&req)
	if err != nil {
		log.Error("[%s] Client error: %v", err)
		return
	}
	toFetch := []jmap.ID{}
	c.db.Update(func(tx *bbolt.Tx) error {
		// These can only error in a catastrophic way
		mboxBucket, err := tx.CreateBucketIfNotExists(mailboxPrefix)
		assert.True(err == nil, "couldn't create bucket: %v", err)

		emlBucket, err := tx.CreateBucketIfNotExists(emailPrefix)
		assert.True(err == nil, "couldn't create bucket: %v", err)

		stateBucket, err := tx.CreateBucketIfNotExists([]byte("state"))
		assert.True(err == nil, "couldn't create bucket: %v", err)

		for _, resp := range r.Responses {
			switch arg := resp.Args.(type) {
			case *mailbox.ChangesResponse:
				for _, v := range arg.Destroyed {
					err := mboxBucket.Delete([]byte(v))
					if err != nil {
						log.Error("[%s] Couldn't delete mailbox: %v", c.name, err)
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
						log.Error("[%s] Couldn't encode mailbox: %v", c.name, err)
						continue
					}
					err = mboxBucket.Put([]byte(v.ID), b)
					if err != nil {
						log.Error("[%s] Couldn't cache mailbox: %v", c.name, err)
						continue
					}
					// TODO: send mailbox to connections
					_ = mbox
				}
				if err := stateBucket.Put(stateMailbox, []byte(arg.State)); err != nil {
					log.Error("[%s] Couldn't update mailbox state: %v", err)
					continue
				}
				log.Trace("[%s] Mailbox state updated to: %q", c.name, arg.State)
			case *email.ChangesResponse:
				for _, v := range arg.Destroyed {
					key := []byte(v)
					err := emlBucket.Delete(key)
					if err != nil {
						log.Error("[%s] Couldn't delete email: %v", c.name, err)
						continue
					}
				}
			case *email.GetResponse:
				if len(arg.List) == 0 {
					continue
				}
				updated := resp.CallID == emlUpdatedCallId
				if updated {
					log.Trace("[%s] Updated %d emails", c.name, len(arg.List))
				} else {
					log.Trace("[%s] Fetched %d new emails", c.name, len(arg.List))
				}
				for _, v := range arg.List {
					var eml Email
					if updated {
						// get the message from the db and
						// update it
						val := emlBucket.Get([]byte(v.ID))
						if len(val) == 0 {
							log.Error("[%s] Couldn't find id %q", c.name, v.ID)
							continue
						}
						err = msgpack.Unmarshal(val, &eml)
						if err != nil {
							log.Error("[%s] Couldn't decode: %q", c.name, err)
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
						log.Error("[%s] Couldn't encode email: %s", c.name, err)
						continue
					}
					err = emlBucket.Put([]byte(v.ID), b)
					if err != nil {
						log.Error("[%s] Couldn't cache email: %s", c.name, err)
						continue
					}
					// TODO: send email to connections
				}
				log.Trace("[%s] Email state updated to: %q", c.name, arg.State)
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
			log.Error("[%s] %v", c.name, err)
		}
	}
}

func (c *JmapClient) fetchEmails(acct jmap.ID, ids []jmap.ID) error {
	toFetch := make([]jmap.ID, 0, len(ids))
	assert.True(c.db != nil, "db is nil")
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
	log.Trace("[%s] Fetching %d emails", c.name, len(toFetch))

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
			Account:             acct,
			IDs:                 batch,
			Properties:          emailProperties,
			FetchTextBodyValues: true,
			BodyProperties:      emailBodyProperties,
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
			batch := c.index.NewBatch()
			for _, resp := range r.Responses {
				switch arg := resp.Args.(type) {
				case *email.GetResponse:
					log.Trace("[%s] Fetched %d new emails", c.name, end, len(toFetch))
					for _, v := range arg.List {
						eml := jmapToMsgpackEmail(v)
						b, err := msgpack.Marshal(eml)
						if err != nil {
							log.Error("[%s] Couldn't encode email: %v", c.name, err)
							continue
						}
						err = emlBucket.Put([]byte(v.ID), b)
						if err != nil {
							log.Error("[%s] Couldn't cache email: %v", c.name, err)
							continue
						}
						if len(v.TextBody) == 0 {
							continue
						}
						part := v.TextBody[0]
						value, ok := v.BodyValues[part.PartID]
						if !ok {
							continue
						}
						switch part.Type {
						case "text/plain":
							indEml := indexedEmail{
								Type: "email",
								Body: value.Value,
							}
							err := batch.Index(string(v.ID), indEml)
							if err != nil {
								log.Error("[%s] indexed error: %v", c.name, err)
							}
						case "text/html":
						// TODO: strip tags
						default:
							log.Warn("[%s] unhandled type: %s", c.name, err)
						}
					}
					if firstState == "" {
						firstState = arg.State
					}
				}
			}
			if end >= len(toFetch) {
				// Save state in the last request
				log.Trace("[%s] Email state updated to: %q", c.name, firstState)
				stateBucket.Put(stateEmail, []byte(firstState))
			}
			return c.index.Batch(batch)
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
	q := bleve.NewMatchQuery(strings.Join(s.Terms, " "))
	sreq := bleve.NewSearchRequest(q)
	sreq.Fields = append(sreq.Fields, "Body")
	sreq.Size = math.MaxInt
	sresult, err := c.index.Search(sreq)
	if err != nil {
		return []Email{}, err
	}
	fmt.Println(len(sresult.Hits))
	for _, hit := range sresult.Hits {
		body, ok := hit.Fields["Body"]
		if !ok {
			continue
		}
		fmt.Println(body)
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
	log.Trace("[%s] Search: elapsed=%s, candidates=%d, results=%d", c.name, time.Since(start), count, len(result))

	return result, nil
}

func (c *JmapClient) parseSearch(args []string) (SearchCriteria, error) {
	root := SearchCriteria{}

	or := false
	for _, arg := range args {
		prefix, term, found := strings.Cut(arg, ":")
		if !found {
			switch prefix {
			case "or", "OR":
				or = true
			case "and", "AND":
				or = false
			case "not", "NOT":
				return root, fmt.Errorf("NOT is currently not supported")
			default:
				root.Terms = append(root.Terms, prefix)
			}
			continue
		}
		s := SearchCriteria{}
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
		case "is":
			switch term {
			case "read":
				s.HasKeyword = "$seen"
			case "unread":
				s.NotKeyword = "$seen"
			}
		}
		switch {
		case or:
			root.Or = append(root.Or, s)
			or = false
		default:
			root.And = append(root.And, s)
		}
	}
	return root, nil
}
