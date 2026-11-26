package service

import (
	"math"
	"strconv"
	"strings"

	"github.com/LuSrackhall/tengshou/internal/model"
)

// 金额中文大写（te-7c5v1）。模板可绑定 contract.amount.words 与
// contract.data.<金额字段键>.words；合同表单可以为每个金额字段保存一个手填
// 覆盖值，覆盖值存放在 contract.data 的保留键里，不新增字段定义：
//
//	__amount_words__   合同内置金额（contract.amount）的大写覆盖
//	__words__<字段键>  动态金额字段的大写覆盖
//
// 保留键只影响正文渲染（service/templates textValue）与表单回显；Excel
// 导出/导入、MCP/API 数据结构都不含这些键。
const (
	amountWordsDataKey = "__amount_words__"
	wordsDataKeyPrefix = "__words__"
)

// wordsOverrideKey returns the reserved data key that stores the manual words
// override for one amount field (built-in amount or a dynamic money field).
func wordsOverrideKey(fieldKey string) string {
	if fieldKey == model.FieldAmount {
		return amountWordsDataKey
	}
	return wordsDataKeyPrefix + fieldKey
}

// moneyWordsFieldKey maps a reserved data key back to the amount field it
// overrides. The second result is false for every non-reserved data key.
func moneyWordsFieldKey(key string) (string, bool) {
	if key == amountWordsDataKey {
		return model.FieldAmount, true
	}
	if fieldKey, ok := strings.CutPrefix(key, wordsDataKeyPrefix); ok && fieldKey != "" {
		return fieldKey, true
	}
	return "", false
}

var moneyDigitWords = [...]string{"零", "壹", "贰", "叁", "肆", "伍", "陆", "柒", "捌", "玖"}
var moneyUnitWords = [...]string{"", "拾", "佰", "仟"}
var moneyGroupUnits = [...]string{"", "万", "亿", "万亿"}

// MoneyWords renders a non-negative amount in Chinese RMB uppercase, e.g.
// 1500 → 人民币壹仟伍佰元整 and 1500.55 → 人民币壹仟伍佰元伍角伍分.
//
// Zero, negative amounts and values too large to represent exactly return an
// empty string: a words binding for an empty amount renders nothing, and
// illegal input never produces misleading text. The algorithm walks the exact
// two-decimal string form (not float arithmetic) so 0.1+0.2 style noise can
// never leak into the output.
func MoneyWords(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return ""
	}
	rounded := math.Round(v*100) / 100
	if rounded == 0 {
		return ""
	}
	text := strconv.FormatFloat(rounded, 'f', 2, 64)
	parts := strings.SplitN(text, ".", 2)
	intPart, frac := parts[0], parts[1]
	// float64 only represents cents exactly below 2^53; longer integer parts
	// are rejected instead of printing digits the value never had.
	if len(intPart) > 16 {
		return ""
	}
	jiao := int(frac[0] - '0')
	fen := int(frac[1] - '0')

	var b strings.Builder
	b.WriteString("人民币")
	if intPart != "0" {
		b.WriteString(moneyIntWords(intPart))
		b.WriteString("元")
	}
	switch {
	case jiao == 0 && fen == 0:
		b.WriteString("整")
	default:
		if jiao > 0 {
			b.WriteString(moneyDigitWords[jiao])
			b.WriteString("角")
		} else if intPart != "0" {
			// 元与分之间有零：100.05 → 壹佰元零伍分.
			b.WriteString("零")
		}
		if fen > 0 {
			b.WriteString(moneyDigitWords[fen])
			b.WriteString("分")
		}
	}
	return b.String()
}

// moneyIntWords converts a decimal integer string (1–16 digits) to uppercase
// digits grouped by 万/亿, e.g. 10001 → 壹万零壹.
func moneyIntWords(digits string) string {
	groups := (len(digits) + 3) / 4
	padded := strings.Repeat("0", groups*4-len(digits)) + digits
	var b strings.Builder
	for i := 0; i < groups; i++ {
		group := padded[i*4 : (i+1)*4]
		if group == "0000" {
			continue
		}
		// A group below 1000 after a higher group needs a spoken zero:
		// 10001 → 壹万零壹, 100200300 → 壹亿零贰拾万零叁佰.
		if b.Len() > 0 && group[0] == '0' {
			b.WriteString("零")
		}
		b.WriteString(moneyGroupWords(group))
		b.WriteString(moneyGroupUnits[groups-1-i])
	}
	return b.String()
}

// moneyGroupWords converts one four-digit group (at least one non-zero digit).
func moneyGroupWords(group string) string {
	var b strings.Builder
	zero := false
	for i := 0; i < 4; i++ {
		digit := int(group[i] - '0')
		if digit == 0 {
			if b.Len() > 0 {
				zero = true
			}
			continue
		}
		if zero {
			b.WriteString("零")
			zero = false
		}
		b.WriteString(moneyDigitWords[digit])
		b.WriteString(moneyUnitWords[3-i])
	}
	return b.String()
}

// applyMoneyWordsInput splits the reserved words-override keys out of one
// contract input, validates them against the current field definitions, and
// applies non-blank values to data. A blank value is an explicit clear: the
// form always submits these keys for money fields, so an empty value restores
// the automatic conversion. Returns the remaining input map for
// applyDynamicFieldInput.
func applyMoneyWordsInput(fields []model.FieldDefinition, data, input map[string]any, v *ValidationErrors) map[string]any {
	if len(input) == 0 {
		return input
	}
	dynamic := make(map[string]model.FieldDefinition, len(fields))
	for _, field := range fields {
		if !field.BuiltIn {
			dynamic[field.Key] = field
		}
	}
	rest := make(map[string]any, len(input))
	for key, raw := range input {
		fieldKey, reserved := moneyWordsFieldKey(key)
		if !reserved {
			rest[key] = raw
			continue
		}
		if fieldKey != model.FieldAmount {
			field, exists := dynamic[fieldKey]
			if !exists {
				v.add(key, "金额大写覆盖", "对应的金额字段不存在")
				continue
			}
			if field.Type != model.FieldTypeMoney {
				v.add(key, field.Label+"（大写）", "只有金额字段可以设置大写覆盖")
				continue
			}
		}
		value := strings.TrimSpace(displayValue(raw))
		if value == "" {
			delete(data, key)
			continue
		}
		data[key] = truncateRunes(value, 200)
	}
	return rest
}

// wordsOverride reads the manual override stored under one reserved key.
func wordsOverride(data map[string]any, fieldKey string) string {
	if len(data) == 0 {
		return ""
	}
	return strings.TrimSpace(displayValue(data[wordsOverrideKey(fieldKey)]))
}
