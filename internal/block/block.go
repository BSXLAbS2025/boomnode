package block

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
)

type Guardian struct {
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`
}

type BlockEntry struct {
	Address  string `json:"address"`
	Reason   string `json:"reason"`
	BannedBy string `json:"banned_by"`
	Date     string `json:"date"`
}

type BlockList struct {
	Version    int          `json:"version"`
	Updated    string       `json:"updated"`
	Quorum     int          `json:"quorum"`
	Guardians  []Guardian   `json:"guardians"`
	Banned     []BlockEntry `json:"banned"`
	Signatures []string     `json:"signatures"`
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

	// Преобразуем guardian'ов в ключи
	pubKeys := make(map[string]ed25519.PublicKey)
	for _, g := range blockList.Guardians {
		key, err := hex.DecodeString(g.PublicKey)
		if err != nil {
			continue
		}
		pubKeys[g.Name] = ed25519.PublicKey(key)
	}

	// Проверяем мультиподпись
	raw := make(map[string]interface{})
	json.Unmarshal(data, &raw)

	sigsRaw, ok := raw["signatures"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("signatures missing or invalid")
	}

	delete(raw, "signatures")
	dataWithoutSigs, _ := json.Marshal(raw)

	validSigs := 0
	for _, sigInterface := range sigsRaw {
		sigStr, ok := sigInterface.(string)
		if !ok {
			continue
		}
		sig, err := hex.DecodeString(sigStr)
		if err != nil {
			continue
		}

		for name, pubKey := range pubKeys {
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
