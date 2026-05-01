package boomex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
	"github.com/tarm/serial"
	"github.com/BSXLAbS2025/boomnode/internal/transport"
)

// --- Типы сообщений ---

type MessageType string

const (
	TypeHELO  MessageType = "HELO"
	TypeMSG   MessageType = "MSG"
	TypeACK   MessageType = "ACK"
	TypeBYE   MessageType = "BYE"
	TypeFETCH MessageType = "FETCH"
)

// --- Структуры ---

type Message struct {
	Type      MessageType `json:"type"`
	ID        string      `json:"id"`
	From      string      `json:"from"`
	To        string      `json:"to"`
	Subject   string      `json:"subject"`
	Body      string      `json:"body"`
	Timestamp time.Time   `json:"timestamp"`
}

type Session struct {
	conn   net.Conn
	addr   string
	writer *bufio.Writer
	reader *bufio.Reader
	mu     sync.Mutex
}

type MessageHandler func(session *Session, msg *Message)

type Server struct {
	listenAddr   string
	myAddress    string
	handler      MessageHandler
	listener     net.Listener
	db           *bolt.DB
	relayEnabled bool
	whitelist    []string
}

// Buckets
var BucketMessages = []byte("messages_msg")

// --- Session методы ---

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

func (s *Session) Close() error {
	return s.conn.Close()
}

func (s *Session) RemoteAddr() string {
	return s.addr
}

func (s *Session) Handshake(myAddress string, expectAddress string) error {
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

	response, err := s.Receive()
	if err != nil {
		return fmt.Errorf("receive HELO response error: %w", err)
	}

	if response.Type != TypeHELO {
		return fmt.Errorf("expected HELO, got %s", response.Type)
	}

	return nil
}

func SendMessageToPeer(tcpAddr string, myAddress string, msg Message) error {
	conn, err := net.DialTimeout("tcp", tcpAddr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("connect to %s failed: %w", tcpAddr, err)
	}
	defer conn.Close()

	session := NewSession(conn)

	if err := session.Handshake(myAddress, msg.To); err != nil {
		return fmt.Errorf("handshake failed: %w", err)
	}

	// Auto FETCH: запрашиваем накопленные сообщения
	fetchMsg := Message{
		Type:      TypeFETCH,
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		From:      myAddress,
		To:        msg.To,
		Timestamp: time.Now(),
	}
	if err := session.Send(fetchMsg); err != nil {
		return fmt.Errorf("send FETCH error: %w", err)
	}

	// Принимаем накопленные сообщения
	for {
		response, err := session.Receive()
		if err != nil {
			break
		}
		if response.Type == TypeMSG && response.Subject == "NO_MAIL" {
			break
		}
		if response.Type == TypeMSG {
			fmt.Printf("=== FETCHED MESSAGE ===\nFrom: %s\nSubject: %s\nBody: %s\n=========================\n", response.From, response.Subject, response.Body)
		}
	}

	// Отправляем основное сообщение
	if err := session.Send(msg); err != nil {
		return fmt.Errorf("send message error: %w", err)
	}

	response, err := session.Receive()
	if err != nil {
		return fmt.Errorf("receive response error: %w", err)
	}

	switch response.Type {
	case TypeACK:
		fmt.Printf("Message delivered and acknowledged by %s\n", tcpAddr)
	case TypeMSG:
		if response.Subject == "RELAY_ERROR" || response.Subject == "RELAY_DISABLED" || response.Subject == "RELAY_DENIED" {
			return fmt.Errorf("relay failed: %s - %s", response.Subject, response.Body)
		}
		fmt.Printf("=== INCOMING MESSAGE ===\nFrom: %s\nSubject: %s\nBody: %s\n=========================\n", response.From, response.Subject, response.Body)
	default:
		return fmt.Errorf("unexpected response type: %s", response.Type)
	}

	return nil
}

// --- Relay (хранение и выдача сообщений) ---

func StoreMessageForRelay(db *bolt.DB, msg *Message) error {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(BucketMessages)
		if b == nil {
			return fmt.Errorf("bucket messages_msg not found")
		}
		key := fmt.Sprintf("%s_%s_%d", msg.To, msg.From, time.Now().UnixNano())
		data, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		return b.Put([]byte(key), data)
	})
}

