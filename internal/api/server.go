package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/BSXLAbS2025/boomnode/internal/boomex"
	"github.com/BSXLAbS2025/boomnode/internal/crypto"
	"github.com/BSXLAbS2025/boomnode/internal/peer"
	bolt "go.etcd.io/bbolt"
)

type Server struct {
	addr      string
	db        *bolt.DB
	keys      *crypto.NodeKeys
	myAddress string
}

func NewServer(addr string, db *bolt.DB, keys *crypto.NodeKeys, myAddress string) *Server {
	return &Server{
		addr:      addr,
		db:        db,
		keys:      keys,
		myAddress: myAddress,
	}
}

func (s *Server) Start() error {
	http.HandleFunc("/api/peers", s.handlePeers)
	http.HandleFunc("/api/msg", s.handleMsg)
	http.HandleFunc("/api/status", s.handleStatus)

	fmt.Printf("API server started on %s\n", s.addr)
	return http.ListenAndServe(s.addr, nil)
}

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
		var p peer.PeerInfo
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, "invalid JSON", 400)
			return
		}
		p.TrustLevel = 5
		p.Transport = "tcp"
		p.DateAdded = time.Now()

		if err := peer.AddPeer(s.db, p); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(201)
		fmt.Fprintf(w, "Peer %s added\n", p.Address)

	default:
		http.Error(w, "method not allowed", 405)
	}
}

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

	// Ищем TCP пира
	peerInfo, err := peer.GetPeer(s.db, req.To)
	if err != nil {
		http.Error(w, "peer not found: "+req.To, 404)
		return
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

	if err := boomex.SendMessageToPeer(peerInfo.TCPHost, s.myAddress, msg); err != nil {
		http.Error(w, "send failed: "+err.Error(), 500)
		return
	}

	w.WriteHeader(200)
	fmt.Fprintf(w, "Message sent to %s\n", req.To)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"address": s.myAddress,
		"status":  "online",
	})
}
