package connections

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

const encryptEnvVar = "DOLLY_CONNECTIONS_KEY"

type cipherEnvelope struct {
	Version    int    `yaml:"version"`
	Cipher     string `yaml:"cipher"`
	Nonce      string `yaml:"nonce"`
	Ciphertext string `yaml:"ciphertext"`
}

func loadEncryptionKey() ([]byte, error) {
	raw := os.Getenv(encryptEnvVar)
	if raw == "" {
		return nil, ErrEncryptKey
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("%w: %s must be standard base64 encoding 32 bytes", ErrEncryptKey, encryptEnvVar)
	}
	return key, nil
}

func sealPlaintext(plaintext []byte) ([]byte, error) {
	key, err := loadEncryptionKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	env := cipherEnvelope{
		Version:    1,
		Cipher:     "aes-256-gcm",
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}
	return yaml.Marshal(env)
}

func openCiphertext(data []byte) ([]byte, error) {
	var env cipherEnvelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("parse cipher envelope: %w", err)
	}
	if env.Version != 1 || env.Cipher != "aes-256-gcm" {
		return nil, fmt.Errorf("unsupported connections store cipher envelope")
	}
	key, err := loadEncryptionKey()
	if err != nil {
		return nil, err
	}
	nonce, err := base64.StdEncoding.DecodeString(env.Nonce)
	if err != nil {
		return nil, fmt.Errorf("decode nonce: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decode ciphertext: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt connections store: %w", err)
	}
	return plaintext, nil
}

func isCipherEnvelope(data []byte) bool {
	var env cipherEnvelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		return false
	}
	return env.Version == 1 && env.Cipher == "aes-256-gcm" && env.Ciphertext != ""
}
