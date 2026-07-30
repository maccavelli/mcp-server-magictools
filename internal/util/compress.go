package util

import (
	"sync"

	"github.com/klauspost/compress/zstd"
)

var (
	encoder *zstd.Encoder
	decoder *zstd.Decoder
	encOnce sync.Once
	decOnce sync.Once
)

func getEncoder() (*zstd.Encoder, error) {
	var err error
	encOnce.Do(func() {
		encoder, err = zstd.NewWriter(nil)
	})
	return encoder, err
}

func getDecoder() (*zstd.Decoder, error) {
	var err error
	decOnce.Do(func() {
		decoder, err = zstd.NewReader(nil)
	})
	return decoder, err
}

// Compress compresses data using ZSTD for better performance/ratio than zlib
func Compress(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	enc, err := getEncoder()
	if err != nil {
		return nil, err
	}
	return enc.EncodeAll(data, make([]byte, 0, len(data)/2)), nil
}

// Decompress decompresses data using ZSTD
func Decompress(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	dec, err := getDecoder()
	if err != nil {
		return nil, err
	}
	return dec.DecodeAll(data, nil)
}
