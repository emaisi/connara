package secret

import (
	"bytes"
	"testing"
)

func TestCodecRoundTripAndAAD(t *testing.T) {
	codec, err := New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := codec.Encrypt([]byte(`{"apiKey":"secret"}`), []byte("env:connection"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := codec.Decrypt(encrypted, []byte("env:connection"))
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != `{"apiKey":"secret"}` {
		t.Fatalf("unexpected plaintext: %s", plain)
	}
	if _, err := codec.Decrypt(encrypted, []byte("another:connection")); err == nil {
		t.Fatal("credential encrypted for one connection must not decrypt for another")
	}
}
