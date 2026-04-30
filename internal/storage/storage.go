package storage

import (
	"fmt"
	"os"
	"path/filepath"

	bolt "go.etcd.io/bbolt"
)

type Store struct {
	db   *bolt.DB
	path string
}

var (
	BucketPeers    = []byte("peers")
	BucketMessages = []byte("messages")
	BucketOutbox   = []byte("outbox")
)

// Open открывает или создаёт базу данных.
// readOnly = true открывает БД только для чтения (не блокирует писателя).
func Open(dataDir string, readOnly bool) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("cannot create data dir: %w", err)
	}

	dbPath := filepath.Join(dataDir, "boomnode.db")

	var opts *bolt.Options
	if readOnly {
		opts = &bolt.Options{ReadOnly: true}
	}

	db, err := bolt.Open(dbPath, 0600, opts)
	if err != nil {
		return nil, fmt.Errorf("cannot open database: %w", err)
	}

	// Если не read-only, создаём buckets (иначе не нужно)
	if !readOnly {
		err = db.Update(func(tx *bolt.Tx) error {
			for _, bucket := range [][]byte{BucketPeers, BucketMessages, BucketOutbox} {
				if _, err := tx.CreateBucketIfNotExists(bucket); err != nil {
					return fmt.Errorf("cannot create bucket %s: %w", bucket, err)
				}
			}
			return nil
		})
		if err != nil {
			db.Close()
			return nil, err
		}
	}

	fmt.Printf("Storage opened: %s (readOnly=%v)\n", dbPath, readOnly)
	return &Store{db: db, path: dbPath}, nil
}

func (s *Store) Close() error {
	fmt.Println("Storage closed.")
	return s.db.Close()
}

func (s *Store) DB() *bolt.DB {
	return s.db
}
