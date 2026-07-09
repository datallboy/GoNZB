package requestauth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/canonical"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
)

const Scheme = "GoNZBNet"

type Signer interface {
	NodeID(context.Context) (string, error)
	Sign(context.Context, []byte) ([]byte, error)
}

type VerifierStore interface {
	GetFederationNodePublicKey(ctx context.Context, nodeID string) (ed25519.PublicKey, error)
	StoreFederationNonce(ctx context.Context, nodeID, nonce string, expiresAt time.Time) (bool, error)
}

type VerificationResult struct {
	NodeID string
	Nonce  string
}

func Sign(ctx context.Context, signer Signer, method, path, rawQuery string, body []byte, now time.Time) (string, error) {
	if signer == nil {
		return "", fmt.Errorf("signer is required")
	}
	nodeID, err := signer.NodeID(ctx)
	if err != nil {
		return "", err
	}
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", err
	}
	nonce := canonical.Base64URL(nonceBytes)
	timestamp := now.UTC().Format(time.RFC3339)
	payload, err := signingPayload(method, path, rawQuery, body, timestamp, nonce, nodeID)
	if err != nil {
		return "", err
	}
	signature, err := signer.Sign(ctx, payload)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`%s node_id="%s",timestamp="%s",nonce="%s",signature="%s"`,
		Scheme,
		nodeID,
		timestamp,
		nonce,
		canonical.Base64URL(signature),
	), nil
}

func Verify(ctx context.Context, store VerifierStore, authorization, method, path, rawQuery string, body []byte, now time.Time, tolerance, nonceTTL time.Duration) (VerificationResult, error) {
	var result VerificationResult
	values, err := ParseAuthorization(authorization)
	if err != nil {
		return result, err
	}
	nodeID := values["node_id"]
	timestampRaw := values["timestamp"]
	nonce := values["nonce"]
	signatureRaw := values["signature"]
	if nodeID == "" || timestampRaw == "" || nonce == "" || signatureRaw == "" {
		return result, fmt.Errorf("missing authorization fields")
	}
	timestamp, err := time.Parse(time.RFC3339, timestampRaw)
	if err != nil {
		return result, fmt.Errorf("invalid timestamp")
	}
	if tolerance <= 0 {
		tolerance = 120 * time.Second
	}
	if now.UTC().Sub(timestamp.UTC()) > tolerance || timestamp.UTC().Sub(now.UTC()) > tolerance {
		return result, fmt.Errorf("timestamp outside tolerance")
	}
	if _, err := canonical.DecodeBase64URL(nonce); err != nil {
		return result, fmt.Errorf("invalid nonce")
	}
	publicKey, err := store.GetFederationNodePublicKey(ctx, nodeID)
	if err != nil {
		return result, err
	}
	if identity.NodeIDFromPublicKey(publicKey) != nodeID {
		return result, fmt.Errorf("stored public key does not match node id")
	}
	payload, err := signingPayload(method, path, rawQuery, body, timestampRaw, nonce, nodeID)
	if err != nil {
		return result, err
	}
	signature, err := canonical.DecodeBase64URL(signatureRaw)
	if err != nil {
		return result, fmt.Errorf("invalid signature")
	}
	if !identity.Verify(publicKey, payload, signature) {
		return result, fmt.Errorf("request signature verification failed")
	}
	if nonceTTL <= 0 {
		nonceTTL = 10 * time.Minute
	}
	inserted, err := store.StoreFederationNonce(ctx, nodeID, nonce, now.UTC().Add(nonceTTL))
	if err != nil {
		return result, err
	}
	if !inserted {
		return result, fmt.Errorf("nonce replay")
	}
	result.NodeID = nodeID
	result.Nonce = nonce
	return result, nil
}

func ParseAuthorization(header string) (map[string]string, error) {
	header = strings.TrimSpace(header)
	if !strings.HasPrefix(header, Scheme+" ") {
		return nil, fmt.Errorf("missing gonzbnet authorization")
	}
	rest := strings.TrimSpace(strings.TrimPrefix(header, Scheme))
	values := make(map[string]string)
	for _, part := range strings.Split(rest, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			return nil, fmt.Errorf("invalid authorization field")
		}
		values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"`)
	}
	return values, nil
}

func signingPayload(method, path, rawQuery string, body []byte, timestamp, nonce, nodeID string) ([]byte, error) {
	return canonical.Marshal(map[string]any{
		"method":     strings.ToUpper(strings.TrimSpace(method)),
		"path":       path,
		"query_hash": hashRaw([]byte(rawQuery)),
		"body_hash":  hashRaw(body),
		"timestamp":  timestamp,
		"nonce":      nonce,
		"node_id":    nodeID,
	})
}

func hashRaw(payload []byte) string {
	sum := sha256.Sum256(payload)
	return canonical.HashPrefixSHA256 + base64.RawURLEncoding.EncodeToString(sum[:])
}

func HeaderFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	return r.Header.Get("Authorization")
}