func FetchMessagesForRelay(db *bolt.DB, recipient string) ([]Message, error) {
	var msgs []Message
	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(BucketMessages)
		if b == nil {
			return fmt.Errorf("bucket messages_msg not found")
		}
		c := b.Cursor()
		prefix := []byte(recipient)
		for k, v := c.Seek(prefix); k != nil && string(k[:len(prefix)]) == recipient; k, v = c.Next() {
			var msg Message
			if err := json.Unmarshal(v, &msg); err != nil {
				continue
			}
			msgs = append(msgs, msg)
		}
		return nil
	})
	return msgs, err
}

func DeleteMessagesForRelay(db *bolt.DB, msgs []Message) error {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(BucketMessages)
		if b == nil {
			return fmt.Errorf("bucket messages_msg not found")
		}
		for _, msg := range msgs {
			c := b.Cursor()
			for k, v := c.First(); k != nil; k, v = c.Next() {
				var m Message
				if json.Unmarshal(v, &m) == nil && m.ID == msg.ID {
					b.Delete(k)
				}
			}
		}
		return nil
	})
}

// ============================================================
// DIAL-UP ТРАНСПОРТ
// ============================================================

// SendMessageViaDialup отправляет сообщение через dial-up модем
func SendMessageViaDialup(device string, baud int, phoneNumber string, myAddress string, msg Message) error {
	// Импортируем транспорт локально, чтобы избежать циклических зависимостей
	// Используем чистый последовательный порт через github.com/tarm/serial
	
	conn, err := dialSerial(device, baud, phoneNumber)
	if err != nil {
		return fmt.Errorf("dial-up connect failed: %w", err)
	}
	defer conn.Close()

	session := NewSession(conn)

	// HELO
	helo := Message{
		Type:      TypeHELO,
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		From:      myAddress,
		To:        msg.To,
		Timestamp: time.Now(),
	}
	if err := session.Send(helo); err != nil {
		return fmt.Errorf("send HELO via dial-up error: %w", err)
	}

	response, err := session.Receive()
	if err != nil {
		return fmt.Errorf("receive HELO response via dial-up error: %w", err)
	}
	if response.Type != TypeHELO {
		return fmt.Errorf("expected HELO, got %s", response.Type)
	}

	fmt.Printf("HELO handshake successful via dial-up with %s\n", response.From)

	// Auto FETCH
	fetchMsg := Message{
		Type:      TypeFETCH,
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		From:      myAddress,
		To:        msg.To,
		Timestamp: time.Now(),
	}
	session.Send(fetchMsg)

	for {
		fetched, err := session.Receive()
		if err != nil {
			break
		}
		if fetched.Type == TypeMSG && fetched.Subject == "NO_MAIL" {
			break
		}
		if fetched.Type == TypeMSG {
			fmt.Printf("=== FETCHED (dial-up) ===\nFrom: %s\nSubject: %s\nBody: %s\n===========================\n", fetched.From, fetched.Subject, fetched.Body)
		}
	}

	// Отправка основного сообщения
	if err := session.Send(msg); err != nil {
		return fmt.Errorf("send MSG via dial-up error: %w", err)
	}

	ack, err := session.Receive()
	if err != nil {
		return fmt.Errorf("receive ACK via dial-up error: %w", err)
	}

	switch ack.Type {
	case TypeACK:
		fmt.Printf("Message delivered via dial-up to %s\n", phoneNumber)
	case TypeMSG:
		if ack.Subject == "RELAY_ERROR" || ack.Subject == "RELAY_DISABLED" || ack.Subject == "RELAY_DENIED" {
			return fmt.Errorf("relay failed via dial-up: %s", ack.Body)
		}
		fmt.Printf("=== INCOMING (dial-up) ===\nFrom: %s\nSubject: %s\nBody: %s\n===========================\n", ack.From, ack.Subject, ack.Body)
	default:
		return fmt.Errorf("unexpected response via dial-up: %s", ack.Type)
	}

	return nil
}

