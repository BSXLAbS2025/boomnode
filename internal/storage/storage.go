package storage

import (
	"fmt"
	"os"
	"path/filepath"

	bolt "go.etcd.io/bbolt"
)

// Store — обёртка над BoltDB
type Store struct {
	db   *bolt.DB
	path string
}

// Buckets — названия таблиц (buckets) в BoltDB
var (
	BucketPeers    = []byte("peers")
	BucketMessages = []byte("messages")
	BucketOutbox   = []byte("outbox")
)

// Open открывает или создаёт базу данных
func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("не могу создать папку данных: %w", err)
	}

	dbPath := filepath.Join(dataDir, "boomnode.db")
	db, err := bolt.Open(dbPath, 0600, nil)
	if err != nil {
		return nil, fmt.Errorf("не могу открыть базу данных: %w", err)
	}

	// Создаём buckets, если их нет
	err = db.Update(func(tx *bolt.Tx) error {
		for _, bucket := range [][]byte{BucketPeers, BucketMessages, BucketOutbox} {
			if _, err := tx.CreateBucketIfNotExists(bucket); err != nil {
				return fmt.Errorf("не могу создать bucket %s: %w", bucket, err)
			}
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, err
	}

	fmt.Printf("Хранилище открыто: %s\n", dbPath)
	return &Store{db: db, path: dbPath}, nil
}

// Close закрывает базу данных
func (s *Store) Close() error {
	fmt.Println("Хранилище закрыто.")
	return s.db.Close()
}

// DB возвращает сырой доступ к BoltDB (для других пакетов)
func (s *Store) DB() *bolt.DB {
	return s.db
}
