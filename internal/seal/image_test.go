package seal

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

func drawCircle(img draw.Image, cx, cy, r int, ink color.Color) {
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			if (x-cx)*(x-cx)+(y-cy)*(y-cy) > r*r || !image.Pt(x, y).In(img.Bounds()) {
				continue
			}
			img.Set(x, y, ink)
		}
	}
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestNormalizeKeepsTransparentPNG(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 300, 260))
	drawCircle(img, 150, 130, 70, color.NRGBA{190, 30, 30, 255})
	out, err := Normalize(encodePNG(t, img))
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if out.MadeTransparent {
		t.Fatal("transparent source should not be reported as white-keyed")
	}
	if out.Width >= 300 || out.Height >= 260 {
		t.Fatalf("expected trimmed image, got %dx%d", out.Width, out.Height)
	}
	if out.Width < 140 || out.Height < 140 {
		t.Fatalf("trimmed too aggressively: %dx%d", out.Width, out.Height)
	}
	if len(out.SHA256) != 64 {
		t.Fatalf("sha256 = %q", out.SHA256)
	}
	decoded, err := png.Decode(bytes.NewReader(out.PNG))
	if err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if r, g, b, a := decoded.At(0, 0).RGBA(); a != 0 && !(r == 0 && g == 0 && b == 0) {
		t.Fatalf("corner should be transparent, got rgba %d,%d,%d,%d", r, g, b, a)
	}
}

func TestNormalizeWhiteBackgroundToTransparent(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 240, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 240; x++ {
			img.Set(x, y, color.RGBA{255, 255, 255, 255})
		}
	}
	drawCircle(img, 120, 120, 60, color.RGBA{200, 40, 40, 255})
	out, err := Normalize(encodePNG(t, img))
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if !out.MadeTransparent {
		t.Fatal("opaque white background should be keyed out")
	}
	decoded, err := png.Decode(bytes.NewReader(out.PNG))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, a := decoded.At(1, 1).RGBA(); a != 0 {
		t.Fatalf("corner alpha = %d, want 0", a)
	}
	mid := decoded.Bounds().Dx() / 2
	if _, _, _, a := decoded.At(mid, decoded.Bounds().Dy()/2).RGBA(); a == 0 {
		t.Fatal("stamp center should stay opaque")
	}
}

func TestNormalizeAcceptsJPEG(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 220, 220))
	for y := 0; y < 220; y++ {
		for x := 0; x < 220; x++ {
			img.Set(x, y, color.RGBA{250, 250, 250, 255})
		}
	}
	drawCircle(img, 110, 110, 55, color.RGBA{190, 20, 20, 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	out, err := Normalize(buf.Bytes())
	if err != nil {
		t.Fatalf("normalize jpeg: %v", err)
	}
	if out.SourceFormat != "jpeg" {
		t.Fatalf("source format = %q", out.SourceFormat)
	}
	if out.Width < 100 || out.Height < 100 {
		t.Fatalf("unexpected size %dx%d", out.Width, out.Height)
	}
}

func TestNormalizeRejectsBadInput(t *testing.T) {
	if _, err := Normalize(nil); err == nil {
		t.Fatal("empty input should fail")
	}
	if _, err := Normalize([]byte("not an image")); err == nil {
		t.Fatal("text input should fail")
	}
	// GIF is not a supported seal format.
	gifImg := image.NewPaletted(image.Rect(0, 0, 64, 64), color.Palette{color.Black, color.White})
	var buf bytes.Buffer
	if err := gif.Encode(&buf, gifImg, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Normalize(buf.Bytes()); err == nil {
		t.Fatal("gif should be rejected")
	}
	small := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	drawCircle(small, 8, 8, 6, color.NRGBA{200, 0, 0, 255})
	if _, err := Normalize(encodePNG(t, small)); err == nil {
		t.Fatal("tiny image should be rejected")
	}
	blank := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			blank.SetNRGBA(x, y, color.NRGBA{255, 255, 255, 255})
		}
	}
	if _, err := Normalize(encodePNG(t, blank)); err == nil {
		t.Fatal("blank white image should be rejected")
	}
}
