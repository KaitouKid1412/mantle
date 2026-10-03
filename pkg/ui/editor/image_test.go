package editor

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func pngBytes(t *testing.T, w, h int, noisy bool) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	seed := uint32(1)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{uint8(x), uint8(y), 128, 255}
			if noisy {
				seed = seed*1664525 + 1013904223
				c = color.RGBA{uint8(seed >> 24), uint8(seed >> 16), uint8(seed >> 8), 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPrepareImagePassThrough(t *testing.T) {
	data := pngBytes(t, 64, 32, false)
	im, err := PrepareImage(data)
	if err != nil {
		t.Fatal(err)
	}
	if im.MediaType != "image/png" || !bytes.Equal(im.Data, data) || im.Width != 64 || im.Height != 32 {
		t.Fatalf("%+v", im)
	}
	if im.Base64() == "" {
		t.Fatal("base64")
	}
}

func TestPrepareImageDownscales(t *testing.T) {
	im, err := PrepareImage(pngBytes(t, 3000, 1000, false))
	if err != nil {
		t.Fatal(err)
	}
	if im.Width != MaxImageDim || im.Height != 666 {
		t.Fatalf("size %dx%d", im.Width, im.Height)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(im.Data))
	if err != nil || cfg.Width != im.Width {
		t.Fatal("encoded size mismatch", err)
	}
}

func TestPrepareImageCapsBytes(t *testing.T) {
	// Noise does not compress: the PNG exceeds the cap and becomes JPEG.
	im, err := PrepareImage(pngBytes(t, 1500, 1500, true))
	if err != nil {
		t.Fatal(err)
	}
	if len(im.Data) > MaxImageBytes || im.MediaType != "image/jpeg" {
		t.Fatalf("%s %d bytes", im.MediaType, len(im.Data))
	}
}

func TestPrepareImageRejectsNonImages(t *testing.T) {
	if _, err := PrepareImage([]byte("hello")); !errors.Is(err, ErrNotImage) {
		t.Fatal(err)
	}
}

func TestLoadImageFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.png")
	os.WriteFile(p, pngBytes(t, 10, 10, false), 0o600)
	im, err := LoadImageFile(p)
	if err != nil || im.Path != p || !im.Loaded() {
		t.Fatal(im, err)
	}
}
