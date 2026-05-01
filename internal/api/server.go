package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/BSXLAbS2025/boomnode/internal/boomex"
	"github.com/BSXLAbS2025/boomnode/internal/crypto"
	"github.com/BSXLAbS2025/boomnode/internal/mesh"
	"github.com/BSXLAbS2025/boomnode/internal/peer"
	bolt "go.etcd.io/bbolt"
)

type Server struct {
	addr      string
	db        *bolt.DB
	keys      *crypto.NodeKeys
	myAddress string
	dht       *mesh.DHT
	meshSrv   *mesh.Server
}

func NewServer(addr string, db *bolt.DB, keys *crypto.NodeKeys, myAddress string, dht *mesh.DHT, meshSrv *mesh.Server) *Server {
	return &Server{
		addr:      addr,
		db:        db,
		keys:      keys,
		myAddress: myAddress,
		dht:       dht,
		meshSrv:   meshSrv,
	}
}

func (s *Server) Start() error {
	http.HandleFunc("/api/peers", s.handlePeers)
	http.HandleFunc("/api/msg", s.handleMsg)
	http.HandleFunc("/api/status", s.handleStatus)
	http.HandleFunc("/api/mesh", s.handleMesh)
	http.HandleFunc("/api/export", s.handleExport)
	http.HandleFunc("/api/import", s.handleImport)

	fmt.Printf("API server started on %s\n", s.addr)
	return http.ListenAndServe(s.addr, nil)
}

// --- PEERS ---

func (s *Server) handlePeers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		peers, err := peer.ListPeers(s.db)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(peers)

	case "POST":
		var req struct {
			Address string `json:"address"`
			Name    string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", 400)
			return
		}

		if req.Address == "" {
			http.Error(w, "address required", 400)
			return
		}

		// Ищем пира через BoomMesh DHT
		var tcpHost string
		if s.meshSrv != nil {
			if addrs, ok := s.meshSrv.FindPeer(req.Address); ok && len(addrs) > 0 {
				tcpHost = net.JoinHostPort(addrs[0].IP.String(), "24554")
				fmt.Printf("Found %s via DHT: %s\n", req.Address, tcpHost)
			} else {
				fmt.Printf("Peer %s not in DHT, will try later\n", req.Address)
			}
		}

		p := peer.PeerInfo{
			Address:    req.Address,
			Name:       req.Name,
			TrustLevel: 5,
			Transport:  "auto",
			TCPHost:    tcpHost,
			DateAdded:  time.Now(),
		}

		if err := peer.AddPeer(s.db, p); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		w.WriteHeader(201)
		fmt.Fprintf(w, "Peer %s added (TCP: %s)\n", p.Address, tcpHost)

	default:
		http.Error(w, "method not allowed", 405)
	}
}

// --- MSG ---

func (s *Server) handleMsg(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST only", 405)
		return
	}

	var req struct {
		To      string `json:"to"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", 400)
		return
	}

	// Ищем пира в БД
	peerInfo, err := peer.GetPeer(s.db, req.To)
	if err != nil {
		http.Error(w, "peer not found: "+req.To, 404)
		return
	}

	// Если TCP-адрес не известен — ищем через DHT
	tcpAddr := peerInfo.TCPHost
	if tcpAddr == "" && s.meshSrv != nil {
		if addrs, ok := s.meshSrv.FindPeer(req.To); ok && len(addrs) > 0 {
			tcpAddr = net.JoinHostPort(addrs[0].IP.String(), "24554")
			// Обновляем в БД
			peer.UpdatePeerTCP(s.db, req.To, tcpAddr)
		}
	}

	msg := boomex.Message{
		Type:      boomex.TypeMSG,
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		From:      s.myAddress,
		To:        req.To,
		Subject:   req.Subject,
		Body:      req.Body,
		Timestamp: time.Now(),
	}

	if tcpAddr == "" {
		// Сохраняем локально, ждём когда пир появится
		if s.db != nil {
			if err := boomex.StoreMessageForRelay(s.db, &msg); err != nil {
				http.Error(w, "store failed: "+err.Error(), 500)
				return
			}
			fmt.Fprintf(w, "Peer offline. Message stored for relay.\n")
			return
		}
		http.Error(w, "peer unreachable and no relay configured", 503)
		return
	}

	// Отправляем напрямую
	fmt.Printf("Sending message to %s at %s...\n", req.To, tcpAddr)
	if err := boomex.SendMessageToPeer(tcpAddr, s.myAddress, msg); err != nil {
		http.Error(w, "send failed: "+err.Error(), 500)
		return
	}

	fmt.Fprintf(w, "Message sent to %s\n", req.To)
}

// --- STATUS ---

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"address":    s.myAddress,
		"status":     "online",
		"dht_peers":  len(s.dht.GetAll()),
		"mesh_peers": s.meshSrv.ListPeers(),
	})
}

// --- MESH ---

func (s *Server) handleMesh(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		peers := s.meshSrv.ListPeers()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(peers)
		return
	}

	if r.Method == "POST" {
		var req struct {
			Address string `json:"address"`
			UDPAddr string `json:"udp_addr"`
			IsRelay bool   `json:"is_relay"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", 400)
			return
		}

		addr, err := net.ResolveUDPAddr("udp", req.UDPAddr)
		if err != nil {
			http.Error(w, "invalid UDP address", 400)
			return
		}

		s.meshSrv.AddPeer(req.Address, *addr, req.IsRelay)
		fmt.Fprintf(w, "Mesh peer %s added\n", req.Address)
		return
	}

	http.Error(w, "method not allowed", 405)
}

// --- EXPORT (сникернет) ---

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST only", 405)
		return
	}

	var req struct {
		To string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", 400)
		return
	}

	// Собираем сообщения для указанного пира из outbox
	msgs, err := boomex.FetchMessagesForRelay(s.db, req.To)
	if err != nil {
		http.Error(w, "fetch error: "+err.Error(), 500)
		return
	}

	if len(msgs) == 0 {
		http.Error(w, "no messages to export", 404)
		return
	}

	// Сериализуем в BoomPack
	data, err := json.Marshal(msgs)
	if err != nil {
		http.Error(w, "marshal error: "+err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=BoomMail_%s_to_%s_%d.bpack", s.myAddress, req.To, time.Now().Unix()))
	w.Write(data)
}

// --- IMPORT (сникернет) ---

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST only", 405)
		return
	}

	r.ParseMultipartForm(10 << 20) // 10 MB
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file required", 400)
		return
	}
	defer file.Close()

	var msgs []boomex.Message
	if err := json.NewDecoder(file).Decode(&msgs); err != nil {
		http.Error(w, "invalid BoomPack file", 400)
		return
	}

	// Сохраняем сообщения
	for _, msg := range msgs {
		boomex.StoreMessageForRelay(s.db, &msg)
		fmt.Printf("Imported: %s -> %s: %s\n", msg.From, msg.To, msg.Subject)
	}

	fmt.Fprintf(w, "Imported %d messages\n", len(msgs))
}
