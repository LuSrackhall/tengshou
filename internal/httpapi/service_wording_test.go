package httpapi_test

import (
	"os"
	"strings"
	"testing"
)

// TestServicePageWording pins the LAN status page to shipped behavior. B5 has
// already delivered the handwritten signature page, so the status page must
// point at 「扫码签名」 on the ledger contract row instead of promising a
// future signature page; it also has to keep the B12 document-tool wording
// boundary. The status page was the one user-facing file missing from the
// domain wording tests, which is how the stale promise survived the B12 audit
// (M1).
func TestServicePageWording(t *testing.T) {
	const file = "web/index.html"
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	text := string(raw)

	// 等价过期承诺：原句是「手写签名页将在后续版本提供」，任何主语换成
	// 「将在后续版本提供」的写法都与已交付事实不符。
	stalePromises := []string{
		"手写签名页将在后续版本提供",
		"将在后续版本提供",
	}
	for _, phrase := range stalePromises {
		if strings.Contains(text, phrase) {
			t.Errorf("%s contains stale promise %q; the signature page has shipped, point at 扫码签名 instead", file, phrase)
		}
	}

	// B12 禁止措辞（含附加项）：文书工具不是签约平台，也不承诺外部平台对接、
	// 自动填报或代办填写。
	forbidden := []string{
		"在线签约", "直接签订", "电子签约", "电子签章", "具有法律效力的电子签名",
		"平台对接", "自动填报", "代填", "法定认证", "电子认证", "电子公章", "代签",
	}
	for _, phrase := range forbidden {
		if strings.Contains(text, phrase) {
			t.Errorf("%s contains forbidden wording %q", file, phrase)
		}
	}

	// 正向锚点：页面必须把用户指向台账页合同行的「扫码签名」，防止再次
	// 改写成「以后再说」。
	if !strings.Contains(text, "手写签名请在台账页的合同行点『扫码签名』") {
		t.Errorf("%s must point to 台账页合同行『扫码签名』", file)
	}
}
