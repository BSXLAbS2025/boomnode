package main

import (
	"fmt"
	"os"
	"time"

	"github.com/BSXLAbS2025/boomnode/internal/boomex"
	"github.com/BSXLAbS2025/boomnode/internal/config"
	"github.com/BSXLAbS2025/boomnode/internal/crypto"
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
			fmt.Println("Usage: bn peer add|list")
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
			fmt.Println("Example: bn msg 127.0.0.1:24554 Hello World")
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
	fmt.Println("  run                            Start node")
	fmt.Println("  peer add <addr> <name> <tcp>   Add peer (server must be stopped)")
	fmt.Println("  peer list                      List peers (server must be stopped)")
	fmt.Println("  msg <tcp-addr> <subject> <body> Send message directly to TCP address")
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

	fmt.Println("=== BoomNode v0.2.0-alpha ===")
	fmt.Println("BoomNet: Where Ideas Detonate")
	fmt.Println()
	fmt.Printf("Address:   %s\n", address)
	fmt.Printf("Node name: %s\n", cfg.Node.Name)
	fmt.Println()

	handler := func(session *boomex.Session, msg *boomex.Message) {
		fmt.Printf("=== INCOMING MESSAGE ===\n")
		fmt.Printf("From:    %s\n", msg.From)
		fmt.Printf("To:      %s\n", msg.To)
		fmt.Printf("Subject: %s\n", msg.Subject)
		fmt.Printf("Body:    %s\n", msg.Body)
		fmt.Printf("=========================\n")
	}

	server := boomex.NewServer("0.0.0.0:24554", address, handler)
	if err := server.Start(); err != nil {
		fmt.Printf("Server error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Node is running. Press Ctrl+C to exit.")
	select {}
}

func addPeer(address, name, tcpAddr string) {
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

	p := peer.PeerInfo{
		Address:    address,
		Name:       name,
		TrustLevel: 5,
		Transport:  "tcp",
		TCPHost:    tcpAddr,
		DateAdded:  time.Now(),
	}

	if err := peer.AddPeer(store.DB(), p); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Peer %s added!\n", address)
}

func listPeers() {
	cfg, err := config.Load("boomnode.yaml")
	if err != nil {
		fmt.Printf("Config error: %v\n", err)
		os.Exit(1)
	}

	store, err := storage.Open(cfg.Storage.DataDir, true)
	if err != nil {
		fmt.Printf("Storage error: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	peers, err := peer.ListPeers(store.DB())
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	if len(peers) == 0 {
		fmt.Println("No peers.")
		return
	}
	for _, p := range peers {
		fmt.Printf("  %s (%s) - %s [trust: %d]\n", p.Address, p.Name, p.TCPHost, p.TrustLevel)
	}
}

func sendMessageDirect(tcpAddr, subject, body string) {
	// Загружаем только конфиг и ключи (без БД, чтобы не блокировать сервер)
	cfg, err := config.Load("boomnode.yaml")
	if err != nil {
		fmt.Printf("Config error: %v\n", err)
		os.Exit(1)
	}

	keys, err := crypto.LoadKeys(cfg.Storage.DataDir)
	if err != nil {
		fmt.Printf("Keys error: %v\n", err)
		os.Exit(1)
	}

	from := crypto.AddressFromKey(keys.PublicKey, cfg.Node.Geo)

	// Создаём сообщение без указания To, подставим адрес из подключения
	msg := boomex.Message{
		Type:      boomex.TypeMSG,
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		From:      from,
		To:        "", // Будет заполнено из HELO ответа или проигнорировано
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
