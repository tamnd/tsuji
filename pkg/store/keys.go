package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

// Key is one inference API key. Only the sha256 of the secret is stored;
// the plaintext exists once, at creation.
type Key struct {
	ID        int64
	Name      string
	Label     string
	Hash      string
	Disabled  bool
	CreatedAt time.Time
}

// ErrKeyNotFound is returned when no enabled key matches.
var ErrKeyNotFound = errors.New("key not found")

// CreateKey mints a new sk-tsuji-v1 key and returns the plaintext once.
func (s *Store) CreateKey(name string) (plaintext string, k *Key, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	plaintext = "sk-tsuji-v1-" + hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(plaintext))
	hash := hex.EncodeToString(sum[:])
	label := plaintext[:16] + "..." + plaintext[len(plaintext)-4:]

	res, err := s.db.Exec(
		`INSERT INTO keys (name, label, hash, disabled, created_at) VALUES (?, ?, ?, 0, ?)`,
		name, label, hash, time.Now().UTC().Unix(),
	)
	if err != nil {
		return "", nil, err
	}
	id, _ := res.LastInsertId()
	return plaintext, &Key{ID: id, Name: name, Label: label, Hash: hash}, nil
}

// KeyBySecret resolves a bearer secret to an enabled key.
func (s *Store) KeyBySecret(secret string) (*Key, error) {
	sum := sha256.Sum256([]byte(secret))
	hash := hex.EncodeToString(sum[:])
	var k Key
	var created int64
	var disabled int
	err := s.db.QueryRow(
		`SELECT id, name, label, hash, disabled, created_at FROM keys WHERE hash = ?`, hash,
	).Scan(&k.ID, &k.Name, &k.Label, &k.Hash, &disabled, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrKeyNotFound
	}
	if err != nil {
		return nil, err
	}
	if disabled != 0 {
		return nil, ErrKeyNotFound
	}
	k.Disabled = disabled != 0
	k.CreatedAt = time.Unix(created, 0).UTC()
	return &k, nil
}
