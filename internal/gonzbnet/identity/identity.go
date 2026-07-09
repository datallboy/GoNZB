package identity

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/datallboy/gonzb/internal/gonzbnet/canonical"
	"golang.org/x/crypto/pbkdf2"
)

const (
	DefaultKeyFileName       = "node_ed25519_private.key"
	NodeIDPrefix             = "node"
	encryptedKeyEnvelopeV1   = "gonzbnet.ed25519.private.v1"
	encryptedKeyKDF          = "pbkdf2-sha256"
	encryptedKeyCipher       = "aes-256-gcm"
	encryptedKeyKDFIter      = 600000
	encryptedKeySaltSize     = 16
	encryptedKeyDerivedBytes = 32
)

var base32NoPadding = base32.StdEncoding.WithPadding(base32.NoPadding)

type Identity struct {
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	nodeID     string
}

func LoadOrCreate(keysDir string) (*Identity, error) {
	return LoadOrCreateWithPassword(keysDir, "")
}

func LoadOrCreateWithPassword(keysDir, password string) (*Identity, error) {
	keysDir = strings.TrimSpace(keysDir)
	if keysDir == "" {
		return nil, fmt.Errorf("gonzbnet keys dir is required")
	}
	if err := os.MkdirAll(keysDir, 0700); err != nil {
		return nil, fmt.Errorf("create gonzbnet keys dir: %w", err)
	}

	keyPath := filepath.Join(keysDir, DefaultKeyFileName)
	if raw, err := os.ReadFile(keyPath); err == nil {
		return identityFromStoredKey(keyPath, strings.TrimSpace(string(raw)), password)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read node identity key: %w", err)
	}

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate node identity key: %w", err)
	}
	encoded, err := encodeStoredPrivateKey(privateKey, password)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, []byte(encoded+"\n"), 0600); err != nil {
		return nil, fmt.Errorf("write node identity key: %w", err)
	}
	return FromPrivateKey(privateKey)
}

func FromPrivateKey(privateKey ed25519.PrivateKey) (*Identity, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("ed25519 private key must be %d bytes", ed25519.PrivateKeySize)
	}
	publicKey, ok := privateKey.Public().(ed25519.PublicKey)
	if !ok || len(publicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("derive ed25519 public key")
	}
	return &Identity{
		privateKey: append(ed25519.PrivateKey(nil), privateKey...),
		publicKey:  append(ed25519.PublicKey(nil), publicKey...),
		nodeID:     NodeIDFromPublicKey(publicKey),
	}, nil
}

func NodeIDFromPublicKey(publicKey ed25519.PublicKey) string {
	sum := sha256.Sum256(publicKey)
	return NodeIDPrefix + "_" + strings.ToLower(base32NoPadding.EncodeToString(sum[:]))
}

func (i *Identity) NodeID(context.Context) (string, error) {
	if i == nil || i.nodeID == "" {
		return "", fmt.Errorf("node identity is not initialized")
	}
	return i.nodeID, nil
}

func (i *Identity) PublicKey(context.Context) (ed25519.PublicKey, error) {
	if i == nil || len(i.publicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("node identity is not initialized")
	}
	return append(ed25519.PublicKey(nil), i.publicKey...), nil
}

func (i *Identity) PublicKeyBase64URL(ctx context.Context) (string, error) {
	publicKey, err := i.PublicKey(ctx)
	if err != nil {
		return "", err
	}
	return canonical.Base64URL(publicKey), nil
}

func (i *Identity) Sign(_ context.Context, payload []byte) ([]byte, error) {
	if i == nil || len(i.privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("node identity is not initialized")
	}
	return ed25519.Sign(i.privateKey, payload), nil
}

func (i *Identity) ExportEncryptedPrivateKey(backupPassword string) (string, error) {
	if i == nil || len(i.privateKey) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("node identity is not initialized")
	}
	if strings.TrimSpace(backupPassword) == "" {
		return "", fmt.Errorf("backup password is required")
	}
	return encryptPrivateKey(i.privateKey, backupPassword)
}

