package main

import (
	"fmt"
	"os"
	"time"

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
				fmt.Println("Пример: bn peer add BM-DE-CYBER CyberPunk cyberpunk.de:24554")
				os.Exit(1)
			}
			addPeer(os.Args[3], os.Args[4], os.Args[5])
		case "list":
			listPeers()
		default:
			fmt.Printf("Неизвестная подкоманда peer: %s\n", os.Args[2])
			os.Exit(1)
		}
	default:
		fmt.Printf("Неизвестная команда: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("BoomNode - узел сети BoomNet")
	fmt.Println()
	fmt.Println("Команды:")
	fmt.Println("  run                  Запустить узел")
	fmt.Println("  peer add <адр> <имя> <tcp>  Добавить пира")
	fmt.Println("  peer list            Список пиров")
}

func runNode() {
	cfg, err := config.Load("boomnode.yaml")
	if err != nil {
		fmt.Printf("Ошибка загрузки конфига: %v\n", err)
		os.Exit(1)
	}

	store, err := storage.Open(cfg.Storage.DataDir)
	if err != nil {
		fmt.Printf("Ошибка открытия хранилища: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	keys, err := crypto.LoadKeys(cfg.Storage.DataDir)
	if err != nil {
		keys, err = crypto.GenerateKeys()
		if err != nil {
			fmt.Printf("Ошибка генерации ключей: %v\n", err)
			os.Exit(1)
		}
		if err := crypto.SaveKeys(keys, cfg.Storage.DataDir); err != nil {
			fmt.Printf("Ошибка сохранения ключей: %v\n", err)
			os.Exit(1)
		}
	}

	address := crypto.AddressFromKey(keys.PublicKey, cfg.Node.Geo)

	fmt.Println("=== BoomNode v0.1.0-alpha ===")
	fmt.Println("BoomNet: Where Ideas Detonate")
	fmt.Println()
	fmt.Printf("Адрес:     %s\n", address)
	fmt.Printf("Имя узла:  %s\n", cfg.Node.Name)
	fmt.Printf("Хранилище: %s\n", cfg.Storage.DataDir)
	fmt.Println()
	fmt.Println("Узел запущен. Ожидание подключений...")
	fmt.Println("(Нажми Ctrl+C для выхода)")

	// Пока просто держим процесс живым
	select {}
}

func addPeer(address, name, tcpAddr string) {
	cfg, err := config.Load("boomnode.yaml")
	if err != nil {
		fmt.Printf("Ошибка загрузки конфига: %v\n", err)
		os.Exit(1)
	}

	store, err := storage.Open(cfg.Storage.DataDir)
	if err != nil {
		fmt.Printf("Ошибка открытия хранилища: %v\n", err)
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
		fmt.Printf("Ошибка добавления пира: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Пир %s (%s) добавлен успешно!\n", address, name)
}

func listPeers() {
	cfg, err := config.Load("boomnode.yaml")
	if err != nil {
		fmt.Printf("Ошибка загрузки конфига: %v\n", err)
		os.Exit(1)
	}

	store, err := storage.Open(cfg.Storage.DataDir)
	if err != nil {
		fmt.Printf("Ошибка открытия хранилища: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	peers, err := peer.ListPeers(store.DB())
	if err != nil {
		fmt.Printf("Ошибка получения списка пиров: %v\n", err)
		os.Exit(1)
	}

	if len(peers) == 0 {
		fmt.Println("Нет добавленных пиров.")
		return
	}

	fmt.Println("Список пиров:")
	fmt.Println("-------------")
	for _, p := range peers {
		fmt.Printf("  %s (%s) — %s [доверие: %d]\n", p.Address, p.Name, p.TCPHost, p.TrustLevel)
	}
}
