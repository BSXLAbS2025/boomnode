package boomex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"
)

// Message types
type MessageType string

const (
	TypeHELO MessageType = "HELO"
	TypeMSG  MessageType = "MSG"
	TypeACK  MessageType = "ACK"
	TypeBYE  MessageType = "BYE"
)

// Message — структура сообщения BoomEx
type Message struct {
	Type      MessageType `json:"type"`
	ID        string      `json:"id"`
	From      string      `json:"from"`
	To        string      `json:"to"`
	Subject   string      `json:"subject"`
	Body      string      `json:"body"`
	Timestamp time.Time   `json:"timestamp"`
}

// Session — сессия обмена с одним пиром
type Session struct {
	conn   net.Conn
	addr   string
	writer *bufio.Writer
	reader *bufio.Reader
	mu     sync.Mutex
}

// NewSession создаёт новую сессию по TCP-соединению
func NewSession(conn net.Conn) *Session {
	return &Session{
		conn:   conn,
		addr:   conn.RemoteAddr().String(),
		writer: bufio.NewWriter(conn),
		reader: bufio.NewReader(conn),
	}
}

// Send отправляет сообщение в сессию
func (s *Session) Send(msg Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}

	_, err = s.writer.WriteString(fmt.Sprintf("%d\n", len(data)))
	if err != nil {
		return fmt.Errorf("write length error: %w", err)
	}
	_, err = s.writer.Write(data)
	if err != nil {
		return fmt.Errorf("write data error: %w", err)
	}
	return s.writer.Flush()
}

// Receive читает сообщение из сессии
func (s *Session) Receive() (*Message, error) {
	line, err := s.reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("read length error: %w", err)
	}

	var length int
	_, err = fmt.Sscanf(line, "%d\n", &length)
	if err != nil {
		return nil, fmt.Errorf("invalid length format: %w", err)
	}

	data := make([]byte, length)
	totalRead := 0
	for totalRead < length {
		n, err := s.reader.Read(data[totalRead:])
		if err != nil {
			return nil, fmt.Errorf("read data error: %w", err)
		}
		totalRead += n
	}

	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	return &msg, nil
}

// Close закрывает сессию
func (s *Session) Close() error {
	return s.conn.Close()
}

// RemoteAddr возвращает адрес пира
func (s *Session) RemoteAddr() string {
	return s.addr
}

// Handshake выполняет рукопожатие с пиром
func (s *Session) Handshake(myAddress string, expectAddress string) error {
	// Отправляем HELO
	helo := Message{
		Type:      TypeHELO,
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		From:      myAddress,
		To:        expectAddress,
		Timestamp: time.Now(),
	}
	if err := s.Send(helo); err != nil {
		return fmt.Errorf("send HELO error: %w", err)
	}

	fmt.Printf("HELO sent to %s\n", s.RemoteAddr())

	// Ждём ответный HELO
	response, err := s.Receive()
	if err != nil {
		return fmt.Errorf("receive HELO response error: %w", err)
	}

	if response.Type != TypeHELO {
		return fmt.Errorf("expected HELO, got %s", response.Type)
	}

	fmt.Printf("HELO received from %s (%s)\n", response.From, s.RemoteAddr())
	return nil
}

// SendMessageToPeer подключается к пиру и отправляет сообщение
func SendMessageToPeer(tcpAddr string, myAddress string, msg Message) error {
	fmt.Printf("Connecting to %s...\n", tcpAddr)

	conn, err := net.DialTimeout("tcp", tcpAddr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("connect to %s failed: %w", tcpAddr, err)
	}
	defer conn.Close()

	fmt.Printf("Connected to %s\n", tcpAddr)

	session := NewSession(conn)

	// Рукопожатие
	if err := session.Handshake(myAddress, msg.To); err != nil {
		return fmt.Errorf("handshake failed: %w", err)
	}

	// Отправляем сообщение
	if err := session.Send(msg); err != nil {
		return fmt.Errorf("send message error: %w", err)
	}

	// Ждём ACK
	ack, err := session.Receive()
	if err != nil {
		return fmt.Errorf("receive ACK error: %w", err)
	}

	if ack.Type != TypeACK {
		return fmt.Errorf("expected ACK, got %s", ack.Type)
	}

	fmt.Printf("Message delivered and acknowledged by %s\n", msg.To)
	return nil
}
