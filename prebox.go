package prebox

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sort"
	"sync"

	"github.com/vmihailenco/msgpack/v5"
)

var errNotConnected = errors.New("not connected to a backend")

const addr = ":2113"

type Mailbox struct {
	Name      string `msgpack:"name"`
	Id        string `msgpack:"id"`
	ParentId  string `msgpack:"parent_id"`
	Role      string `msgpack:"role"`
	Total     uint   `msgpack:"total"`
	Unread    uint   `msgpack:"unread"`
	SortOrder uint   `msgpack:"sort_order"`
}

type Email struct {
	Type       string    `msgpack:"type"`
	Id         string    `msgpack:"id"`
	Subject    string    `msgpack:"subject"`
	MessageId  string    `msgpack:"message_id"`
	InReplyTo  string    `msgpack:"in_reply_to"`
	Date       string    `msgpack:"date"`
	References []string  `msgpack:"references"`
	ReplyTo    []Address `msgpack:"reply_to"`
	From       []Address `msgpack:"from"`
	To         []Address `msgpack:"to"`
	Cc         []Address `msgpack:"cc"`
	Bcc        []Address `msgpack:"bcc"`
	Mailboxes  []string  `msgpack:"mailbox_ids"`
	Keywords   []string  `msgpack:"keywords"`
	Size       uint      `msgpack:"size"`
}

type Address struct {
	Name  string `msgpack:"name"`
	Email string `msgpack:"email"`
}

type Server struct {
	backends []Backend
	mu       sync.Mutex
}

func NewServer(cfgs []Config) *Server {
	s := &Server{
		backends: make([]Backend, 0, len(cfgs)),
	}
	for _, cfg := range cfgs {
		switch cfg.url.Scheme {
		case "jmap":
			log.Println("jmap backend")
			client, err := NewJmapClient(cfg.name, cfg.url)
			if err != nil {
				log.Printf("error: %v", err)
				continue
			}
			err = client.Connect()
			if err != nil {
				log.Printf("[%s] error: %v", cfg.name, err)
				continue
			}
			s.backends = append(s.backends, client)
		}
	}
	return s
}

func (s *Server) ListenAndServe() error {
	l, err := net.Listen("tcp", ":2113")
	if err != nil {
		return err
	}
	log.Println("Listening on 2113...")
	defer l.Close()
	for {
		conn, err := l.Accept()
		if err != nil {
			log.Printf("error: %v", err)
			continue
		}
		go func() {
			c := Connection{
				conn:   conn,
				server: s,
			}
			err := c.Serve()
			if err != nil && !errors.Is(err, io.EOF) {
				c.Log("error: %v", err)
			}
		}()
	}
}

type Connection struct {
	conn    net.Conn
	backend Backend
	server  *Server
	writeMu sync.Mutex
}

func (c *Connection) Log(format string, v ...any) {
	message := format
	if len(v) > 0 {
		message = fmt.Sprintf(format, v...)
	}
	log.Printf("[%s] %s", c.conn.RemoteAddr().String(), message)
}

func (c *Connection) Serve() error {
	c.Log("New connection")
	defer c.Log("Connection closed")
	dec := msgpack.NewDecoder(c.conn)
	for {
		msg, err := dec.DecodeSlice()
		if err != nil {
			// If we error we we are unlikely able to recover
			return err
		}
		msgType, id, method, args, err := validateMsg(msg)
		if err != nil {
			// A single bad message is recoverable
			c.writeErrorNotification(fmt.Errorf("invalid RPC message: %v", err))
			continue
		}

		switch msgType {
		case 0: // Request
			switch method {
			case "list_remotes":
				err := c.handleListRemotes(id)
				if err != nil {
					c.writeErrorResponse(id, "list_remotes", err)
					continue
				}
			case "connect":
				err := c.handleConnect(id, args)
				if err != nil {
					c.writeErrorResponse(id, "connect", err)
					continue
				}
			case "list_mailboxes":
				err := c.handleListMailboxes(id)
				if err != nil {
					c.writeErrorResponse(id, "list_mailboxes", err)
					continue
				}
			case "search":
				err := c.handleSearch(id, args)
				if err != nil {
					c.writeErrorResponse(id, "search", err)
					continue
				}
			default:
				c.writeErrorResponse(id, method, fmt.Errorf("unhandled method", method))
			}
		case 1: // Response
		case 2: // Notification
		default:
			c.Log("Invalid RPC message. MsgType must be 0, 1, or 2. Got: %d. Message=%v", msgType, msg)
		}
	}
}

func (c *Connection) handleListRemotes(id int) error {
	c.server.mu.Lock()
	defer c.server.mu.Unlock()
	result := make([]string, 0, len(c.server.backends))
	for _, backend := range c.server.backends {
		result = append(result, backend.Name())
	}
	msg := []interface{}{
		1,
		id,
		"list_remotes",
		result,
	}
	return c.writeMsg(msg)
}

func (c *Connection) handleConnect(id int, args []interface{}) error {
	err := expectLen(1, args)
	if err != nil {
		return err
	}
	name, err := expectString(args[0])
	if err != nil {
		return err
	}
	c.server.mu.Lock()
	defer c.server.mu.Unlock()
	for _, backend := range c.server.backends {
		if backend.Name() != name {
			continue
		}
		c.backend = backend
		msg := []interface{}{
			1,
			id,
			"connect",
			[]interface{}{},
		}
		return c.writeMsg(msg)
	}
	return fmt.Errorf("backend not found: %s", name)
}

func (c *Connection) handleListMailboxes(id int) error {
	if c.backend == nil {
		return errNotConnected
	}
	mailboxes, err := c.backend.ListMailboxes()
	if err != nil {
		return err
	}
	msg := []interface{}{
		1,
		id,
		"list_mailboxes",
		mailboxes,
	}
	return c.writeMsg(msg)
}

// msg should be the full msgpack-rpc msg.
func (c *Connection) writeMsg(msg []interface{}) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	buf := bufio.NewWriter(c.conn)
	enc := msgpack.NewEncoder(buf)
	err := enc.Encode(msg)
	if err != nil {
		return err
	}
	return buf.Flush()
}

func (c *Connection) writeErrorResponse(id int, method string, err error) {
	c.Log("Request error: id=%d method=%s error: %v", id, method, err)
	msg := []interface{}{
		1, // response
		id,
		"error",
		[]interface{}{
			method,
			err.Error(),
		},
	}
	if err := c.writeMsg(msg); err != nil {
		c.Log("couldn't encode error response: %v", err)
	}
}

func (c *Connection) writeErrorNotification(err error) {
	c.Log("RPC error: %v", err)
	msg := []interface{}{
		2, // notification
		"error",
		[]interface{}{
			err.Error(),
		},
	}
	if err := c.writeMsg(msg); err != nil {
		c.Log("couldn't encode error response: %v", err)
	}
}

func (c *Connection) handleSearch(id int, args []interface{}) error {
	if c.backend == nil {
		return errNotConnected
	}
	query := make([]string, 0, len(args))
	for _, arg := range args {
		term, err := expectString(arg)
		if err != nil {
			return err
		}
		query = append(query, term)
	}
	emls, err := c.backend.Search(query)
	if err != nil {
		return err
	}

	msg := []interface{}{
		1, // response
		id,
		"search",
		emls,
	}
	return c.writeMsg(msg)
}
