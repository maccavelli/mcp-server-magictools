package util

import (
	"bytes"
	"testing"
)

func TestCompressionRoundTrip(t *testing.T) {
	input := []byte("hello, magictools! this is a repeatable string to ensure zlib works fine. hello, magictools!")

	// 1. Compress
	compressed, err := Compress(input)
	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}

	if len(compressed) == 0 {
		t.Fatal("compressed data is empty")
	}

	// 2. Decompress
	decompressed, err := Decompress(compressed)
	if err != nil {
		t.Fatalf("Decompress failed: %v", err)
	}

	if !bytes.Equal(input, decompressed) {
		t.Errorf("round-trip failed: got %q, want %q", string(decompressed), string(input))
	}
}

func TestDecompressInvalidData(t *testing.T) {
	invalid := []byte("not zlib data")
	_, err := Decompress(invalid)
	if err == nil {
		t.Error("expected error for invalid zlib data, got nil")
	}
}
