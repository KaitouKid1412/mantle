package editor

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif" // decoder registration
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
)

// Image is an image attachment.
type Image struct {
	// Path is the source file for dropped images; empty for clipboard
	// images. Images created from a path are loaded lazily (LoadImageFile)
	// by the submit pipeline, off the UI goroutine.
	Path string
	// MediaType is "image/png", "image/jpeg", "image/gif" or "image/webp".
	MediaType string
	// Data is the encoded image, already downscaled to the limits.
	Data          []byte
	Width, Height int
}

// Loaded reports whether the image data is present.
func (im *Image) Loaded() bool { return im != nil && len(im.Data) > 0 }

// Base64 returns the data base64-encoded, as image content blocks carry it.
func (im *Image) Base64() string { return base64.StdEncoding.EncodeToString(im.Data) }

// Image limits. The API rejects images above about 5 MB (base64) and
// downsamples large dimensions anyway, so mantle shrinks them first.
const (
	MaxImageDim   = 2000
	MaxImageBytes = 3_750_000 // ≈ 5 MB once base64-encoded
	maxSourceSize = 64 << 20  // refuse to decode files larger than this
)

// ErrImageTooLarge is returned when an image cannot be brought under the
// limits (for example a huge WebP, which mantle cannot re-encode).
var ErrImageTooLarge = errors.New("image is too large")

// ErrNotImage is returned for data that is not a supported image.
var ErrNotImage = errors.New("not a supported image (png, jpeg, gif, webp)")

// LoadImageFile reads and prepares an image file.
func LoadImageFile(path string) (*Image, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.Size() > maxSourceSize {
		return nil, ErrImageTooLarge
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	im, err := PrepareImage(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	im.Path = path
	return im, nil
}

// PrepareImage detects the format and downscales or re-encodes the image
// when it exceeds MaxImageDim or MaxImageBytes.
func PrepareImage(data []byte) (*Image, error) {
	mt := http.DetectContentType(data)
	switch mt {
	case "image/png", "image/jpeg", "image/gif":
	case "image/webp":
		// No WebP codec in the standard library: pass it through when small.
		if len(data) > MaxImageBytes {
			return nil, ErrImageTooLarge
		}
		return &Image{MediaType: mt, Data: data}, nil
	default:
		return nil, ErrNotImage
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotImage, err)
	}
	if cfg.Width <= MaxImageDim && cfg.Height <= MaxImageDim && len(data) <= MaxImageBytes {
		return &Image{MediaType: mt, Data: data, Width: cfg.Width, Height: cfg.Height}, nil
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotImage, err)
	}
	return shrink(src)
}

// shrink fits src into MaxImageDim and MaxImageBytes: PNG first, then JPEG
// at falling quality and size.
func shrink(src image.Image) (*Image, error) {
	b := src.Bounds()
	w, h := fitWithin(b.Dx(), b.Dy(), MaxImageDim)
	for attempt := 0; attempt < 6; attempt++ {
		img := resize(src, w, h)
		var buf bytes.Buffer
		if attempt == 0 {
			if err := png.Encode(&buf, img); err == nil && buf.Len() <= MaxImageBytes {
				return &Image{MediaType: "image/png", Data: buf.Bytes(), Width: w, Height: h}, nil
			}
			buf.Reset()
		}
		q := 85 - attempt*10
		if err := jpeg.Encode(&buf, flatten(img), &jpeg.Options{Quality: q}); err != nil {
			return nil, err
		}
		if buf.Len() <= MaxImageBytes {
			return &Image{MediaType: "image/jpeg", Data: buf.Bytes(), Width: w, Height: h}, nil
		}
		w, h = w*3/4, h*3/4
		if w < 1 || h < 1 {
			break
		}
	}
	return nil, ErrImageTooLarge
}

func fitWithin(w, h, max int) (int, int) {
	if w <= max && h <= max {
		return w, h
	}
	if w >= h {
		return max, maxInt(1, h*max/w)
	}
	return maxInt(1, w*max/h), max
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// resize scales src to w×h with a box filter (area average). It is only
// used for shrinking.
func resize(src image.Image, w, h int) *image.RGBA {
	sb := src.Bounds()
	rgba, ok := src.(*image.RGBA)
	if !ok || sb.Min != (image.Point{}) {
		rgba = image.NewRGBA(image.Rect(0, 0, sb.Dx(), sb.Dy()))
		draw.Draw(rgba, rgba.Bounds(), src, sb.Min, draw.Src)
	}
	sw, sh := rgba.Bounds().Dx(), rgba.Bounds().Dy()
	if sw == w && sh == h {
		return rgba
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		y0, y1 := y*sh/h, (y+1)*sh/h
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < w; x++ {
			x0, x1 := x*sw/w, (x+1)*sw/w
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var r, g, b, a, n uint64
			for sy := y0; sy < y1; sy++ {
				off := sy*rgba.Stride + x0*4
				for sx := x0; sx < x1; sx++ {
					p := rgba.Pix[off : off+4 : off+4]
					r += uint64(p[0])
					g += uint64(p[1])
					b += uint64(p[2])
					a += uint64(p[3])
					n++
					off += 4
				}
			}
			d := dst.PixOffset(x, y)
			dst.Pix[d] = uint8(r / n)
			dst.Pix[d+1] = uint8(g / n)
			dst.Pix[d+2] = uint8(b / n)
			dst.Pix[d+3] = uint8(a / n)
		}
	}
	return dst
}

// flatten composites an image with alpha onto white for JPEG encoding.
func flatten(src *image.RGBA) image.Image {
	opaque := true
	for i := 3; i < len(src.Pix); i += 4 {
		if src.Pix[i] != 0xff {
			opaque = false
			break
		}
	}
	if opaque {
		return src
	}
	dst := image.NewRGBA(src.Bounds())
	draw.Draw(dst, dst.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Over)
	return dst
}
