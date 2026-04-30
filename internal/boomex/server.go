package boomex

import (
	"fmt"
	"net"
	"time"
)

// MessageHandler — функция, вызываемая при получении сообщения
type MessageHandler func(session *Session, msg *Message)

// Server — TCP-сервер BoomEx
type Server struct {
	listenAddr string
	handler    MessageHandler
	listener   net.Listener
	myAddress  string
}

// NewServer создаёт новый сервер
func NewServer(listenAddr string, myAddress string, handler MessageHandler) *Server {
	return &Server{
		listenAddr: listenAddr,
		myAddress:  myAddress,
		handler:    handler,
	}
}

// Start запускает сервер
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

// Stop останавливает сервер
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

	// Ждём HELO от клиента
	helo, err := session.Receive()
	if err != nil {
		fmt.Printf("Connection from %s closed before HELO: %v\n", session.RemoteAddr(), err)
		return
	}

	if helo.Type != TypeHELO {
		fmt.Printf("Expected HELO from %s, got %s\n", session.RemoteAddr(), helo.Type)
		return
	}

	fmt.Printf("HELO received from %s (%s)\n", helo.From, session.RemoteAddr())

	// Отправляем ответный HELO
	response := Message{
		Type:      TypeHELO,
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		From:      s.myAddress,
		To:        helo.From,
		Timestamp: time.Now(),
	}

	if err := session.Send(response); err != nil {
		fmt.Printf("Failed to send HELO response to %s: %v\n", session.RemoteAddr(), err)
		return
	}

	// Теперь принимаем сообщения
	for {
		msg, err := session.Receive()
		if err != nil {
			fmt.Printf("Connection from %s closed: %v\n", session.RemoteAddr(), err)
			return
		}

		switch msg.Type {
		case TypeMSG:
			fmt.Printf("=== INCOMING MESSAGE ===\n")
			fmt.Printf("From:    %s\n", msg.From)
			fmt.Printf("To:      %s\n", msg.To)
			fmt.Printf("Subject: %s\n", msg.Subject)
			fmt.Printf("Body:    %s\n", msg.Body)
			fmt.Printf("=========================\n")

			// Отправляем ACK
			ack := Message{
				Type: TypeACK,
				ID:   msg.ID,
				From: s.myAddress,
				To:   msg.From,
			}
			session.Send(ack)

			if s.handler != nil {
				s.handler(session, msg)
			}

		case TypeBYE:
			fmt.Printf("Client %s said goodbye\n", session.RemoteAddr())
			return
		}
	}
}
