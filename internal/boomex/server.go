package boomex

import (
	"fmt"
	"net"
)

type MessageHandler func(session *Session, msg *Message)

type Server struct {
	listenAddr string
	handler    MessageHandler
	listener   net.Listener
}

func NewServer(listenAddr string, handler MessageHandler) *Server {
	return &Server{
		listenAddr: listenAddr,
		handler:    handler,
	}
}

func (s *Server) Start() error {
	var err error
	s.listener, err = net.Listen("tcp", s.listenAddr)
	if err != nil {
		return fmt.Errorf("listen error on %s: %w", s.listenAddr, err)
	}

	fmt.Printf("BoomEx server started on %s\n", s.listenAddr)

	go s.acceptLoop()
	return nil
}

func (s *Server) Stop() error {
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConnection(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	session := NewSession(conn)
	defer session.Close()

	fmt.Printf("New connection from %s\n", session.RemoteAddr())

	for {
		msg, err := session.Receive()
		if err != nil {
			fmt.Printf("Connection from %s closed: %v\n", session.RemoteAddr(), err)
			return
		}

		fmt.Printf("=== INCOMING MESSAGE ===\n")
		fmt.Printf("From:    %s\n", msg.From)
		fmt.Printf("To:      %s\n", msg.To)
		fmt.Printf("Subject: %s\n", msg.Subject)
		fmt.Printf("Body:    %s\n", msg.Body)
		fmt.Printf("=========================\n")

		if s.handler != nil {
			s.handler(session, msg)
		}
	}
}
