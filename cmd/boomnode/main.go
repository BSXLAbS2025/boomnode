package main

import (
	"encoding/json"
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
		handlePeerCommand()
	case "msg":
		handleMsgCommand()
	case "export":
		handleExportCommand()
	case "import":
		handleImportCommand()
	case "status":
		handleStatusCommand()
	case "mesh":
		handleMeshCommand()
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printUsage()
	}
}

func printUsage() {
	fmt.Println("BoomNode - BoomNet node")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  run                              Start node")
	fmt.Println("  status                           Show node status")
	fmt.Println("  peer add <bm-addr> <name>        Add peer by BM address")
	fmt.Println("  peer list                        List peers")
	fmt.Println("  msg <bm-addr> <subject> <body>   Send message to peer")
	fmt.Println("  export <bm-addr> [file]          Export messages for sneakernet")
	fmt.Println("  import <file>                    Import messages from sneakernet")
	fmt.Println("  mesh list                        List known DHT peers")
	fmt.Println("  mesh add <addr> <udp_addr>       Add DHT peer manually")
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
	fmt.Printf("Mode:      %s\n", cfg.Node.Mode)
	fmt.Println()

	dht := mesh.NewDHT()
	isRelay := cfg.Node.Mode == "relay"
	meshSrv, err := mesh.NewServer("0.0.0.0:24553", dht, address, pubKeyStr, isRelay)
	if err != nil {
		fmt.Printf("BoomMesh error: %v\n", err)
		os.Exit(1)
	}
	if err := meshSrv.Start(); err != nil {
		fmt.Printf("BoomMesh start error: %v\n", err)
		os.Exit(1)
	}
	defer meshSrv.Stop()

	handler := func(session *boomex.Session, msg *boomex.Message) {
		fmt.Printf("=== INCOMING MESSAGE ===\nFrom: %s\nSubject: %s\nBody: %s\n=========================\n", msg.From, msg.Subject, msg.Body)
		if msg.To != address && store.DB() != nil {
			boomex.StoreMessageForRelay(store.DB(), msg)
			fmt.Printf("Stored relay message: %s -> %s\n", msg.From, msg.To)
		}
	}

	boomexSrv := boomex.NewServer("0.0.0.0:24554", address, store.DB(), isRelay, cfg.Relay.Whitelist, handler)
	if err := boomexSrv.Start(); err != nil {
		fmt.Printf("BoomEx server error: %v\n", err)
		os.Exit(1)
	}

	apiSrv := api.NewServer("127.0.0.1:24555", store.DB(), keys, address, dht, meshSrv)
	go func() {
		if err := apiSrv.Start(); err != nil {
			fmt.Printf("API server error: %v\n", err)
			os.Exit(1)
		}
	}()

	fmt.Println("Node is running. Press Ctrl+C to exit.")
	fmt.Println("Services: BoomEx :24554 | BoomMesh :24553 | API :24555")
	select {}
}

func handlePeerCommand() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: bn peer add <bm-addr> <name>")
		fmt.Println("       bn peer list")
		os.Exit(1)
	}

	cfg, _ := config.Load("boomnode.yaml")

	switch os.Args[2] {
	case "add":
		if len(os.Args) < 5 {
			fmt.Println("Usage: bn peer add <bm-addr> <name>")
			os.Exit(1)
		}
		store, _ := storage.Open(cfg.Storage.DataDir, false)
		defer store.Close()

		p := peer.PeerInfo{
			Address:    os.Args[3],
			Name:       os.Args[4],
			TrustLevel: 5,
			Transport:  "auto",
			DateAdded:  time.Now(),
		}
		peer.AddPeer(store.DB(), p)
		fmt.Printf("Peer %s added!\n", os.Args[3])

	case "list":
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
}
func handleMsgCommand() {
	if len(os.Args) < 5 {
		fmt.Println("Usage: bn msg <bm-addr> <subject> <body>")
		fmt.Println("       bn msg tcp:<host:port> <subject> <body>")
		os.Exit(1)
	}

	cfg, _ := config.Load("boomnode.yaml")
	keys, _ := crypto.LoadKeys(cfg.Storage.DataDir)
	from := crypto.AddressFromKey(keys.PublicKey, cfg.Node.Geo)
	to := os.Args[2]

	var tcpAddr string

	// Проверяем, прямой TCP или BM-адрес
	if len(to) > 4 && to[:4] == "tcp:" {
		// Прямая отправка: bn msg tcp:127.0.0.1:24554 "Hello" "World"
		tcpAddr = to[4:]
	} else {
		// Поиск пира в БД
		store, _ := storage.Open(cfg.Storage.DataDir, true)
		defer store.Close()

		peerInfo, err := peer.GetPeer(store.DB(), to)
		if err != nil {
			fmt.Printf("Peer %s not found. Add it first:\n  bn peer add %s <name>\n", to, to)
			fmt.Printf("Or send directly:\n  bn msg tcp:<host:port> \"subject\" \"body\"\n")
			os.Exit(1)
		}

		tcpAddr = peerInfo.TCPHost
		if tcpAddr == "" {
			fmt.Printf("No route to %s. Peer is offline.\n", to)
			fmt.Printf("Try direct TCP: bn msg tcp:<host:port> \"subject\" \"body\"\n")
			os.Exit(1)
		}
	}

	msg := boomex.Message{
		Type:      boomex.TypeMSG,
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		From:      from,
		To:        to,
		Subject:   os.Args[3],
		Body:      os.Args[4],
		Timestamp: time.Now(),
	}

	fmt.Printf("Sending to %s...\n", tcpAddr)
	if err := boomex.SendMessageToPeer(tcpAddr, from, msg); err != nil {
		fmt.Printf("Send error: %v\n", err)
		os.Exit(1)
	}
}