// dialSerial открывает последовательный порт и звонит
func dialSerial(device string, baud int, phoneNumber string) (*serialConn, error) {
	cfg := &serial.Config{
		Name:        device,
		Baud:        baud,
		ReadTimeout: 60 * time.Second,
	}

	port, err := serial.OpenPort(cfg)
	if err != nil {
		return nil, fmt.Errorf("open port: %w", err)
	}

	reader := bufio.NewReader(port)

	// AT-инициализация
	port.Write([]byte("ATZ\r\n"))
	time.Sleep(1 * time.Second)
	port.Write([]byte("ATE0\r\n"))
	time.Sleep(200 * time.Millisecond)
	port.Write([]byte("ATV1\r\n"))
	time.Sleep(200 * time.Millisecond)

	// Звонок
	port.Write([]byte(fmt.Sprintf("ATDT%s\r\n", phoneNumber)))

	// Ждём CONNECT
	timeout := time.After(60 * time.Second)
	for {
		select {
		case <-timeout:
			port.Close()
			return nil, fmt.Errorf("dial timeout")
		default:
			line, err := reader.ReadString('\n')
			if err != nil {
				port.Close()
				return nil, fmt.Errorf("read error: %w", err)
			}
			fmt.Printf("Modem: %s", line)
			if len(line) >= 7 && line[:7] == "CONNECT" {
				return &serialConn{port: port, reader: reader}, nil
			}
			if line == "BUSY\r\n" || line == "NO CARRIER\r\n" || line == "ERROR\r\n" {
				port.Close()
				return nil, fmt.Errorf("call failed: %s", line)
			}
		}
	}
}

// serialConn реализует net.Conn для последовательного порта
type serialConn struct {
	port   *serial.Port
	reader *bufio.Reader
}

func (s *serialConn) Read(p []byte) (n int, err error)   { return s.port.Read(p) }
func (s *serialConn) Write(p []byte) (n int, err error)  { return s.port.Write(p) }
func (s *serialConn) Close() error                        { s.port.Write([]byte("ATH0\r\n")); time.Sleep(500 * time.Millisecond); return s.port.Close() }
func (s *serialConn) LocalAddr() net.Addr                 { return &serialAddr{"modem"} }
func (s *serialConn) RemoteAddr() net.Addr                { return &serialAddr{"remote"} }
func (s *serialConn) SetDeadline(t time.Time) error       { return nil }
func (s *serialConn) SetReadDeadline(t time.Time) error   { return nil }
func (s *serialConn) SetWriteDeadline(t time.Time) error  { return nil }

type serialAddr struct{ name string }
func (a *serialAddr) Network() string { return "serial" }
func (a *serialAddr) String() string  { return a.name }

// ============================================================
// РАДИО-ТРАНСПОРТ
// ============================================================

// SendMessageViaRadio отправляет сообщение через радио
func SendMessageViaRadio(myCall string, msg Message) error {
	cfg := transport.RadioConfig{
		MyCall:     myCall,
		Device:     "soundcard",
		Frequency:  "144.800",
		Mode:       "afsk",
		SampleRate: 22050,
		BitRate:    1200,
	}

	conn, err := transport.RadioListen(cfg)
	if err != nil {
		return fmt.Errorf("radio init failed: %w", err)
	}
	defer conn.Close()

	session := NewSession(conn)

	// HELO
	helo := Message{
		Type:      TypeHELO,
		ID:        fmt.Sprintf("radio-%d", time.Now().UnixNano()),
		From:      myCall,
		To:        msg.To,
		Timestamp: time.Now(),
	}
	if err := session.Send(helo); err != nil {
		return fmt.Errorf("send HELO via radio: %w", err)
	}

	// Ждём ответный HELO
	response, err := session.Receive()
	if err != nil {
		return fmt.Errorf("receive HELO via radio: %w", err)
	}
	if response.Type != TypeHELO {
		return fmt.Errorf("expected HELO, got %s", response.Type)
	}

	fmt.Printf("Radio handshake with %s successful!\n", response.From)

	// Отправляем сообщение
	if err := session.Send(msg); err != nil {
		return fmt.Errorf("send MSG via radio: %w", err)
	}

	// Ждём ACK
	ack, err := session.Receive()
	if err != nil {
		return fmt.Errorf("receive ACK via radio: %w", err)
	}

	if ack.Type == TypeACK {
		fmt.Println("Message delivered via radio!")
	}

	return nil
}

// --- Server ---

func NewServer(listenAddr string, myAddress string, db *bolt.DB, relayEnabled bool, whitelist []string, handler MessageHandler) *Server {
	s := &Server{
		listenAddr:   listenAddr,
		myAddress:    myAddress,
		handler:      handler,
		db:           db,
		relayEnabled: relayEnabled,
		whitelist:    whitelist,
	}
	s.createBuckets()
	return s
}

func (s *Server) createBuckets() {
	if s.db != nil {
		s.db.Update(func(tx *bolt.Tx) error {
			tx.CreateBucketIfNotExists(BucketMessages)
			return nil
		})
	}
}

