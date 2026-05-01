package echo

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/BSXLAbS2025/boomnode/internal/boomex"
	bolt "go.etcd.io/bbolt"
)

// EchoArea — эхоконференция
type EchoArea struct {
	Name        string    `json:"name"`        // boombox.general
	Description string    `json:"description"`
	Moderator   string    `json:"moderator"`   // BM-адрес модератора
	Created     time.Time `json:"created"`
	Messages    int       `json:"messages"`    // счётчик сообщений
}

// Subscribe подписывает узел на эху
func Subscribe(db *bolt.DB, areaName string, description string) error {
	area := EchoArea{
		Name:        areaName,
		Description: description,
		Created:     time.Now(),
	}
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("echoes"))
		if b == nil {
			return fmt.Errorf("bucket echoes not found")
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

// PostToEcho отправляет сообщение в эху (всем подписчикам)
func PostToEcho(db *bolt.DB, areaName string, from string, subject string, body string) error {
	msg := boomex.Message{
		Type:      boomex.TypeMSG,
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		From:      from,
		To:        areaName, // в поле To — имя эхи
		Subject:   fmt.Sprintf("[%s] %s", areaName, subject),
		Body:      body,
		Timestamp: time.Now(),
	}
	return boomex.StoreMessageForRelay(db, &msg)
}
