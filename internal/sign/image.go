package sign

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/draw"
	"image/png"
)

// Image limits. The signing page draws on a 1200x480 logical canvas; these
// bounds keep malicious or accidental uploads cheap to process.
const (
	maxImagePixels = 12_000_000
	minImageWidth  = 120
	minImageHeight = 60
	minInkPixels   = 150
	minInkWidth    = 80
	minInkHeight   = 20
	minCoverage    = 0.01 // ink pixels / bounding-box pixels
	minMeanAlpha   = 32   // average opacity of inked pixels
	trimPadding    = 20   // transparent margin kept around the strokes
	alphaCutoff    = 8    // below this the pixel is background
)

// ImageError is a user-facing rejection of a submitted drawing.
type ImageError struct {
	Code    string
	Message string
}

func (e *ImageError) Error() string { return e.Message }

func imageErr(code, format string, args ...any) *ImageError {
	return &ImageError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// NormalizedImage is a print-ready signature: transparent background, black
// ink, trimmed to the strokes with a small margin.
type NormalizedImage struct {
	PNG      []byte
	SHA256   string
	Width    int
	Height   int
	InkRatio float64
}

// NormalizePNG decodes a submitted PNG, converts it to black ink on a
// transparent background (so phone screenshots with white backgrounds also
// work), trims empty margins, and rejects drawings that are too small or too
// light to be a signature.
func NormalizePNG(data []byte) (NormalizedImage, error) {
	if len(data) == 0 {
		return NormalizedImage{}, imageErr("invalid_image", "签名图片为空，请重新签名")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return NormalizedImage{}, imageErr("invalid_image", "签名图片无法识别，请重新签名")
	}
	if format != "png" {
		return NormalizedImage{}, imageErr("invalid_image", "签名图片必须是 PNG 格式")
	}
	if cfg.Width < minImageWidth || cfg.Height < minImageHeight {
		return NormalizedImage{}, imageErr("too_small", "签名图片尺寸过小，请重新签名")
	}
	if cfg.Width*cfg.Height > maxImagePixels {
		return NormalizedImage{}, imageErr("too_large", "签名图片过大，请重新签名")
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return NormalizedImage{}, imageErr("invalid_image", "签名图片无法解码，请重新签名")
	}
	bounds := img.Bounds()
	nrgba := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(nrgba, nrgba.Bounds(), img, bounds.Min, draw.Src)

	w, h := nrgba.Bounds().Dx(), nrgba.Bounds().Dy()
	// alpha holds the normalized ink alpha for every pixel. Near-white or
	// transparent pixels become background; anti-aliased edges stay soft.
	alpha := make([]uint8, w*h)
	minX, minY, maxX, maxY := w, h, -1, -1
	var inkPixels int
	var inkSum uint64
	pix := nrgba.Pix
	for y := 0; y < h; y++ {
		row := y * nrgba.Stride
		for x := 0; x < w; x++ {
			i := row + x*4
			r, g, b, a := pix[i], pix[i+1], pix[i+2], pix[i+3]
			if a == 0 {
				continue
			}
			lum := (299*int(r) + 587*int(g) + 114*int(b)) / 1000
			ink := int(a) * (255 - lum) / 255
			if ink < alphaCutoff {
				continue
			}
			alpha[y*w+x] = uint8(ink)
			inkPixels++
			inkSum += uint64(ink)
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
	if inkPixels < minInkPixels || maxX < minX || maxY < minY {
		return NormalizedImage{}, imageErr("too_light", "签名太淡或笔画太少，请把姓名写清楚后重新提交")
	}
	boxW, boxH := maxX-minX+1, maxY-minY+1
	if boxW < minInkWidth || boxH < minInkHeight {
		return NormalizedImage{}, imageErr("too_small", "签名太小，请在签名框内写得大一些后重新提交")
	}
	coverage := float64(inkPixels) / float64(boxW*boxH)
	meanAlpha := float64(inkSum) / float64(inkPixels)
	if coverage < minCoverage || meanAlpha < minMeanAlpha {
		return NormalizedImage{}, imageErr("too_light", "签名太淡，请换用更清晰的一笔后重新提交")
	}

	// Trim to the strokes plus a small transparent margin.
	x0, y0 := minX-trimPadding, minY-trimPadding
	x1, y1 := maxX+trimPadding, maxY+trimPadding
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
		for x := 0; x < outW; x++ {
			a := alpha[(y0+y)*w+(x0+x)]
			if a == 0 {
				continue
			}
			// Black ink, straight alpha. The PDF renderer honors alpha, so
			// the signature prints cleanly on paper.
			out.Pix[y*out.Stride+x*4+3] = a
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return NormalizedImage{}, fmt.Errorf("sign: encode normalized png: %w", err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return NormalizedImage{
		PNG:      buf.Bytes(),
		SHA256:   hex.EncodeToString(sum[:]),
		Width:    outW,
		Height:   outH,
		InkRatio: coverage,
	}, nil
}
