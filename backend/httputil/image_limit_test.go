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

// webpHeader builds an extended-format (VP8X) WebP header declaring the given
// canvas size, with no image data.
func webpHeader(width, height uint32) []byte {
	chunk := []byte{0, 0, 0, 0} // flags + reserved
	for _, v := range []uint32{width - 1, height - 1} {
		chunk = append(chunk, byte(v), byte(v>>8), byte(v>>16))
	}
	body := append([]byte("WEBPVP8X"), binary.LittleEndian.AppendUint32(nil, uint32(len(chunk)))...)
	body = append(body, chunk...)
	out := append([]byte("RIFF"), binary.LittleEndian.AppendUint32(nil, uint32(len(body)))...)
	return append(out, body...)
}

func TestCheckImageDimensions_WebP(t *testing.T) {
	if err := CheckImageDimensions(bytes.NewReader(webpHeader(20000, 20000))); err != ErrImageTooLarge {
		t.Errorf("20000x20000 WebP header: got %v, want ErrImageTooLarge", err)
	}
	if err := CheckImageDimensions(bytes.NewReader(webpHeader(4000, 3000))); err != nil {
		t.Errorf("12 megapixel WebP rejected: %v", err)
	}
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
