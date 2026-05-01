package mesh

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// Server — UDP-сервер BoomMesh
type Server struct {
	conn     *net.UDPConn
	dht      *DHT
	myAddr   string
	myPubKey string
	isRelay  bool
}

func NewServer(bindAddr string, dht *DHT, myAddr string, myPubKey string, isRelay bool) (*Server, error) {
	addr, err := net.ResolveUDPAddr("udp", bindAddr)
	if err != nil {
		return nil, fmt.Errorf("resolve UDP addr: %w", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen UDP: %w", err)
	}

	return &Server{
		conn:     conn,
		dht:      dht,
		myAddr:   myAddr,
		myPubKey: myPubKey,
		isRelay:  isRelay,
	}, nil
}

// Start запускает UDP-сервер
func (s *Server) Start() error {
	fmt.Printf("BoomMesh UDP server started on %s\n", s.conn.LocalAddr())

	go s.readLoop()
	go s.beaconLoop()
	go s.cleanupLoop()

	return nil
}

// Stop останавливает сервер
func (s *Server) Stop() error {
	return s.conn.Close()
}

// readLoop читает входящие UDP-пакеты
func (s *Server) readLoop() {
	buf := make([]byte, 4096)
	for {
		n, remoteAddr, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}

		var msg MeshMessage
		if err := json.Unmarshal(buf[:n], &msg); err != nil {
			continue
		}

		s.handleMessage(msg, *remoteAddr)
	}
}

// beaconLoop рассылает маяки всем известным пирам
func (s *Server) beaconLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		announce := MeshMessage{
			Type:      MeshANNOUNCE,
			From:      s.myAddr,
			Data:      s.myPubKey,
			Timestamp: time.Now(),
		}
		if s.isRelay {
			announce.Data = "relay:" + s.myPubKey
		}
		announceData, _ := json.Marshal(announce)

		for _, peer := range s.dht.GetAll() {
			for _, addr := range peer.Addrs {
				s.conn.WriteToUDP(announceData, &addr)
			}
		}
	}
}

// cleanupLoop удаляет устаревших пиров
func (s *Server) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.dht.RemoveStale(15 * time.Minute)
	}
}

// handleMessage обрабатывает входящее сообщение
func (s *Server) handleMessage(msg MeshMessage, remoteAddr net.UDPAddr) {
	switch msg.Type {
	case MeshANNOUNCE:
		// Пир объявил о себе
		peer := MeshPeer{
			BMAddress: msg.From,
			Addrs:     []net.UDPAddr{remoteAddr},
			IsRelay:   msg.Data[:5] == "relay",
		}
		s.dht.AddOrUpdate(peer)

		// Отвечаем своим ANNOUNCE
		response := MeshMessage{
			Type:      MeshANNOUNCE,
			From:      s.myAddr,
			Data:      s.myPubKey,
			Timestamp: time.Now(),
		}
		if s.isRelay {
			response.Data = "relay:" + s.myPubKey
		}
		respData, _ := json.Marshal(response)
		s.conn.WriteToUDP(respData, &remoteAddr)

	case MeshFIND:
		target := msg.To
		fmt.Printf("DHT: looking for %s (asked by %s)\n", target, msg.From)

		if peer, ok := s.dht.Find(target); ok {
			// Нашли
			addrData, _ := json.Marshal(peer.Addrs)
			found := MeshMessage{
				Type:      MeshFOUND,
				From:      s.myAddr,
				To:        msg.From,
				Data:      string(addrData),
				Timestamp: time.Now(),
			}
			foundData, _ := json.Marshal(found)
			s.conn.WriteToUDP(foundData, &remoteAddr)
			fmt.Printf("DHT: found %s, sent to %s\n", target, msg.From)
		} else {
			// Распространяем запрос другим пирам
			fmt.Printf("DHT: %s not in local table, forwarding FIND\n", target)
			findData, _ := json.Marshal(msg)
			for _, p := range s.dht.GetAll() {
				for _, addr := range p.Addrs {
					if addr.String() != remoteAddr.String() {
						s.conn.WriteToUDP(findData, &addr)
					}
				}
			}
		}

	case MeshFOUND:
		fmt.Printf("DHT: received FOUND for %s from %s\n", msg.To, msg.From)
		if msg.Data != "" {
			var addrs []net.UDPAddr
			if err := json.Unmarshal([]byte(msg.Data), &addrs); err == nil {
				peer := MeshPeer{
					BMAddress: msg.To,
					Addrs:     addrs,
				}
				s.dht.AddOrUpdate(peer)
			}
		}

	case MeshHOLEPUNCH:
		fmt.Printf("Hole punch request from %s\n", msg.From)
		punch := MeshMessage{
			Type: MeshPONG,
			From: s.myAddr,
			To:   msg.From,
		}
		punchData, _ := json.Marshal(punch)
		s.conn.WriteToUDP(punchData, &remoteAddr)

	case MeshPING:
		// Отвечаем PONG
		pong := MeshMessage{
			Type: MeshPONG,
			From: s.myAddr,
			To:   msg.From,
		}
		pongData, _ := json.Marshal(pong)
		s.conn.WriteToUDP(pongData, &remoteAddr)
	}
}

// FindPeer ищет пира через DHT и возвращает адреса
func (s *Server) FindPeer(bmAddress string) ([]net.UDPAddr, bool) {
	if peer, ok := s.dht.Find(bmAddress); ok {
		return peer.Addrs, true
	}

	// Если не нашли локально — рассылаем FIND
	findMsg := MeshMessage{
		Type: MeshFIND,
		From: s.myAddr,
		To:   bmAddress,
	}

	findData, _ := json.Marshal(findMsg)
	for _, peer := range s.dht.GetAll() {
		for _, addr := range peer.Addrs {
			s.conn.WriteToUDP(findData, &addr)
		}
	}

	return nil, false
}

// AddPeer добавляет пира вручную (для известных публичных узлов)
func (s *Server) AddPeer(bmAddress string, addr net.UDPAddr, isRelay bool) {
	peer := MeshPeer{
		BMAddress: bmAddress,
		Addrs:     []net.UDPAddr{addr},
		IsRelay:   isRelay,
	}
	s.dht.AddOrUpdate(peer)
}

// ListPeers возвращает всех известных DHT-пиров
func (s *Server) ListPeers() []*MeshPeer {
	return s.dht.GetAll()
}
