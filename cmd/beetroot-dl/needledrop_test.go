package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"testing"
)

// encryptForTest produces a PKCS#7-padded, AES-CBC encrypted segment, i.e. a
// well-formed input for decryptSegment.
func encryptForTest(t *testing.T, plaintext []byte, key StreamKey) []byte {
	t.Helper()
	block, err := aes.NewCipher(key.Value)
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	padding := block.BlockSize() - len(plaintext)%block.BlockSize()
	padded := append(append([]byte(nil), plaintext...), bytes.Repeat([]byte{byte(padding)}, padding)...)
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, key.IV).CryptBlocks(out, padded)
	return out
}

func testKey() StreamKey {
	return StreamKey{
		Value: bytes.Repeat([]byte{0x0b}, 16),
		IV:    bytes.Repeat([]byte{0x0c}, 16),
	}
}

func TestDecryptSegmentRoundTrip(t *testing.T) {
	key := testKey()
	for _, plaintext := range [][]byte{
		[]byte("a"),
		[]byte("exactly sixteen!"), // a full block, so padding is a whole block
		bytes.Repeat([]byte("audio"), 100),
	} {
		got, err := decryptSegment(encryptForTest(t, plaintext, key), key)
		if err != nil {
			t.Fatalf("decrypt %q: %v", plaintext, err)
		}
		if !bytes.Equal(got, plaintext) {
			t.Errorf("round trip: got %q, want %q", got, plaintext)
		}
	}
}

// A malformed segment must produce an error, never a panic — the bytes come
// from the CDN and used to be trusted blindly.
func TestDecryptSegmentRejectsMalformedInput(t *testing.T) {
	key := testKey()

	cases := map[string][]byte{
		"empty":       {},
		"misaligned":  bytes.Repeat([]byte{0x01}, 17),
		"oversized padding": func() []byte {
			block, _ := aes.NewCipher(key.Value)
			// One block that decrypts to a trailing byte larger than the
			// segment length, which previously sliced out of bounds.
			out := make([]byte, 16)
			cipher.NewCBCEncrypter(block, key.IV).CryptBlocks(out, bytes.Repeat([]byte{0xff}, 16))
			return out
		}(),
	}

	for name, segment := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := decryptSegment(segment, key); err == nil {
				t.Fatal("expected an error, got nil")
			}
		})
	}
}

func TestDecryptSegmentRejectsBadIV(t *testing.T) {
	key := testKey()
	segment := encryptForTest(t, []byte("hello"), key)
	key.IV = []byte{0x01, 0x02}
	if _, err := decryptSegment(segment, key); err == nil {
		t.Fatal("expected an error for a short IV, got nil")
	}
}
