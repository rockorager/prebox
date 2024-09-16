package keywork

import (
	"log"
	"net"

	"github.com/vmihailenco/msgpack/v5"
)

const addr = ":2113"

type Server struct{}

func NewServer() *Server {
	return &Server{}
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
			err := s.handleConn(conn)
			if err != nil {
				log.Printf("error: %v", err)
			}
		}()
	}
}

func (s *Server) handleConn(conn net.Conn) error {
	log.Println("New connection")
	dec := msgpack.NewDecoder(conn)
	for {
		msg, err := dec.DecodeSlice()
		if err != nil {
			// If we error we we are unlikely able to recover
			return err
		}
		msgType, id, method, args, err := validateMsg(msg)
		if err != nil {
			// A single bad message is recoverable
			log.Printf("Invalid RPC message: %v", err)
			continue
		}

		switch msgType {
		case 0: // Request
			switch method {
			case "connect":
				err := s.handleConnect(id, args)
				if err != nil {
					log.Printf("Invalid RPC request: %v", err)
					continue
				}
			}
		case 1: // Response
		case 2: // Notification
		default:
			log.Printf("Invalid RPC message. MsgType must be 0, 1, or 2. Got: %d. Message=%v", msgType, msg)
		}
	}
}

func (s *Server) handleConnect(id int, args []interface{}) error {
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
