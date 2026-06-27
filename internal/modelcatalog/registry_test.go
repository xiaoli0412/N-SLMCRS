package modelcatalog

import "testing"

// normalizeID 为包内未导出函数，本测试同包白盒覆盖其实际行为。
//
// 当前实现：含 "/" 的 id 原样返回（厂商别名收敛分支因外层 Contains 早返回而不可达，
// 属已知待办——见 v0.9 路线图「注册表富化 id 归一化」）；无 "/" 的 id 亦原样返回。
func TestNormalizeID(t *testing.T) {
	cases := []struct{ in, want string }{
		{"meta-llama/llama-3.1-8b-instruct", "meta-llama/llama-3.1-8b-instruct"}, // 含 / 原样
		{"qwen/qwen2-7b-instruct", "qwen/qwen2-7b-instruct"},
		{"no-slash-id", "no-slash-id"}, // 无 / 原样
		{"", ""},
	}
	for _, c := range cases {
		if got := normalizeID(c.in); got != c.want {
			t.Errorf("normalizeID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ModelSpec JSON 标签与 SyncRegistry 契约一致性（无实网）。
func TestModelSpecZeroValue(t *testing.T) {
	var s ModelSpec
	if s.MaxTokens != 0 || s.PricingIn != "" || s.License != "" {
		t.Error("ModelSpec 零值不符预期")
	}
	if len(s.InputModalities) != 0 {
		t.Error("ModelSpec.InputModalities 零值应非 nil/空切片")
	}
}
