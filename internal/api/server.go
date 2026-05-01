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
	"github.com/BSXLAbS2025/boomnode/internal/echo"
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
	http.HandleFunc("/api/echoes", s.handleEchoes)
	http.HandleFunc("/api/echo", s.handleEchoPost)
	http.HandleFunc("/", s.handleWeb)

	fmt.Printf("API server started on %s\n", s.addr)
	fmt.Printf("Web UI: http://%s/\n", s.addr)
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
		var tcpHost string
		if s.meshSrv != nil {
			if addrs, ok := s.meshSrv.FindPeer(req.Address); ok && len(addrs) > 0 {
				tcpHost = net.JoinHostPort(addrs[0].IP.String(), "24554")
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
	peerInfo, err := peer.GetPeer(s.db, req.To)
	if err != nil {
		http.Error(w, "peer not found", 404)
		return
	}
	tcpAddr := peerInfo.TCPHost
	if tcpAddr == "" && s.meshSrv != nil {
		if addrs, ok := s.meshSrv.FindPeer(req.To); ok && len(addrs) > 0 {
			tcpAddr = net.JoinHostPort(addrs[0].IP.String(), "24554")
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
		if s.db != nil {
			boomex.StoreMessageForRelay(s.db, &msg)
			fmt.Fprintf(w, "Peer offline. Message stored for relay.\n")
			return
		}
		http.Error(w, "peer unreachable", 503)
		return
	}
	if err := boomex.SendMessageToPeer(tcpAddr, s.myAddress, msg); err != nil {
		http.Error(w, "send failed: "+err.Error(), 500)
		return
	}
	fmt.Fprintf(w, "Message sent to %s\n", req.To)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	dhtPeers := 0
	if s.dht != nil {
		dhtPeers = len(s.dht.GetAll())
	}
	meshPeers := []*mesh.MeshPeer{}
	if s.meshSrv != nil {
		meshPeers = s.meshSrv.ListPeers()
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"address":    s.myAddress,
		"status":     "online",
		"dht_peers":  dhtPeers,
		"mesh_peers": meshPeers,
	})
}

func (s *Server) handleMesh(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		if s.meshSrv == nil {
			http.Error(w, "BoomMesh not enabled", 503)
			return
		}
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
		if s.meshSrv == nil {
			http.Error(w, "BoomMesh not enabled", 503)
			return
		}
		s.meshSrv.AddPeer(req.Address, *addr, req.IsRelay)
		fmt.Fprintf(w, "Mesh peer %s added\n", req.Address)
		return
	}
	http.Error(w, "method not allowed", 405)
}

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
	msgs, err := boomex.FetchMessagesForRelay(s.db, req.To)
	if err != nil {
		http.Error(w, "fetch error", 500)
		return
	}
	if len(msgs) == 0 {
		http.Error(w, "no messages to export", 404)
		return
	}
	data, err := json.Marshal(msgs)
	if err != nil {
		http.Error(w, "marshal error", 500)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=BoomMail_%s_to_%s_%d.bpack", s.myAddress, req.To, time.Now().Unix()))
	w.Write(data)
}

func (s *Server) handleEchoes(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		echoes, err := echo.ListEchoes(s.db)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(echoes)
		return
	}
	if r.Method == "POST" {
		var req struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", 400)
			return
		}
		if err := echo.Subscribe(s.db, req.Name, req.Description); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		fmt.Fprintf(w, "Subscribed to %s\n", req.Name)
		return
	}
	http.Error(w, "method not allowed", 405)
}

