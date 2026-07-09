package identity

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/datallboy/gonzb/internal/gonzbnet/canonical"
)

const (
	DefaultKeyFileName = "node_ed25519_private.key"
	NodeIDPrefix       = "node"
)

var base32NoPadding = base32.StdEncoding.WithPadding(base32.NoPadding)

type Identity struct {
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	nodeID     string
}

func LoadOrCreate(keysDir string) (*Identity, error) {
	keysDir = strings.TrimSpace(keysDir)
	if keysDir == "" {
		return nil, fmt.Errorf("gonzbnet keys dir is required")
	}
	if err := os.MkdirAll(keysDir, 0700); err != nil {
		return nil, fmt.Errorf("create gonzbnet keys dir: %w", err)
	}

	keyPath := filepath.Join(keysDir, DefaultKeyFileName)
	if raw, err := os.ReadFile(keyPath); err == nil {
		return fromEncodedPrivateKey(strings.TrimSpace(string(raw)))
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read node identity key: %w", err)
	}

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate node identity key: %w", err)
	}
	encoded := canonical.Base64URL(privateKey)
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
