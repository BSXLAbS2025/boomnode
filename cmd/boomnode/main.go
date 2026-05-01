package main

import (
	"fmt"
	"os"
	"time"

	"github.com/BSXLAbS2025/boomnode/internal/api"
	"github.com/BSXLAbS2025/boomnode/internal/boomex"
	"github.com/BSXLAbS2025/boomnode/internal/config"
	"github.com/BSXLAbS2025/boomnode/internal/crypto"
	"github.com/BSXLAbS2025/boomnode/internal/mesh"
	"github.com/BSXLAbS2025/boomnode/internal/peer"
	"github.com/BSXLAbS2025/boomnode/internal/storage"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "run":
		runNode()
	case "peer":
		if len(os.Args) < 3 {
			fmt.Println("Usage: bn peer add|list (server must be stopped)")
			os.Exit(1)
		}
		switch os.Args[2] {
		case "add":
			if len(os.Args) < 5 {
				fmt.Println("Usage: bn peer add <addr> <name> <tcp>")
				os.Exit(1)
			}
			addPeer(os.Args[3], os.Args[4], os.Args[5])
		case "list":
			listPeers()
		default:
			fmt.Printf("Unknown peer command: %s\n", os.Args[2])
		}
	case "msg":
		if len(os.Args) < 5 {
			fmt.Println("Usage: bn msg <tcp-addr:port> <subject> <body>")
			fmt.Println("Or use API: curl -X POST http://127.0.0.1:24555/api/msg ...")
			os.Exit(1)
		}
		sendMessageDirect(os.Args[2], os.Args[3], os.Args[4])
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printUsage()
	}
}

func printUsage() {
	fmt.Println("BoomNode - BoomNet node")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  run                          Start node (BoomEx + BoomMesh + API)")
	fmt.Println("  peer add <addr> <name> <tcp> Add peer (offline)")
	fmt.Println("  peer list                    List peers (offline)")
	fmt.Println("  msg <tcp-addr> <subj> <body> Send direct message")
	fmt.Println()
	fmt.Println("API (when running):")
	fmt.Println("  GET  http://127.0.0.1:24555/api/status")
	fmt.Println("  GET  http://127.0.0.1:24555/api/peers")
	fmt.Println("  POST http://127.0.0.1:24555/api/peers")
	fmt.Println("  POST http://127.0.0.1:24555/api/msg")
	fmt.Println("  GET  http://127.0.0.1:24555/api/mesh")
	fmt.Println("  POST http://127.0.0.1:24555/api/export (sneakernet)")
	fmt.Println("  POST http://127.0.0.1:24555/api/import (sneakernet)")
}

func runNode() {
	cfg, err := config.Load("boomnode.yaml")
	if err != nil {
		fmt.Printf("Config error: %v\n", err)
		os.Exit(1)
	}

	store, err := storage.Open(cfg.Storage.DataDir, false)
	if err != nil {
		fmt.Printf("Storage error: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	keys, err := crypto.LoadKeys(cfg.Storage.DataDir)
	if err != nil {
		keys, _ = crypto.GenerateKeys()
		crypto.SaveKeys(keys, cfg.Storage.DataDir)
	}

	address := crypto.AddressFromKey(keys.PublicKey, cfg.Node.Geo)
	pubKeyStr := fmt.Sprintf("%x", keys.PublicKey)

	fmt.Println("=== BoomNode v0.3.0-alpha ===")
	fmt.Println("BoomNet: Where Ideas Detonate")
	fmt.Println()
	fmt.Printf("Address:   %s\n", address)
	fmt.Printf("Node name: %s\n", cfg.Node.Name)
	fmt.Println()

	// --- BoomMesh (P2P-оверлей) ---
	dht := mesh.NewDHT()
	meshSrv, err := mesh.NewServer("0.0.0.0:24553", dht, address, pubKeyStr, true)
	if err != nil {
		fmt.Printf("BoomMesh error: %v\n", err)
		os.Exit(1)
	}
	if err := meshSrv.Start(); err != nil {
		fmt.Printf("BoomMesh start error: %v\n", err)
		os.Exit(1)
	}
	defer meshSrv.Stop()

	// --- BoomEx (почтовый протокол) ---
	handler := func(session *boomex.Session, msg *boomex.Message) {
		fmt.Printf("=== INCOMING MESSAGE ===\n")
		fmt.Printf("From:    %s\n", msg.From)
		fmt.Printf("To:      %s\n", msg.To)
		fmt.Printf("Subject: %s\n", msg.Subject)
		fmt.Printf("Body:    %s\n", msg.Body)
		fmt.Printf("=========================\n")

		if msg.To != address && store.DB() != nil {
			boomex.StoreMessageForRelay(store.DB(), msg)
			fmt.Printf("Stored relay message: %s -> %s\n", msg.From, msg.To)
		}
	}

	isRelay := cfg.Node.Mode == "relay"
	boomexSrv := boomex.NewServer("0.0.0.0:24554", address, store.DB(), isRelay, cfg.Relay.Whitelist, handler)
	if err := boomexSrv.Start(); err != nil {
		fmt.Printf("BoomEx server error: %v\n", err)
		os.Exit(1)
	}

	// --- API ---
	apiSrv := api.NewServer("127.0.0.1:24555", store.DB(), keys, address, dht, meshSrv)
	go func() {
		if err := apiSrv.Start(); err != nil {
			fmt.Printf("API server error: %v\n", err)
			os.Exit(1)
		}
	}()

	fmt.Println("Node is running. Press Ctrl+C to exit.")
	fmt.Println()
	fmt.Println("Services:")
	fmt.Println("  BoomEx  : TCP 0.0.0.0:24554 (почта)")
	fmt.Println("  BoomMesh: UDP 0.0.0.0:24553 (P2P-оверлей)")
	fmt.Println("  API     : HTTP 127.0.0.1:24555 (управление)")
	fmt.Println("  Sneaker : Export/Import .bpack files")
	select {}
}

func addPeer(address, name, tcpAddr string) {
	cfg, _ := config.Load("boomnode.yaml")
	store, _ := storage.Open(cfg.Storage.DataDir, false)
	defer store.Close()

	p := peer.PeerInfo{
		Address:    address,
		Name:       name,
		TrustLevel: 5,
		Transport:  "tcp",
		TCPHost:    tcpAddr,
		DateAdded:  time.Now(),
	}
	peer.AddPeer(store.DB(), p)
	fmt.Printf("Peer %s added!\n", address)
}

func listPeers() {
	cfg, _ := config.Load("boomnode.yaml")
	store, _ := storage.Open(cfg.Storage.DataDir, true)
	defer store.Close()

	peers, _ := peer.ListPeers(store.DB())
	if len(peers) == 0 {
		fmt.Println("No peers.")
		return
	}
	for _, p := range peers {
		fmt.Printf("  %s (%s) - %s [%d]\n", p.Address, p.Name, p.TCPHost, p.TrustLevel)
	}
}

func sendMessageDirect(tcpAddr, subject, body string) {
	cfg, _ := config.Load("boomnode.yaml")
	keys, _ := crypto.LoadKeys(cfg.Storage.DataDir)
	from := crypto.AddressFromKey(keys.PublicKey, cfg.Node.Geo)

	msg := boomex.Message{
		Type:      boomex.TypeMSG,
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		From:      from,
		To:        "",
		Subject:   subject,
		Body:      body,
		Timestamp: time.Now(),
	}

	fmt.Printf("Sending message to %s...\n", tcpAddr)
	if err := boomex.SendMessageToPeer(tcpAddr, from, msg); err != nil {
		fmt.Printf("Send error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Message sent!")
}