func (s *Server) Start() error {
	var err error
	s.listener, err = net.Listen("tcp", s.listenAddr)
	if err != nil {
		return fmt.Errorf("listen error on %s: %w", s.listenAddr, err)
	}

	fmt.Printf("BoomEx server started on %s (relay: %v)\n", s.listenAddr, s.relayEnabled)

	go s.acceptLoop()
	return nil
}

func (s *Server) Stop() error {
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

func (s *Server) isInWhitelist(address string) bool {
	if len(s.whitelist) == 0 {
		return true
	}
	for _, w := range s.whitelist {
		if w == address {
			return true
		}
	}
	return false
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

	for {
		msg, err := session.Receive()
		if err != nil {
			fmt.Printf("Connection from %s closed: %v\n", session.RemoteAddr(), err)
			return
		}

		switch msg.Type {
		case TypeMSG:
			if msg.To == s.myAddress {
				fmt.Printf("=== INCOMING MESSAGE ===\n")
				fmt.Printf("From:    %s\n", msg.From)
				fmt.Printf("To:      %s\n", msg.To)
				fmt.Printf("Subject: %s\n", msg.Subject)
				fmt.Printf("Body:    %s\n", msg.Body)
				fmt.Printf("=========================\n")

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

			} else if s.relayEnabled {
				if s.isInWhitelist(msg.To) {
					if s.db != nil {
						if err := StoreMessageForRelay(s.db, msg); err != nil {
							fmt.Printf("Relay store error: %v\n", err)
							errMsg := Message{
								Type:    TypeMSG,
								ID:      msg.ID,
								From:    s.myAddress,
								To:      msg.From,
								Subject: "RELAY_ERROR",
								Body:    "Failed to store message: " + err.Error(),
							}
							session.Send(errMsg)
						} else {
							fmt.Printf("Stored relay message: %s -> %s\n", msg.From, msg.To)
							ack := Message{
								Type: TypeACK,
								ID:   msg.ID,
								From: s.myAddress,
								To:   msg.From,
							}
							session.Send(ack)
						}
					}
				} else {
					fmt.Printf("Relay denied: %s not in whitelist\n", msg.To)
					nak := Message{
						Type:    TypeMSG,
						ID:      msg.ID,
						From:    s.myAddress,
						To:      msg.From,
						Subject: "RELAY_DENIED",
						Body:    fmt.Sprintf("Recipient %s is not in relay whitelist", msg.To),
					}
					session.Send(nak)
				}

			} else {
				fmt.Printf("Relay disabled, rejecting message for %s\n", msg.To)
				nak := Message{
					Type:    TypeMSG,
					ID:      msg.ID,
					From:    s.myAddress,
					To:      msg.From,
					Subject: "RELAY_DISABLED",
					Body:    "This node does not relay messages",
				}
				session.Send(nak)
			}

		case TypeFETCH:
			fmt.Printf("FETCH request from %s\n", msg.From)
			if s.relayEnabled && s.db != nil {
				msgs, err := FetchMessagesForRelay(s.db, msg.From)
				if err != nil {
					fmt.Printf("FETCH error: %v\n", err)
					errMsg := Message{
						Type:    TypeMSG,
						ID:      msg.ID,
						From:    s.myAddress,
						To:      msg.From,
						Subject: "FETCH_ERROR",
						Body:    "Failed to fetch messages: " + err.Error(),
					}
					session.Send(errMsg)
				} else if len(msgs) > 0 {
					fmt.Printf("Delivering %d relayed messages to %s\n", len(msgs), msg.From)
					for _, m := range msgs {
						if err := session.Send(m); err != nil {
							fmt.Printf("Error sending relayed message: %v\n", err)
							break
						}
					}
					DeleteMessagesForRelay(s.db, msgs)
				} else {
					empty := Message{
						Type:    TypeMSG,
						ID:      msg.ID,
						From:    s.myAddress,
						To:      msg.From,
						Subject: "NO_MAIL",
						Body:    "No messages for you",
					}
					session.Send(empty)
				}
			} else {
				nak := Message{
					Type:    TypeMSG,
					ID:      msg.ID,
					From:    s.myAddress,
					To:      msg.From,
					Subject: "FETCH_DISABLED",
					Body:    "This node does not support FETCH",
				}
				session.Send(nak)
			}

		case TypeBYE:
			fmt.Printf("Client %s said goodbye\n", session.RemoteAddr())
			return

		default:
			fmt.Printf("Unknown message type from %s: %s\n", session.RemoteAddr(), msg.Type)
		}
	}
}
