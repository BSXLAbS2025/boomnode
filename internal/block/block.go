package block

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
)

// BlockEntry — запись о бане
type BlockEntry struct {
	Address  string `json:"address"`
	Reason   string `json:"reason"`
	BannedBy string `json:"banned_by"`
	Date     string `json:"date"`
}

// BlockList — структура block.json
type BlockList struct {
	Version   int          `json:"version"`
	Updated   string       `json:"updated"`
	Signature string       `json:"signature"`
	Banned    []BlockEntry `json:"banned"`
}

// LoadBlockList загружает и проверяет подпись block.json
func LoadBlockList(path string, rootPubKey ed25519.PublicKey) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read block.json: %w", err)
	}

	// Извлекаем сигнатуру и данные без неё
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	sigStr, ok := raw["signature"].(string)
	if !ok {
		return nil, fmt.Errorf("signature missing")
	}

	delete(raw, "signature")
	dataWithoutSig, _ := json.Marshal(raw)

	// Декодируем сигнатуру из hex
	sig := make([]byte, ed25519.SignatureSize)
	n, err := fmt.Sscanf(sigStr, "%x", &sig)
	if err != nil || n != 1 {
		return nil, fmt.Errorf("invalid signature format")
	}

	// Проверяем подпись
	if !ed25519.Verify(rootPubKey, dataWithoutSig, sig) {
		return nil, fmt.Errorf("invalid block.json signature — possible tampering")
	}

	// Парсим список
	var blockList BlockList
	if err := json.Unmarshal(data, &blockList); err != nil {
		return nil, fmt.Errorf("cannot parse block.json: %w", err)
	}

	banned := make(map[string]bool)
	for _, entry := range blockList.Banned {
		banned[entry.Address] = true
	}

	return banned, nil
}

// IsBanned проверяет, забанен ли адрес
func IsBanned(banned map[string]bool, address string) bool {
	return banned[address]
}
