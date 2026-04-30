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
			fmt.Println("Использование: bn peer add|list")
			os.Exit(1)
		}
		switch os.Args[2] {
		case "add":
			if len(os.Args) < 5 {
				fmt.Println("Использование: bn peer add <адрес> <имя> <tcp-адрес>")
				os.Exit(1)
			}
			addPeer(os.Args[3], os.Args[4], os.Args[5])
		case "list":
			listPeers()
		default:
			fmt.Printf("Неизвестная подкоманда: %s\n", os.Args[2])
		}
	case "msg":
		if len(os.Args) < 5 {
			fmt.Println("Использование: bn msg <кому> <тема> <текст>")
			os.Exit(1)
		}
		sendMessage(os.Args[2], os.Args[3], os.Args[4])
	default:
		fmt.Printf("Неизвестная команда: %s\n", command)
		printUsage()
	}
}

func printUsage() {
	fmt.Println("BoomNode - узел сети BoomNet")
	fmt.Println()
	fmt.Println("Команды:")
	fmt.Println("  run                        Запустить узел")
	fmt.Println("  peer add <адр> <имя> <tcp> Добавить пира")
	fmt.Println("  peer list                  Список пиров")
	fmt.Println("  msg <кому> <тема> <текст>  Отправить сообщение")
}

func runNode() {
	cfg, err := config.Load("boomnode.yaml")
	if err != nil {
		fmt.Printf("Ошибка загрузки конфига: %v\n", err)
		os.Exit(1)
	}

	store, err := storage.Open(cfg.Storage.DataDir)
	if err != nil {
		fmt.Printf("Ошибка хранилища: %v\n", err)
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
	fmt.Printf("Адрес:     %s\n", address)
	fmt.Printf("Имя узла:  %s\n", cfg.Node.Name)
	fmt.Println()

	// Обработчик входящих сообщений
	handler := func(session *boomex.Session, msg *boomex.Message) {
		// Сохраняем сообщение в БД
		fmt.Printf("[ВХОДЯЩЕЕ] %s -> %s: %s\n", msg.From, msg.To, msg.Subject)
	}

	// Запускаем сервер
	server := boomex.NewServer("0.0.0.0:24554", handler)
	if err := server.Start(); err != nil {
		fmt.Printf("Ошибка запуска сервера: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Узел запущен. Ожидание подключений...")
	fmt.Println("(Нажми Ctrl+C для выхода)")

	select {}
}

func addPeer(address, name, tcpAddr string) {
	cfg, _ := config.Load("boomnode.yaml")
	store, _ := storage.Open(cfg.Storage.DataDir)
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
		fmt.Printf("Ошибка: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Пир %s добавлен!\n", address)
}

func listPeers() {
	cfg, _ := config.Load("boomnode.yaml")
	store, _ := storage.Open(cfg.Storage.DataDir)
	defer store.Close()

	peers, _ := peer.ListPeers(store.DB())
	if len(peers) == 0 {
		fmt.Println("Нет пиров.")
		return
	}
	for _, p := range peers {
		fmt.Printf("  %s (%s) — %s [%d]\n", p.Address, p.Name, p.TCPHost, p.TrustLevel)
	}
}

func sendMessage(to, subject, body string) {
	cfg, _ := config.Load("boomnode.yaml")
	store, _ := storage.Open(cfg.Storage.DataDir)
	defer store.Close()

	keys, err := crypto.LoadKeys(cfg.Storage.DataDir)
	if err != nil {
		fmt.Printf("Ошибка загрузки ключей: %v\n", err)
		os.Exit(1)
	}

	from := crypto.AddressFromKey(keys.PublicKey, cfg.Node.Geo)

	// Ищем пира в БД
	peerInfo, err := peer.GetPeer(store.DB(), to)
	if err != nil {
		fmt.Printf("Пир %s не найден. Сначала добавьте его: bn peer add %s <имя> <tcp>\n", to, to)
		os.Exit(1)
	}

	msg := boomex.Message{
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		From:      from,
		To:        to,
		Subject:   subject,
		Body:      body,
		Timestamp: time.Now(),
	}

	if err := boomex.SendMessageToPeer(peerInfo.TCPHost, msg); err != nil {
		fmt.Printf("Ошибка отправки: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Сообщение отправлено!")
}
