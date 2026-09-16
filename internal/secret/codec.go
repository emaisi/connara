package secret

import (
	"apihub-go/internal/jsonutil"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

type Codec struct {
	aead   cipher.AEAD
	keys   map[int]cipher.AEAD
	active int
}

type envelope struct {
	Version    int    `json:"version"`
	KeyVersion int    `json:"keyVersion,omitempty"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

func New(key []byte) (*Codec, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Codec{aead: aead, keys: map[int]cipher.AEAD{1: aead}, active: 1}, nil
}

func KeyFromBase64(value string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode APIHUB_ENCRYPTION_KEY: %w", err)
	}
	if len(key) != 32 {
		return nil, errors.New("APIHUB_ENCRYPTION_KEY must decode to exactly 32 bytes")
	}
	return key, nil
}

func (c *Codec) Encrypt(plaintext, aad []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ciphertext := c.aead.Seal(nil, nonce, plaintext, aad)
	return json.Marshal(envelope{
		Version:    1,
		KeyVersion: c.active,
		Nonce:      base64.RawStdEncoding.EncodeToString(nonce),
		Ciphertext: base64.RawStdEncoding.EncodeToString(ciphertext),
	})
}

func (c *Codec) Decrypt(data, aad []byte) ([]byte, error) {
	var value envelope
	if err := jsonutil.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("decode credential envelope: %w", err)
	}
	if value.Version != 1 {
		return nil, fmt.Errorf("unsupported credential envelope version %d", value.Version)
	}
	nonce, err := base64.RawStdEncoding.DecodeString(value.Nonce)
	if err != nil {
		return nil, errors.New("invalid credential nonce")
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(value.Ciphertext)
	if err != nil {
		return nil, errors.New("invalid credential ciphertext")
	}
	version := value.KeyVersion
	if version == 0 {
		version = 1
	}
	aead, ok := c.keys[version]
	if !ok {
		return nil, fmt.Errorf("encryption key version %d is unavailable", version)
	}
	if len(nonce) != aead.NonceSize() {
		return nil, errors.New("invalid credential nonce size")
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, errors.New("decrypt credential: authentication failed")
	}
	return plaintext, nil
}

func (c *Codec) Version() int16 { return int16(c.active) }

func NewKeyring(keys map[int][]byte, active int) (*Codec, error) {
	if active < 1 || active > 32767 {
		return nil, errors.New("invalid active key version")
	}
	ring := &Codec{keys: map[int]cipher.AEAD{}, active: active}
	for version, key := range keys {
		if version < 1 || version > 32767 {
			return nil, errors.New("invalid key version")
		}
		codec, err := New(key)
		if err != nil {
			return nil, err
		}
		ring.keys[version] = codec.aead
	}
	var ok bool
	ring.aead, ok = ring.keys[active]
	if !ok {
		return nil, errors.New("active encryption key is missing")
	}
	return ring, nil
}

func FromConfig(current, previous string, version int) (*Codec, error) {
	key, err := KeyFromBase64(current)
	if err != nil {
		return nil, err
	}
	encoded := map[int]string{}
	if previous != "" {
		if err := jsonutil.Unmarshal([]byte(previous), &encoded); err != nil {
			return nil, fmt.Errorf("invalid previous encryption keys: %w", err)
		}
	}
	keys := map[int][]byte{version: key}
	for v, value := range encoded {
		if v == version {
			return nil, errors.New("previous keys contains active version")
		}
		k, err := KeyFromBase64(value)
		if err != nil {
			return nil, err
		}
		keys[v] = k
	}
	return NewKeyring(keys, version)
}
