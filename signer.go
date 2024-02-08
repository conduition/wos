package wos

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"io"
)

// Signer represents an HMAC-SHA256 signer which signs the given HTTP request
// details using the APISecret from the WoS [Credentials].
//
// For a simple instantiation of Signer, see [SimpleSigner].
type Signer interface {
	// SignRequest should return a SHA256 HMAC on the following string:
	//
	// 	endpoint + nonce + apiToken + requestBody
	//
	// SignRequest may also perform validation or introspection on the request
	// and decide whether to sign it.
	//
	// If the Signer does not want to sign the request, it should return an error
	// which will be wrapped and bubbled up to the higher-level [Wallet] method.
	SignRequest(ctx context.Context, endpoint, nonce, apiToken, requestBody string) ([]byte, error)
}

// SimpleSigner implements [Signer] with a static secret and no validation.
//
// Use [Credentials.SimpleSigner] to create a SimpleSigner from a set of credentials.
type SimpleSigner struct {
	apiSecret string
}

// NewSimpleSigner creates a SimpleSigner from a given APISecret
func NewSimpleSigner(apiSecret string) *SimpleSigner {
	return &SimpleSigner{apiSecret}
}

// SignRequest implements Signer.
func (s *SimpleSigner) SignRequest(
	ctx context.Context,
	endpoint, nonce, apiToken, requestBody string,
) ([]byte, error) {
	hasher := hmac.New(sha256.New, []byte(s.apiSecret))
	io.WriteString(hasher, endpoint)
	io.WriteString(hasher, nonce)
	io.WriteString(hasher, apiToken)
	io.WriteString(hasher, requestBody)
	return hasher.Sum(nil), nil
}
