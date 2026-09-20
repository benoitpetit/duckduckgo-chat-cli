package chat

import "testing"

func TestNewDurableStreamBuildsDuckAIJWK(t *testing.T) {
	stream, err := newDurableStream()
	if err != nil {
		t.Fatalf("newDurableStream() error = %v", err)
	}
	if stream.MessageID == "" || stream.ConversationID == "" {
		t.Fatal("durable stream IDs must not be empty")
	}
	if stream.PublicKey == nil {
		t.Fatal("durable stream public key is nil")
	}
	if stream.PublicKey.Alg != "RSA-OAEP-256" || stream.PublicKey.Kty != "RSA" {
		t.Fatalf("unexpected JWK type: alg=%q kty=%q", stream.PublicKey.Alg, stream.PublicKey.Kty)
	}
	if stream.PublicKey.E != "AQAB" || stream.PublicKey.N == "" {
		t.Fatalf("invalid RSA JWK exponent/modulus: e=%q n-empty=%t", stream.PublicKey.E, stream.PublicKey.N == "")
	}
}
