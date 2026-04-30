package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base32"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
)

// NodeKeys хранит ключи узла
type NodeKeys struct {
	PublicKey  ed25519.PublicKey
	PrivateKey ed25519.PrivateKey
}

// GenerateKeys создаёт новую пару ключей
func GenerateKeys() (*NodeKeys, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("не могу сгенерировать ключи: %w", err)
	}
	return &NodeKeys{PublicKey: pub, PrivateKey: priv}, nil
}

// SaveKeys сохраняет ключи в файлы (PKCS8 для приватного, PKIX для публичного)
func SaveKeys(keys *NodeKeys, dir string) error {
	// Сохраняем приватный ключ в формате PKCS8
	privBytes, err := x509.MarshalPKCS8PrivateKey(keys.PrivateKey)
	if err != nil {
		return fmt.Errorf("не могу маршалить приватный ключ: %w", err)
	}
	privPath := dir + "/boomnode_ed25519"
	if err := os.WriteFile(privPath, pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privBytes,
	}), 0600); err != nil {
		return fmt.Errorf("не могу записать приватный ключ: %w", err)
	}
	fmt.Printf("Приватный ключ сохранён: %s\n", privPath)

	// Сохраняем публичный ключ в формате PKIX
	pubBytes, err := x509.MarshalPKIXPublicKey(keys.PublicKey)
	if err != nil {
		return fmt.Errorf("не могу маршалить публичный ключ: %w", err)
	}
	pubPath := dir + "/boomnode_ed25519.pub"
	if err := os.WriteFile(pubPath, pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubBytes,
	}), 0644); err != nil {
		return fmt.Errorf("не могу записать публичный ключ: %w", err)
	}
	fmt.Printf("Публичный ключ сохранён: %s\n", pubPath)

	return nil
}

// LoadKeys загружает ключи из файлов
func LoadKeys(dir string) (*NodeKeys, error) {
	// Загружаем приватный ключ
	privPath := dir + "/boomnode_ed25519"
	privPEM, err := os.ReadFile(privPath)
	if err != nil {
		return nil, fmt.Errorf("не могу прочитать приватный ключ: %w", err)
	}
	privBlock, _ := pem.Decode(privPEM)
	if privBlock == nil {
		return nil, fmt.Errorf("не могу декодировать приватный ключ")
	}
	privKey, err := x509.ParsePKCS8PrivateKey(privBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("не могу распарсить приватный ключ: %w", err)
	}

	ed25519Key, ok := privKey.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("приватный ключ не Ed25519")
	}

	// Загружаем публичный ключ
	pubPath := dir + "/boomnode_ed25519.pub"
	pubPEM, err := os.ReadFile(pubPath)
	if err != nil {
		return nil, fmt.Errorf("не могу прочитать публичный ключ: %w", err)
	}
	pubBlock, _ := pem.Decode(pubPEM)
	if pubBlock == nil {
		return nil, fmt.Errorf("не могу декодировать публичный ключ")
	}
	pubKey, err := x509.ParsePKIXPublicKey(pubBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("не могу распарсить публичный ключ: %w", err)
	}

	ed25519Pub, ok := pubKey.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("публичный ключ не Ed25519")
	}

	return &NodeKeys{
		PublicKey:  ed25519Pub,
		PrivateKey: ed25519Key,
	}, nil
}

// AddressFromKey генерирует BM-адрес из публичного ключа
func AddressFromKey(pub ed25519.PublicKey, geo string) string {
	hash := sha256.Sum256(pub)
	encoded := strings.ToUpper(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(hash[:5]))
	return fmt.Sprintf("BM-%s-%s", geo, encoded)
}
