package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	cryptoutil "github.com/xieyuqiyu-source/CLIProxyCloud/internal/crypto"
)

type Storage struct {
	root string
	key  []byte
}

func New(root string, key string) (*Storage, error) {
	if root == "" {
		return nil, fmt.Errorf("storage root is required")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &Storage{
		root: root,
		key:  cryptoutil.NormalizeKey(key),
	}, nil
}

func (s *Storage) Save(scope string, name string, content []byte) (string, string, error) {
	encrypted, err := cryptoutil.Encrypt(s.key, content)
	if err != nil {
		return "", "", err
	}

	hash := cryptoutil.SHA256Hex(content)
	dir := filepath.Join(s.root, scope, time.Now().Format("2006-01"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}

	fileName := fmt.Sprintf("%d-%s.bin", time.Now().UnixNano(), hash[:12])
	path := filepath.Join(dir, fileName)
	if err := os.WriteFile(path, encrypted, 0o600); err != nil {
		return "", "", err
	}

	return path, hash, nil
}

func (s *Storage) Read(path string) ([]byte, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return cryptoutil.Decrypt(s.key, content)
}

func (s *Storage) Delete(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
