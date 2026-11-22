// Package seal validates and normalizes imported official-seal pictures.
//
// A seal is printed as a picture placed by a contract template; this package
// only prepares the image so it renders cleanly on paper. PNG files keep their
// transparency. Opaque PNG and JPG files (for example phone photos of a red
// stamp on white paper) get a transparent background by keying out near-white
// pixels, which also makes the stamp sit naturally over the PDF page.
package seal

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	"image/png"
)

// Image limits bound the work an imported file can trigger.
const (
	maxPixels  = 20_000_000
	minSide    = 32
	minContent = 64  // inked pixels required to accept the picture
	inkCutoff  = 8   // below this alpha the pixel is background
	opaqueA    = 250 // source at or above this alpha counts as opaque
	whiteCut   = 246 // luminance at or above this becomes transparent
	whiteSoft  = 224 // luminance at or below this stays fully opaque
)

// ImageError is a user-facing rejection of an imported picture.
type ImageError struct {
	Code    string
	Message string
}

func (e *ImageError) Error() string { return e.Message }

func imageErr(code, format string, args ...any) *ImageError {
	return &ImageError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// NormalizedImage is a print-ready seal picture: PNG, transparent background
// where possible, trimmed to the stamp with a small margin.
type NormalizedImage struct {
	PNG          []byte
	SHA256       string
	Width        int
	Height       int
	SourceFormat string
	// MadeTransparent reports whether an opaque source had its white
	// background keyed out; the UI uses it to explain the preview.
	MadeTransparent bool
}

// Normalize decodes a PNG or JPG seal picture, validates its size and content,
// trims empty margins and re-encodes a PNG asset.
func Normalize(data []byte) (NormalizedImage, error) {
	if len(data) == 0 {
		return NormalizedImage{}, imageErr("invalid_image", "公章图片为空")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return NormalizedImage{}, imageErr("invalid_image", "公章图片无法识别，请导入 PNG 或 JPG 文件")
	}
	if format != "png" && format != "jpeg" {
		return NormalizedImage{}, imageErr("invalid_image", "公章图片只支持 PNG 或 JPG 格式")
	}
	if cfg.Width < minSide || cfg.Height < minSide {
		return NormalizedImage{}, imageErr("too_small", "公章图片太小，请使用更清晰的图片")
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return NormalizedImage{}, imageErr("too_large", "公章图片像素过大，请压缩后再导入")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return NormalizedImage{}, imageErr("invalid_image", "公章图片无法解码，请重新导出")
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	nrgba := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(nrgba, nrgba.Bounds(), img, bounds.Min, draw.Src)

	// A source that already carries transparency keeps its pixels untouched.
	hasAlpha := false
	for i := 3; i < len(nrgba.Pix); i += 4 {
		if nrgba.Pix[i] < opaqueA {
			hasAlpha = true
			break
		}
	}
	alpha := make([]uint8, w*h)
	minX, minY, maxX, maxY := w, h, -1, -1
	content := 0
	pix := nrgba.Pix
	for y := 0; y < h; y++ {
		row := y * nrgba.Stride
		for x := 0; x < w; x++ {
			i := row + x*4
			a := int(pix[i+3])
			if a == 0 {
				continue
			}
			if !hasAlpha {
				lum := (299*int(pix[i]) + 587*int(pix[i+1]) + 114*int(pix[i+2])) / 1000
				switch {
				case lum >= whiteCut:
					a = 0
				case lum > whiteSoft:
					// Fade the anti-aliased edge between white paper and ink.
					a = a * (whiteCut - lum) / (whiteCut - whiteSoft)
				}
			}
			if a < inkCutoff {
				continue
			}
			alpha[y*w+x] = uint8(a)
			content++
			if x < minX {
				minX = x
			}
			if x > maxX {
				maxX = x
			}
			if y < minY {
				minY = y
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	if content < minContent || maxX < minX || maxY < minY {
		return NormalizedImage{}, imageErr("too_empty", "图片中看不出公章内容，请换一张清楚的图片")
	}
	boxW, boxH := maxX-minX+1, maxY-minY+1
	if boxW < minSide/2 || boxH < minSide/2 {
		return NormalizedImage{}, imageErr("too_small", "公章图案太小，请使用更清晰的图片")
	}

	// Trim to the stamp with a margin proportional to its size.
	pad := boxW
	if boxH < pad {
		pad = boxH
	}
	pad /= 50
	if pad < 4 {
		pad = 4
	}
	if pad > 24 {
		pad = 24
	}
	x0, y0 := minX-pad, minY-pad
	x1, y1 := maxX+pad, maxY+pad
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 >= w {
		x1 = w - 1
	}
	if y1 >= h {
		y1 = h - 1
	}
	outW, outH := x1-x0+1, y1-y0+1
	out := image.NewNRGBA(image.Rect(0, 0, outW, outH))
	for y := 0; y < outH; y++ {
		src := (y0+y)*w + x0
		dst := y * out.Stride
		for x := 0; x < outW; x++ {
			a := alpha[src+x]
			if a == 0 {
				continue
			}
			i := dst + x*4
			j := (y0+y)*nrgba.Stride + (x0+x)*4
			out.Pix[i] = pix[j]
			out.Pix[i+1] = pix[j+1]
			out.Pix[i+2] = pix[j+2]
			out.Pix[i+3] = a
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return NormalizedImage{}, fmt.Errorf("seal: encode normalized png: %w", err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return NormalizedImage{
		PNG:             buf.Bytes(),
		SHA256:          hex.EncodeToString(sum[:]),
		Width:           outW,
		Height:          outH,
		SourceFormat:    format,
		MadeTransparent: !hasAlpha,
	}, nil
}
