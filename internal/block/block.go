package block

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
)

type BlockEntry struct {
	Address  string `json:"address"`
	Reason   string `json:"reason"`
	BannedBy string `json:"banned_by"`
	Date     string `json:"date"`
}

type BlockList struct {
	Version    int          `json:"version"`
	Updated    string       `json:"updated"`
	Banned     []BlockEntry `json:"banned"`
	Signatures []string     `json:"signatures"` // hex-подписи Хранителей
	Quorum     int          `json:"quorum"`     // минимальное число подписей (например, 3)
}

// Guardians — список публичных ключей Хранителей (можно вынести в конфиг)
var Guardians = map[string]ed25519.PublicKey{
	"BSX":     hexToPubKey("abc123..."), // заменить на реальные ключи!
	"Guardian2": hexToPubKey("def456..."),
	"Guardian3": hexToPubKey("789abc..."),
	"Guardian4": hexToPubKey("fedcba..."),
	"Guardian5": hexToPubKey("123456..."),
}

func hexToPubKey(hexStr string) ed25519.PublicKey {
	key, _ := hex.DecodeString(hexStr)
	return ed25519.PublicKey(key)
}

func LoadBlockList(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read block.json: %w", err)
	}

	var blockList BlockList
	if err := json.Unmarshal(data, &blockList); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	// Проверяем мультиподпись
	raw := make(map[string]interface{})
	json.Unmarshal(data, &raw)
	sigs := raw["signatures"].([]interface{})
	delete(raw, "signatures")
	dataWithoutSigs, _ := json.Marshal(raw)

	validSigs := 0
	for _, sigInterface := range sigs {
		sigStr := sigInterface.(string)
		sig, _ := hex.DecodeString(sigStr)

		// Проверяем каждую подпись
		for name, pubKey := range Guardians {
			if ed25519.Verify(pubKey, dataWithoutSigs, sig) {
				validSigs++
				fmt.Printf("✅ Valid signature from Guardian: %s\n", name)
				break
			}
		}
	}

	if validSigs < blockList.Quorum {
		return nil, fmt.Errorf("not enough valid signatures: %d/%d", validSigs, blockList.Quorum)
	}

	fmt.Printf("✅ Block list verified with %d/%d signatures\n", validSigs, blockList.Quorum)

	banned := make(map[string]bool)
	for _, entry := range blockList.Banned {
		banned[entry.Address] = true
	}

	return banned, nil
}

func IsBanned(banned map[string]bool, address string) bool {
	return banned[address]
}
