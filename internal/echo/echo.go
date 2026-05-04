package echo

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/BSXLAbS2025/boomnode/internal/block"
	"github.com/BSXLAbS2025/boomnode/internal/boomex"
	bolt "go.etcd.io/bbolt"
)

// EchoArea — эхоконференция
type EchoArea struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Moderator   string    `json:"moderator"`
	Created     time.Time `json:"created"`
	Messages    int       `json:"messages"`
}

// Subscribe подписывает узел на эху
func Subscribe(db *bolt.DB, areaName string, description string, moderator string) error {
	area := EchoArea{
		Name:        areaName,
		Description: description,
		Moderator:   moderator,
		Created:     time.Now(),
	}
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("echoes"))
		if b == nil {
			return fmt.Errorf("bucket echoes not found")
		}
		// Не перезаписываем существующую эху
		if existing := b.Get([]byte(areaName)); existing != nil {
			return nil
		}
		data, _ := json.Marshal(area)
		return b.Put([]byte(areaName), data)
	})
}

// ListEchoes возвращает список всех эх
func ListEchoes(db *bolt.DB) ([]EchoArea, error) {
	var echoes []EchoArea
	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("echoes"))
		if b == nil {
			return fmt.Errorf("bucket echoes not found")
		}
		return b.ForEach(func(k, v []byte) error {
			var area EchoArea
			if err := json.Unmarshal(v, &area); err != nil {
				return err
			}
			echoes = append(echoes, area)
			return nil
		})
	})
	return echoes, err
}

// GetEchoInfo возвращает информацию об эхе
func GetEchoInfo(db *bolt.DB, name string) (*EchoArea, error) {
	var area EchoArea
	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("echoes"))
		if b == nil {
			return fmt.Errorf("bucket echoes not found")
		}
		data := b.Get([]byte(name))
		if data == nil {
			return fmt.Errorf("echo area '%s' not found", name)
		}
		return json.Unmarshal(data, &area)
	})
	if err != nil {
		return nil, err
	}
	return &area, nil
}

// PostToEcho отправляет сообщение в эху (всем подписчикам)
func PostToEcho(db *bolt.DB, areaName string, from string, subject string, body string) error {
	msg := boomex.Message{
		Type:      boomex.TypeMSG,
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		From:      from,
		To:        areaName,
		Subject:   fmt.Sprintf("[%s] %s", areaName, subject),
		Body:      body,
		Timestamp: time.Now(),
	}

	// Сохраняем сообщение
	if err := boomex.StoreMessageForRelay(db, &msg); err != nil {
		return err
	}

	// Обновляем счётчик сообщений в эхе
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("echoes"))
		if b == nil {
			return fmt.Errorf("bucket echoes not found")
		}
		data := b.Get([]byte(areaName))
		if data == nil {
			return fmt.Errorf("echo area %s not found", areaName)
		}
		var area EchoArea
		if err := json.Unmarshal(data, &area); err != nil {
			return err
		}
		area.Messages++
		newData, _ := json.Marshal(area)
		return b.Put([]byte(areaName), newData)
	})
}

// AddBan добавляет запись о бане в эху
func AddBan(db *bolt.DB, echoName string, entry block.BlockEntry) error {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("bans"))
		if b == nil {
			return fmt.Errorf("bucket bans not found")
		}
		key := fmt.Sprintf("%s_%s", echoName, entry.Address)
		data, _ := json.Marshal(entry)
		return b.Put([]byte(key), data)
	})
}

// IsBannedInEcho проверяет, забанен ли адрес в конкретной эхе
func IsBannedInEcho(db *bolt.DB, echoName string, address string) bool {
	var entry block.BlockEntry
	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("bans"))
		if b == nil {
			return fmt.Errorf("bucket bans not found")
		}
		key := fmt.Sprintf("%s_%s", echoName, address)
		data := b.Get([]byte(key))
		if data == nil {
			return fmt.Errorf("not banned")
		}
		return json.Unmarshal(data, &entry)
	})
	return err == nil
}