func Verify(publicKey ed25519.PublicKey, payload, signature []byte) bool {
	if len(publicKey) != ed25519.PublicKeySize || len(signature) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(publicKey, payload, signature)
}

func fromEncodedPrivateKey(encoded string) (*Identity, error) {
	raw, err := canonical.DecodeBase64URL(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode node identity key: %w", err)
	}
	return FromPrivateKey(ed25519.PrivateKey(raw))
}

type encryptedKeyEnvelope struct {
	Version    string `json:"version"`
	KDF        string `json:"kdf"`
	Cipher     string `json:"cipher"`
	Iterations int    `json:"iterations"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

func identityFromStoredKey(keyPath, encoded, password string) (*Identity, error) {
	if strings.HasPrefix(strings.TrimSpace(encoded), "{") {
		return fromEncryptedPrivateKey(encoded, password)
	}
	identity, err := fromEncodedPrivateKey(encoded)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(password) == "" {
		return identity, nil
	}
	encrypted, err := encodeStoredPrivateKey(identity.privateKey, password)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, []byte(encrypted+"\n"), 0600); err != nil {
		return nil, fmt.Errorf("encrypt existing node identity key: %w", err)
	}
	return identity, nil
}

func encodeStoredPrivateKey(privateKey ed25519.PrivateKey, password string) (string, error) {
	if strings.TrimSpace(password) == "" {
		return canonical.Base64URL(privateKey), nil
	}
	return encryptPrivateKey(privateKey, password)
}

func encryptPrivateKey(privateKey ed25519.PrivateKey, password string) (string, error) {
	salt := make([]byte, encryptedKeySaltSize)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate node key salt: %w", err)
	}
	key := deriveKey(password, salt, encryptedKeyKDFIter)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create node key cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create node key cipher mode: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate node key nonce: %w", err)
	}
	envelope := encryptedKeyEnvelope{
		Version:    encryptedKeyEnvelopeV1,
		KDF:        encryptedKeyKDF,
		Cipher:     encryptedKeyCipher,
		Iterations: encryptedKeyKDFIter,
		Salt:       canonical.Base64URL(salt),
		Nonce:      canonical.Base64URL(nonce),
		Ciphertext: canonical.Base64URL(gcm.Seal(nil, nonce, privateKey, nil)),
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return "", fmt.Errorf("marshal encrypted node identity key: %w", err)
	}
	return string(payload), nil
}

func fromEncryptedPrivateKey(encoded, password string) (*Identity, error) {
	if strings.TrimSpace(password) == "" {
		return nil, fmt.Errorf("gonzbnet key password is required for encrypted node identity key")
	}
	var envelope encryptedKeyEnvelope
	if err := json.Unmarshal([]byte(encoded), &envelope); err != nil {
		return nil, fmt.Errorf("decode encrypted node identity key: %w", err)
	}
	if envelope.Version != encryptedKeyEnvelopeV1 || envelope.KDF != encryptedKeyKDF || envelope.Cipher != encryptedKeyCipher {
		return nil, fmt.Errorf("unsupported encrypted node identity key format")
	}
	if envelope.Iterations <= 0 {
		return nil, fmt.Errorf("encrypted node identity key iterations are invalid")
	}
	salt, err := canonical.DecodeBase64URL(envelope.Salt)
	if err != nil {
		return nil, fmt.Errorf("decode encrypted node identity key salt: %w", err)
	}
	nonce, err := canonical.DecodeBase64URL(envelope.Nonce)
	if err != nil {
		return nil, fmt.Errorf("decode encrypted node identity key nonce: %w", err)
	}
	ciphertext, err := canonical.DecodeBase64URL(envelope.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decode encrypted node identity key ciphertext: %w", err)
	}
	key := deriveKey(password, salt, envelope.Iterations)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create node key cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create node key cipher mode: %w", err)
	}
	privateKey, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt node identity key: %w", err)
	}
	return FromPrivateKey(ed25519.PrivateKey(privateKey))
}

func deriveKey(password string, salt []byte, iterations int) []byte {
	return pbkdf2.Key([]byte(password), salt, iterations, encryptedKeyDerivedBytes, sha256.New)
}