func handleExportCommand() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: bn export <bm-addr> [output-file]")
		os.Exit(1)
	}

	cfg, _ := config.Load("boomnode.yaml")
	store, _ := storage.Open(cfg.Storage.DataDir, true)
	defer store.Close()

	keys, _ := crypto.LoadKeys(cfg.Storage.DataDir)
	from := crypto.AddressFromKey(keys.PublicKey, cfg.Node.Geo)
	to := os.Args[2]

	msgs, err := boomex.FetchMessagesForRelay(store.DB(), to)
	if err != nil || len(msgs) == 0 {
		fmt.Println("No messages to export.")
		os.Exit(0)
	}

	filename := fmt.Sprintf("BoomMail_%s_to_%s.bpack", from, to)
	if len(os.Args) > 3 {
		filename = os.Args[3]
	}

	data, _ := json.Marshal(msgs)
	os.WriteFile(filename, data, 0644)
	fmt.Printf("Exported %d messages to %s\n", len(msgs), filename)
}

func handleImportCommand() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: bn import <file.bpack>")
		os.Exit(1)
	}

	cfg, _ := config.Load("boomnode.yaml")
	store, _ := storage.Open(cfg.Storage.DataDir, false)
	defer store.Close()

	data, err := os.ReadFile(os.Args[2])
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		os.Exit(1)
	}

	var msgs []boomex.Message
	if err := json.Unmarshal(data, &msgs); err != nil {
		fmt.Printf("Invalid BoomPack file: %v\n", err)
		os.Exit(1)
	}

	for _, msg := range msgs {
		boomex.StoreMessageForRelay(store.DB(), &msg)
	}
	fmt.Printf("Imported %d messages.\n", len(msgs))
}

func handleStatusCommand() {
	cfg, _ := config.Load("boomnode.yaml")
	keys, _ := crypto.LoadKeys(cfg.Storage.DataDir)
	address := crypto.AddressFromKey(keys.PublicKey, cfg.Node.Geo)

	fmt.Printf("Address:   %s\n", address)
	fmt.Printf("Node name: %s\n", cfg.Node.Name)
	fmt.Printf("Mode:      %s\n", cfg.Node.Mode)
	fmt.Printf("Data dir:  %s\n", cfg.Storage.DataDir)
}

func handleMeshCommand() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: bn mesh list")
		fmt.Println("       bn mesh add <bm-addr> <udp_addr>")
		os.Exit(1)
	}

	switch os.Args[2] {
	case "list":
		fmt.Println("DHT peers are only available when node is running.")
		fmt.Println("Use: curl http://127.0.0.1:24555/api/mesh")
	case "add":
		if len(os.Args) < 5 {
			fmt.Println("Usage: bn mesh add <bm-addr> <udp_addr:port>")
			os.Exit(1)
		}
		fmt.Printf("To add DHT peer, use API:\n  curl -X POST http://127.0.0.1:24555/api/mesh -H 'Content-Type: application/json' -d '{\"address\":\"%s\",\"udp_addr\":\"%s\"}'\n", os.Args[3], os.Args[4])
	}
}
