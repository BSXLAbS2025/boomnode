package mesh

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"
)

// --- Типы сообщений ---

type MeshType string

const (
	MeshPING      MeshType = "PING"
	MeshPONG      MeshType = "PONG"
	MeshANNOUNCE  MeshType = "ANNOUNCE"
	MeshFIND      MeshType = "FIND"
	MeshFOUND     MeshType = "FOUND"
	MeshHOLEPUNCH MeshType = "HOLEPUNCH"
)

// --- Структуры данных ---

type MeshPeer struct {
	BMAddress string        `json:"bm_address"`
	PublicKey string        `json:"public_key"`
	Addrs     []net.UDPAddr `json:"addrs"`
	LastSeen  time.Time     `json:"last_seen"`
	IsRelay   bool          `json:"is_relay"`
}

type MeshMessage struct {
	Type      MeshType  `json:"type"`
	From      string    `json:"from"`
	To        string    `json:"to,omitempty"`
	Data      string    `json:"data,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// --- DHT ---

type DHT struct {
	mu    sync.RWMutex
	peers map[string]*MeshPeer
}

func NewDHT() *DHT {
	return &DHT{
		peers: make(map[string]*MeshPeer),
	}
}

func (d *DHT) AddOrUpdate(peer MeshPeer) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if existing, ok := d.peers[peer.BMAddress]; ok {
		existing.Addrs = peer.Addrs
		existing.LastSeen = time.Now()
		existing.IsRelay = peer.IsRelay
	} else {
		peer.LastSeen = time.Now()
		d.peers[peer.BMAddress] = &peer
		fmt.Printf("DHT: added peer %s\n", peer.BMAddress)
	}
}

func (d *DHT) Find(bmAddress string) (*MeshPeer, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	peer, ok := d.peers[bmAddress]
	return peer, ok
}

func (d *DHT) GetAll() []*MeshPeer {
	d.mu.RLock()
	defer d.mu.RUnlock()

	peers := make([]*MeshPeer, 0, len(d.peers))
	for _, p := range d.peers {
		peers = append(peers, p)
	}
	return peers
}

func (d *DHT) RemoveStale(maxAge time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for addr, peer := range d.peers {
		if time.Since(peer.LastSeen) > maxAge {
			delete(d.peers, addr)
			fmt.Printf("DHT: removed stale peer %s\n", addr)
		}
	}
}

// --- UDP Сервер ---

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

func (s *Server) Start() error {
	fmt.Printf("BoomMesh UDP server started on %s\n", s.conn.LocalAddr())

	go s.readLoop()
	go s.beaconLoop()
	go s.cleanupLoop()

	return nil
}

func (s *Server) Stop() error {
	return s.conn.Close()
}

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

func (s *Server) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.dht.RemoveStale(15 * time.Minute)
	}
}

func (s *Server) handleMessage(msg MeshMessage, remoteAddr net.UDPAddr) {
	switch msg.Type {
	case MeshANNOUNCE:
		peer := MeshPeer{
			BMAddress: msg.From,
			Addrs:     []net.UDPAddr{remoteAddr},
			IsRelay:   len(msg.Data) >= 5 && msg.Data[:5] == "relay",
		}
		s.dht.AddOrUpdate(peer)

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
		pong := MeshMessage{
			Type: MeshPONG,
			From: s.myAddr,
			To:   msg.From,
		}
		pongData, _ := json.Marshal(pong)
		s.conn.WriteToUDP(pongData, &remoteAddr)
	}
}

func (s *Server) FindPeer(bmAddress string) ([]net.UDPAddr, bool) {
	if peer, ok := s.dht.Find(bmAddress); ok {
		return peer.Addrs, true
	}

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

func (s *Server) AddPeer(bmAddress string, addr net.UDPAddr, isRelay bool) {
	peer := MeshPeer{
		BMAddress: bmAddress,
		Addrs:     []net.UDPAddr{addr},
		IsRelay:   isRelay,
	}
	s.dht.AddOrUpdate(peer)
}

func (s *Server) ListPeers() []*MeshPeer {
	return s.dht.GetAll()
}
