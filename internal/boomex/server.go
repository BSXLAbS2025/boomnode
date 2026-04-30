package boomex

import (
	"fmt"
	"net"
)

// MessageHandler — функция, вызываемая при получении сообщения
type MessageHandler func(session *Session, msg *Message)

// Server — TCP-сервер BoomEx
type Server struct {
	listenAddr string
	handler    MessageHandler
	listener   net.Listener
}

// NewServer создаёт новый сервер
func NewServer(listenAddr string, handler MessageHandler) *Server {
	return &Server{
		listenAddr: listenAddr,
		handler:    handler,
	}
}

// Start запускает сервер
func (s *Server) Start() error {
	var err error
	s.listener, err = net.Listen("tcp", s.listenAddr)
	if err != nil {
		return fmt.Errorf("не могу слушать порт %s: %w", s.listenAddr, err)
	}

	fmt.Printf("BoomEx сервер запущен на %s\n", s.listenAddr)

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
			// Сервер остановлен
			return
		}

		go s.handleConnection(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	session := NewSession(conn)
	defer session.Close()

	fmt.Printf("Новое подключение от %s\n", session.RemoteAddr())

	for {
		msg, err := session.Receive()
		if err != nil {
			fmt.Printf("Соединение с %s закрыто: %v\n", session.RemoteAddr(), err)
			return
		}

		fmt.Printf("Получено сообщение от %s:\n", msg.From)
		fmt.Printf("  Кому: %s\n", msg.To)
		fmt.Printf("  Тема: %s\n", msg.Subject)
		fmt.Printf("  Текст: %s\n", msg.Body)

		if s.handler != nil {
			s.handler(session, msg)
		}
	}
}
