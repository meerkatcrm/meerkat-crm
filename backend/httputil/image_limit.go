package httputil

import (
	"errors"
	"image"
	"io"

	// Register every format the server decodes anywhere (uploads, vCard photos),
	// so DecodeConfig can read each one's header here whatever the caller imports.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "github.com/gen2brain/heic"
	_ "golang.org/x/image/webp"
)

// MaxImagePixels caps the images the server will decode. A decoded image costs
// up to 4 bytes per pixel whatever the file size, so a few kilobytes of PNG
// that declare 50000x50000 would allocate 10 GB. 64 megapixels covers current
// phone cameras.
const MaxImagePixels = 64_000_000

// ErrImageTooLarge is returned by CheckImageDimensions.
var ErrImageTooLarge = errors.New("image dimensions are too large")

// CheckImageDimensions reads only the image header and rejects an image with
// more than MaxImagePixels. A header it cannot read is not an error here: every
// decoder the server uses is registered above, so the decode that follows cannot
// read it either and reports the failure.
func CheckImageDimensions(r io.Reader) error {
	cfg, _, err := image.DecodeConfig(r)
	if err != nil {
		return nil
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxImagePixels/cfg.Height {
		return ErrImageTooLarge
	}
	return nil
}
