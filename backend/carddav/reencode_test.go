package carddav

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestReencodePhotoReturnsAJPEGOfTheSameImage(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 30, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 30; x++ {
			src.Set(x, y, color.RGBA{R: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}

	out, err := ReencodePhoto(buf.Bytes(), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("output is not a JPEG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 30 || b.Dy() != 20 {
		t.Errorf("size is %dx%d, want 30x20", b.Dx(), b.Dy())
	}
	if r, _, _, _ := img.At(15, 10).RGBA(); r>>8 < 180 {
		t.Errorf("centre lost its colour: red %d", r>>8)
	}
}

func TestReencodePhotoRefusesWhatIsNotAPhoto(t *testing.T) {
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	if _, err := ReencodePhoto(svg, "image/svg+xml"); err == nil {
		t.Error("an SVG was accepted")
	}
}
