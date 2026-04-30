package main

import (
	"fmt"
	"os"

	"github.com/твой_ник/boomnode/internal/config"
	"github.com/твой_ник/boomnode/internal/crypto"
)

func main() {
	fmt.Println("=== BoomNode v0.1.0-alpha ===")
	fmt.Println("BoomNet: Where Ideas Detonate")
	fmt.Println()

	// Загружаем конфиг
	cfg, err := config.Load("boomnode.yaml")
	if err != nil {
		fmt.Printf("Ошибка загрузки конфига: %v\n", err)
		os.Exit(1)
	}

	// Создаём папку для данных, если нет
	if err := os.MkdirAll(cfg.Storage.DataDir, 0700); err != nil {
		fmt.Printf("Ошибка создания папки данных: %v\n", err)
		os.Exit(1)
	}

	// Пытаемся загрузить существующие ключи, если нет — генерируем
	keys, err := crypto.LoadKeys(cfg.Storage.DataDir)
	if err != nil {
		fmt.Println("Ключи не найдены, генерирую новые...")
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

	// Генерируем BoomNet-адрес
	address := crypto.AddressFromKey(keys.PublicKey, cfg.Node.Geo)
	fmt.Printf("Ваш адрес: %s\n", address)
	fmt.Printf("Имя узла: %s\n", cfg.Node.Name)
	fmt.Println()
	fmt.Println("BoomNode готов к работе.")
}
