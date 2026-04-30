package storage

import "fmt"

type Store struct {
	path string
}

func Open(path string) (*Store, error) {
	fmt.Printf("Хранилище открыто: %s\n", path)
	return &Store{path: path}, nil
}

func (s *Store) Close() error {
	fmt.Println("Хранилище закрыто.")
	return nil
}
