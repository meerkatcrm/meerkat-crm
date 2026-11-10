package httputil

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"testing"
)

// pngHeader builds a PNG that declares the given size and carries no pixels.
func pngHeader(width, height uint32) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], width)
	binary.BigEndian.PutUint32(ihdr[4:], height)
	ihdr[8], ihdr[9] = 8, 6 // 8-bit RGBA
	chunk := append([]byte("IHDR"), ihdr...)
	out := []byte("\x89PNG\r\n\x1a\n")
	out = binary.BigEndian.AppendUint32(out, 13)
	out = append(out, chunk...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(chunk))
}

func TestCheckImageDimensions(t *testing.T) {
	var small bytes.Buffer
	if err := png.Encode(&small, image.NewRGBA(image.Rect(0, 0, 10, 10))); err != nil {
		t.Fatal(err)
	}
	if err := CheckImageDimensions(bytes.NewReader(small.Bytes())); err != nil {
		t.Errorf("small image rejected: %v", err)
	}
	if err := CheckImageDimensions(bytes.NewReader(pngHeader(50000, 50000))); err != ErrImageTooLarge {
		t.Errorf("50000x50000 header: got %v, want ErrImageTooLarge", err)
	}
	if err := CheckImageDimensions(bytes.NewReader(pngHeader(8000, 8000))); err != nil {
		t.Errorf("64 megapixels rejected: %v", err)
	}
	if err := CheckImageDimensions(bytes.NewReader([]byte("not an image"))); err != nil {
		t.Errorf("unreadable header must be left to the decoder, got %v", err)
	}
}
