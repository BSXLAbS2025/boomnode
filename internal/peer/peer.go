package peer

import (
	"encoding/json"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

// PeerInfo — информация о пире
type PeerInfo struct {
	Address     string    `json:"address"`
	Name        string    `json:"name"`
	PublicKey   string    `json:"public_key"`
	TrustLevel  int       `json:"trust_level"` // 0 = untrusted, 10 = full
	LastSeen    time.Time `json:"last_seen"`
	Transport   string    `json:"transport"`    // tcp, mesh, sneakernet
	TCPHost     string    `json:"tcp_host"`     // host:port для TCP
	DateAdded   time.Time `json:"date_added"`
}

// AddPeer добавляет нового пира в базу данных
func AddPeer(db *bolt.DB, p PeerInfo) error {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("peers"))
		if b == nil {
			return fmt.Errorf("bucket 'peers' не найден")
		}

		// Проверяем, существует ли уже такой пир
		if existing := b.Get([]byte(p.Address)); existing != nil {
			return fmt.Errorf("пир %s уже существует", p.Address)
		}

		data, err := json.Marshal(p)
		if err != nil {
			return fmt.Errorf("не могу сериализовать пира: %w", err)
		}

		return b.Put([]byte(p.Address), data)
	})
}

// GetPeer получает информацию о пире по его адресу
func GetPeer(db *bolt.DB, address string) (*PeerInfo, error) {
	var peer PeerInfo
	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("peers"))
		if b == nil {
			return fmt.Errorf("bucket 'peers' не найден")
		}
		data := b.Get([]byte(address))
		if data == nil {
			return fmt.Errorf("пир %s не найден", address)
		}
		return json.Unmarshal(data, &peer)
	})
	if err != nil {
		return nil, err
	}
	return &peer, nil
}

// ListPeers возвращает список всех пиров
func ListPeers(db *bolt.DB) ([]PeerInfo, error) {
	var peers []PeerInfo
	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("peers"))
		if b == nil {
			return fmt.Errorf("bucket 'peers' не найден")
		}
		return b.ForEach(func(k, v []byte) error {
			var p PeerInfo
			if err := json.Unmarshal(v, &p); err != nil {
				return err
			}
			peers = append(peers, p)
			return nil
		})
	})
	return peers, err
}
