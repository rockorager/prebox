package keywork

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/mail"
	"sync"

	"github.com/vmihailenco/msgpack/v5"
)

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
	Id         string          `msgpack:"id"`
	Subject    string          `msgpack:"subject"`
	MessageId  string          `msgpack:"message_id"`
	InReplyTo  string          `msgpack:"in_reply_to"`
	References []string        `msgpack:"references"`
	From       []*mail.Address `msgpack:"from"`
	Mailboxes  []string        `msgpack:"mailbox_ids"`
	Keywords   []string        `msgpack:"keywords"`
	ReceivedAt int64           `msgpack:"timestamp"`
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
			NewJmapClient(cfg.name, cfg.url)
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
				enc:    msgpack.NewEncoder(conn),
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
	server *Server
	conn   net.Conn
	enc    *msgpack.Encoder
	encMu  sync.Mutex
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
			c.Log("Invalid RPC message: %v", err)
			continue
		}

		switch msgType {
		case 0: // Request
			switch method {
			case "connect":
				err := c.handleConnect(id, args)
				if err != nil {
					c.Log("Invalid RPC request: %v", err)
					continue
				}
			}
		case 1: // Response
		case 2: // Notification
		default:
			c.Log("Invalid RPC message. MsgType must be 0, 1, or 2. Got: %d. Message=%v", msgType, msg)
		}
	}
}

func (c *Connection) handleConnect(id int, args []interface{}) error {
	err := expectLen(2, args)
	if err != nil {
		return err
	}
	uri, err := expectString(args[0])
	if err != nil {
		return err
	}
	name, err := expectString(args[1])
	if err != nil {
		return err
	}
	_ = uri
	_ = name
	return nil
}
