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
	http.HandleFunc("/", s.handleWeb)

	fmt.Printf("API server started on %s\n", s.addr)
	fmt.Printf("Web UI: http://%s/\n", s.addr)
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

	peerInfo, err := peer.GetPeer(s.db, req.To)
	if err != nil {
		http.Error(w, "peer not found: "+req.To, 404)
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

// --- MESH ---

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

	msgs, err := boomex.FetchMessagesForRelay(s.db, req.To)
	if err != nil {
		http.Error(w, "fetch error: "+err.Error(), 500)
		return
	}

	if len(msgs) == 0 {
		http.Error(w, "no messages to export", 404)
		return
	}

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
		fmt.Printf("Imported: %s -> %s: %s\n", msg.From, msg.To, msg.Subject)
	}

	fmt.Fprintf(w, "Imported %d messages\n", len(msgs))
}

// --- WEB UI ---

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
    <title>BoomNode Control Panel</title>
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body { font-family: 'Courier New', monospace; background: #0a0a0a; color: #00ff00; max-width: 900px; margin: 20px auto; padding: 20px; }
        h1 { color: #00ff88; text-align: center; margin-bottom: 20px; }
        .section { border: 1px solid #00ff00; border-radius: 8px; padding: 15px; margin: 10px 0; background: #0d0d0d; }
        .section h3 { color: #00ff88; margin-bottom: 10px; }
        input, textarea, button { background: #111; color: #00ff00; border: 1px solid #00ff00; border-radius: 4px; padding: 10px; margin: 5px 0; width: 100%; font-family: inherit; font-size: 14px; }
        textarea { resize: vertical; }
        button { cursor: pointer; background: #003300; font-weight: bold; }
        button:hover { background: #005500; }
        pre { background: #111; padding: 10px; white-space: pre-wrap; border-radius: 4px; min-height: 20px; }
        .row { display: flex; gap: 10px; }
        .row > * { flex: 1; }
        .badge { display: inline-block; padding: 2px 8px; border-radius: 4px; font-size: 12px; }
        .badge-online { background: #003300; color: #00ff00; }
        .badge-offline { background: #330000; color: #ff0000; }
    </style>
</head>
<body>
    <h1>🚀 BoomNode Control Panel</h1>

    <div id="status" class="section">Loading...</div>

    <div class="section">
        <h3>➕ Add Peer</h3>
        <div class="row">
            <input id="peerAddr" placeholder="BM address (BM-DE-CYBERPUNK)">
            <input id="peerName" placeholder="Name">
        </div>
        <button onclick="addPeer()">Add Peer</button>
    </div>

    <div class="section">
        <h3>📩 Send Message</h3>
        <input id="msgTo" placeholder="BM address">
        <input id="msgSubject" placeholder="Subject">
        <textarea id="msgBody" rows="3" placeholder="Message body"></textarea>
        <button onclick="sendMsg()">Send</button>
    </div>

    <div class="section">
        <h3>📋 Peers</h3>
        <pre id="peers">Loading...</pre>
    </div>

    <div class="section">
        <h3>🌐 DHT Mesh</h3>
        <pre id="mesh">Loading...</pre>
    </div>

    <div class="section">
        <h3>💾 Sneakernet</h3>
        <div class="row">
            <div>
                <input id="exportTo" placeholder="BM address to export for">
                <button onclick="exportMail()">Export .bpack</button>
            </div>
            <div>
                <input id="importFile" type="file" style="padding: 8px;">
                <button onclick="importMail()">Import .bpack</button>
            </div>
        </div>
    </div>

    <script>
        const API = "http://127.0.0.1:24555";

        async function loadStatus() {
            try {
                const r = await fetch(API + "/api/status");
                const d = await r.json();
                document.getElementById("status").innerHTML =
                    "<b>Address:</b> " + d.address +
                    " | <b>Status:</b> <span class='badge badge-online'>" + d.status + "</span>" +
                    " | <b>DHT peers:</b> " + d.dht_peers;
            } catch(e) {
                document.getElementById("status").innerHTML =
                    "<b>Status:</b> <span class='badge badge-offline'>OFFLINE</span>";
            }
        }

        async function addPeer() {
            const addr = document.getElementById("peerAddr").value.trim();
            const name = document.getElementById("peerName").value.trim() || "Unknown";
            if (!addr) { alert("Enter BM address"); return; }
            try {
                const r = await fetch(API + "/api/peers", {
                    method: "POST",
                    headers: {"Content-Type": "application/json"},
                    body: JSON.stringify({address: addr, name: name})
                });
                alert(await r.text());
                loadPeers();
            } catch(e) {
                alert("Error: " + e.message);
            }
        }

        async function sendMsg() {
            const to = document.getElementById("msgTo").value.trim();
            const subject = document.getElementById("msgSubject").value.trim();
            const body = document.getElementById("msgBody").value.trim();
            if (!to || !subject || !body) { alert("Fill all fields"); return; }
            try {
                const r = await fetch(API + "/api/msg", {
                    method: "POST",
                    headers: {"Content-Type": "application/json"},
                    body: JSON.stringify({to: to, subject: subject, body: body})
                });
                alert(await r.text());
            } catch(e) {
                alert("Error: " + e.message);
            }
        }

        async function loadPeers() {
            try {
                const r = await fetch(API + "/api/peers");
                const d = await r.json();
                document.getElementById("peers").textContent = JSON.stringify(d, null, 2);
            } catch(e) {
                document.getElementById("peers").textContent = "Error loading peers";
            }
        }

        async function loadMesh() {
            try {
                const r = await fetch(API + "/api/mesh");
                const d = await r.json();
                document.getElementById("mesh").textContent = JSON.stringify(d, null, 2);
            } catch(e) {
                document.getElementById("mesh").textContent = "Error loading mesh";
            }
        }

        async function exportMail() {
            const to = document.getElementById("exportTo").value.trim();
            if (!to) { alert("Enter BM address"); return; }
            try {
                const r = await fetch(API + "/api/export", {
                    method: "POST",
                    headers: {"Content-Type": "application/json"},
                    body: JSON.stringify({to: to})
                });
                if (!r.ok) { alert(await r.text()); return; }
                const blob = await r.blob();
                const url = window.URL.createObjectURL(blob);
                const a = document.createElement("a");
                a.href = url;
                a.download = "BoomMail.bpack";
                a.click();
                window.URL.revokeObjectURL(url);
            } catch(e) {
                alert("Error: " + e.message);
            }
        }

        async function importMail() {
            const fileInput = document.getElementById("importFile");
            const file = fileInput.files[0];
            if (!file) { alert("Select a .bpack file"); return; }
            const formData = new FormData();
            formData.append("file", file);
            try {
                const r = await fetch(API + "/api/import", {
                    method: "POST",
                    body: formData
                });
                alert(await r.text());
            } catch(e) {
                alert("Error: " + e.message);
            }
        }

        loadStatus();
        loadPeers();
        loadMesh();
        setInterval(loadStatus, 5000);
        setInterval(loadPeers, 30000);
        setInterval(loadMesh, 30000);
    </script>
</body>
</html>`
}