func (s *Server) handleEchoPost(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST only", 405)
		return
	}
	var req struct {
		Area    string `json:"area"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", 400)
		return
	}
	if err := echo.PostToEcho(s.db, req.Area, s.myAddress, req.Subject, req.Body); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	fmt.Fprintf(w, "Posted to %s\n", req.Area)
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST only", 405)
		return
	}
	r.ParseMultipartForm(10 << 20)
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
	for _, msg := range msgs {
		boomex.StoreMessageForRelay(s.db, &msg)
	}
	fmt.Fprintf(w, "Imported %d messages\n", len(msgs))
}

func (s *Server) handleWeb(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, webUI)
}

const webUI = `<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="UTF-8">
<title>BoomNode</title>
<style>
body{font-family:monospace;background:#0a0a0a;color:#0f0;max-width:800px;margin:20px auto;padding:20px}
h1{color:#0f8;text-align:center}
.section{border:1px solid #0f0;padding:15px;margin:10px 0;border-radius:8px;background:#0d0d0d}
input,textarea,button{background:#111;color:#0f0;border:1px solid #0f0;padding:10px;margin:5px 0;width:100%;box-sizing:border-box;font-family:inherit;border-radius:4px}
button{background:#030;cursor:pointer}
button:hover{background:#050}
pre{background:#111;padding:10px;white-space:pre-wrap;border-radius:4px}
.badge{display:inline-block;padding:2px 8px;border-radius:4px;font-size:12px}
.on{background:#030;color:#0f0}
.off{background:#300;color:#f00}
</style>
</head>
<body>
<h1>BoomNode</h1>
<div id="status" class="section">Loading...</div>
<div class="section">
<h3>Add Peer</h3>
<input id="peerAddr" placeholder="BM address"><input id="peerName" placeholder="Name">
<button onclick="addPeer()">Add</button>
</div>
<div class="section">
<h3>Send Message</h3>
<input id="msgTo" placeholder="BM address"><input id="msgSubject" placeholder="Subject">
<textarea id="msgBody" rows="3" placeholder="Body"></textarea>
<button onclick="sendMsg()">Send</button>
</div>
<div class="section"><h3>Peers</h3><pre id="peers">Loading...</pre></div>
<div class="section"><h3>DHT Mesh</h3><pre id="mesh">Loading...</pre></div>
<div class="section">
<h3>Sneakernet</h3>
<input id="exportTo" placeholder="BM address"><button onclick="exportMail()">Export</button>
<input type="file" id="importFile"><button onclick="importMail()">Import</button>
</div>
<script>
const A="http://127.0.0.1:24555";
async function S(){try{let r=await fetch(A+"/api/status");let d=await r.json();document.getElementById("status").innerHTML="<b>Address:</b> "+d.address+" <span class='badge on'>"+d.status+"</span> | <b>DHT:</b> "+d.dht_peers}catch(e){document.getElementById("status").innerHTML="<span class='badge off'>OFFLINE</span>"}}
async function addPeer(){let a=document.getElementById("peerAddr").value,n=document.getElementById("peerName").value||"Unknown";if(!a)return;await fetch(A+"/api/peers",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({address:a,name:n})});L()}
async function sendMsg(){let t=document.getElementById("msgTo").value,s=document.getElementById("msgSubject").value,b=document.getElementById("msgBody").value;if(!t||!s||!b)return;await fetch(A+"/api/msg",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({to:t,subject:s,body:b})})}
async function L(){try{let r=await fetch(A+"/api/peers");document.getElementById("peers").textContent=JSON.stringify(await r.json(),null,2)}catch(e){}}
async function M(){try{let r=await fetch(A+"/api/mesh");document.getElementById("mesh").textContent=JSON.stringify(await r.json(),null,2)}catch(e){}}
async function exportMail(){let t=document.getElementById("exportTo").value;if(!t)return;let r=await fetch(A+"/api/export",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({to:t})});if(!r.ok)return;let b=await r.blob(),u=window.URL.createObjectURL(b),a=document.createElement("a");a.href=u;a.download="BoomMail.bpack";a.click()}
async function importMail(){let f=document.getElementById("importFile").files[0];if(!f)return;let d=new FormData();d.append("file",f);await fetch(A+"/api/import",{method:"POST",body:d})}
S();L();M();setInterval(S,5000)
</script>
</body>
</html>`
