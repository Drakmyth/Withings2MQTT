package crypto

import "testing"

func TestTokenCipherRoundTrip(t *testing.T) {
	cipher, err := NewTokenCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := cipher.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if string(encrypted) == "secret" {
		t.Fatal("token stored as plaintext")
	}
	plain, err := cipher.Decrypt(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != "secret" {
		t.Fatalf("got %q", plain)
	}
}
