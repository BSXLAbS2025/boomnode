package boomex

import (
	"fmt"
	"net"
	"time"

	bolt "go.etcd.io/bbolt"
)

// MessageHandler — функция, вызываемая при получении сообщения
type MessageHandler func(session *Session, msg *Message)

// Server — TCP-сервер BoomEx
type Server struct {
	listenAddr   string
	myAddress    string
	handler      MessageHandler
	listener     net.Listener
	db           *bolt.DB
	relayEnabled bool
	whitelist    []string
}

// NewServer создаёт новый сервер
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

// createBuckets создаёт необходимые bucket'ы в БД
func (s *Server) createBuckets() {
	if s.db != nil {
		s.db.Update(func(tx *bolt.Tx) error {
			tx.CreateBucketIfNotExists(BucketMessages)
			return nil
		})
	}
}

// Start запускает сервер
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

// Stop останавливает сервер
func (s *Server) Stop() error {
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

// isInWhitelist проверяет, есть ли адрес в белом списке
func (s *Server) isInWhitelist(address string) bool {
	if len(s.whitelist) == 0 {
		return true // Пустой список = разрешено всем
	}
	for _, w := range s.whitelist {
		if w == address {
			return true
		}
	}
	return false
}

// acceptLoop принимает входящие соединения
func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConnection(conn)
	}
}

// handleConnection обрабатывает одно соединение
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

	// Основной цикл обработки команд
	for {
		msg, err := session.Receive()
		if err != nil {
			fmt.Printf("Connection from %s closed: %v\n", session.RemoteAddr(), err)
			return
		}

		switch msg.Type {
		case TypeMSG:
			if msg.To == s.myAddress {
				// Сообщение адресовано нам
				fmt.Printf("=== INCOMING MESSAGE ===\n")
				fmt.Printf("From:    %s\n", msg.From)
				fmt.Printf("To:      %s\n", msg.To)
				fmt.Printf("Subject: %s\n", msg.Subject)
				fmt.Printf("Body:    %s\n", msg.Body)
				fmt.Printf("=========================\n")

				// Отправляем подтверждение
				ack := Message{
					Type: TypeACK,
					ID:   msg.ID,
					From: s.myAddress,
					To:   msg.From,
				}
				session.Send(ack)

				// Вызываем внешний обработчик
				if s.handler != nil {
					s.handler(session, msg)
				}

			} else if s.relayEnabled {
				// Режим релея: сохраняем для другого узла
				if s.isInWhitelist(msg.To) {
					if s.db != nil {
						if err := StoreMessageForRelay(s.db, msg); err != nil {
							fmt.Printf("Relay store error: %v\n", err)
							// Отправляем ошибку отправителю
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
							// Подтверждаем хранение
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
					// Адресат не в белом списке
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
				// Не релейный режим — отказываем
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
			// Команда FETCH: забрать накопленные сообщения
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
					// Отправляем все накопленные сообщения
					fmt.Printf("Delivering %d relayed messages to %s\n", len(msgs), msg.From)
					for _, m := range msgs {
						if err := session.Send(m); err != nil {
							fmt.Printf("Error sending relayed message: %v\n", err)
							break
						}
					}
					// Удаляем доставленные
					DeleteMessagesForRelay(s.db, msgs)
				} else {
					// Нет сообщений
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
				// Не релей
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
