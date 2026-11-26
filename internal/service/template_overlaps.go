package service

import (
	"fmt"
	"math"

	"github.com/LuSrackhall/tengshou/internal/model"
)

// overlapRatioThreshold is the non-blocking warning threshold for a seal or
// signature box covering a field box (30% of the smaller box). Saving is never
// blocked: the editor lists the pairs so the user can move a box and save
// again.
const overlapRatioThreshold = 0.30

// PlacementOverlap describes one seal/signature–field box pair whose overlap
// exceeds overlapRatioThreshold. A and B are placement ids so the editor can
// select and locate them in the placement list.
type PlacementOverlap struct {
	Page    int     `json:"page"`
	A       string  `json:"a"`
	B       string  `json:"b"`
	Ratio   float64 `json:"ratio"`
	Message string  `json:"message"`
}

// placementOverlaps reports seal/signature boxes that cover a field box on the
// same page beyond overlapRatioThreshold. The ratio is the intersection area
// divided by the smaller box area, so a small field under a large seal is still
// reported. Result order follows the placement list and is stable for the same
// mapping.
func placementOverlaps(list []model.Placement) []PlacementOverlap {
	out := []PlacementOverlap{}
	for _, picture := range list {
		if picture.Type != model.PlacementSeal && picture.Type != model.PlacementSignature {
			continue
		}
		for _, field := range list {
			if field.Type != model.PlacementField || field.Page != picture.Page {
				continue
			}
			ratio := boxOverlapRatio(picture, field)
			if ratio <= overlapRatioThreshold {
				continue
			}
			out = append(out, PlacementOverlap{
				Page:  picture.Page,
				A:     picture.ID,
				B:     field.ID,
				Ratio: math.Round(ratio*100) / 100,
				Message: fmt.Sprintf("位置 %s（%s）与位置 %s（字段 %s）重叠 %.0f%%，保存后文字可能压在图片上，请调整",
					picture.ID, placementKindLabel(picture.Type), field.ID, field.Field, ratio*100),
			})
		}
	}
	return out
}

// boxOverlapRatio returns the intersection area of two boxes on the same page
// divided by the smaller box area. Boxes on different pages never overlap.
func boxOverlapRatio(a, b model.Placement) float64 {
	if a.Page != b.Page {
		return 0
	}
	width := math.Min(a.X+a.W, b.X+b.W) - math.Max(a.X, b.X)
	height := math.Min(a.Y+a.H, b.Y+b.H) - math.Max(a.Y, b.Y)
	if width <= 0 || height <= 0 {
		return 0
	}
	smaller := math.Min(a.W*a.H, b.W*b.H)
	if smaller <= 0 {
		return 0
	}
	return (width * height) / smaller
}

// placementKindLabel names a placement type for user-visible messages.
func placementKindLabel(kind string) string {
	switch kind {
	case model.PlacementSeal:
		return "公章"
	case model.PlacementSignature:
		return "签名"
	default:
		return "字段"
	}
}
