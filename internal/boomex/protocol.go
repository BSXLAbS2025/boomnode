package boomex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"
)

type Message struct {
	ID        string    `json:"id"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Subject   string    `json:"subject"`
	Body      string    `json:"body"`
	Timestamp time.Time `json:"timestamp"`
}

type Session struct {
	conn   net.Conn
	addr   string
	writer *bufio.Writer
	reader *bufio.Reader
	mu     sync.Mutex
}

func NewSession(conn net.Conn) *Session {
	return &Session{
		conn:   conn,
		addr:   conn.RemoteAddr().String(),
		writer: bufio.NewWriter(conn),
		reader: bufio.NewReader(conn),
	}
}

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
	_, err = s.reader.Read(data)
	if err != nil {
		return nil, fmt.Errorf("read data error: %w", err)
	}

	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	return &msg, nil
}

func (s *Session) Close() error {
	return s.conn.Close()
}

func (s *Session) RemoteAddr() string {
	return s.addr
}

func SendMessageToPeer(tcpAddr string, msg Message) error {
	conn, err := net.DialTimeout("tcp", tcpAddr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("connect to %s failed: %w", tcpAddr, err)
	}
	defer conn.Close()

	session := NewSession(conn)
	if err := session.Send(msg); err != nil {
		return fmt.Errorf("send error: %w", err)
	}

	fmt.Printf("Message sent to %s via %s\n", msg.To, tcpAddr)
	return nil
}
