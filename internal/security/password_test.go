package security

import "testing"

func TestHashPasswordVerifies(t *testing.T) {
	salt, hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if salt == "" || hash == "" || salt == hash {
		t.Fatalf("HashPassword() returned invalid verifier data: salt=%q hash=%q", salt, hash)
	}
	if !VerifyPassword("correct horse battery staple", salt, hash) {
		t.Fatal("VerifyPassword() rejected the password used to create the hash")
	}
}

func TestVerifyPasswordRejectsWrongPassword(t *testing.T) {
	salt, hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if VerifyPassword("incorrect password", salt, hash) {
		t.Fatal("VerifyPassword() accepted an incorrect password")
	}
}

func TestVerifyPasswordRejectsMalformedEncoding(t *testing.T) {
	if VerifyPassword("password", "not-hex", "not-hex") {
		t.Fatal("VerifyPassword() accepted malformed verifier encoding")
	}
}
