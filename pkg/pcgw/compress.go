package pcgw

import (
	"github.com/klauspost/compress/zstd"
)

var zstdEncoder, _ = zstd.NewWriter(nil)
var zstdDecoder, _ = zstd.NewReader(nil)

// CompressWikitext compresses wikitext bytes with zstd.
func CompressWikitext(data []byte) ([]byte, error) {
	if zstdEncoder == nil {
		var err error
		zstdEncoder, err = zstd.NewWriter(nil)
		if err != nil {
			return nil, err
		}
	}
	return zstdEncoder.EncodeAll(data, make([]byte, 0, len(data)/2)), nil
}

// DecompressWikitext decompresses zstd-compressed wikitext bytes.
func DecompressWikitext(data []byte) ([]byte, error) {
	if zstdDecoder == nil {
		var err error
		zstdDecoder, err = zstd.NewReader(nil)
		if err != nil {
			return nil, err
		}
	}
	return zstdDecoder.DecodeAll(data, nil)
}
