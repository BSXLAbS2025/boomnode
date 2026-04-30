package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/pem"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
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

// SaveKeys сохраняет ключи в файлы
func SaveKeys(keys *NodeKeys, dir string) error {
	privBytes, err := ssh.MarshalPrivateKey(keys.PrivateKey, "BoomNet private key")
	if err != nil {
		return fmt.Errorf("не могу маршалить приватный ключ: %w", err)
	}
	privPath := dir + "/boomnode_ed25519"
	if err := os.WriteFile(privPath, pem.EncodeToMemory(privBytes), 0600); err != nil {
		return fmt.Errorf("не могу записать приватный ключ: %w", err)
	}

	pubKey, err := ssh.NewPublicKey(keys.PublicKey)
	if err != nil {
		return fmt.Errorf("не могу создать SSH-ключ: %w", err)
	}
	pubBytes := ssh.MarshalAuthorizedKey(pubKey)
	pubPath := dir + "/boomnode_ed25519.pub"
	if err := os.WriteFile(pubPath, pubBytes, 0644); err != nil {
		return fmt.Errorf("не могу записать публичный ключ: %w", err)
	}

	return nil
}

// LoadKeys загружает ключи из файлов
func LoadKeys(dir string) (*NodeKeys, error) {
	privPath := dir + "/boomnode_ed25519"
	privPEM, err := os.ReadFile(privPath)
	if err != nil {
		return nil, fmt.Errorf("не могу прочитать приватный ключ: %w", err)
	}
	privBlock, _ := pem.Decode(privPEM)
	privKey, err := ssh.ParseRawPrivateKey(privBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("не могу распарсить приватный ключ: %w", err)
	}

	ed25519Key, ok := privKey.(*ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("приватный ключ не Ed25519")
	}
	return &NodeKeys{
		PublicKey:  ed25519Key.Public().(ed25519.PublicKey),
		PrivateKey: *ed25519Key,
	}, nil
}

// AddressFromKey генерирует BM-адрес
func AddressFromKey(pub ed25519.PublicKey, geo string) string {
	hash := sha256.Sum256(pub)
	encoded := strings.ToUpper(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(hash[:5]))
	return fmt.Sprintf("BM-%s-%s", geo, encoded)
}
