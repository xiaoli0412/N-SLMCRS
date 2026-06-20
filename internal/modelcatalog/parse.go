package modelcatalog

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	// paramToken 匹配「数字+单位」：8b / 70b / 6.7b / 436m / 550b（大小写均可）。
	paramToken = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*([bBmM])`)
	// moeToken 匹配 MoE 专家×参数：8x22b / 8X7B。
	moeToken = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*[xX]\s*(\d+(?:\.\d+)?)\s*([bBmM])`)
	// activeToken 匹配 MoE 激活参数：a55b / a3b / a12b。
	activeToken = regexp.MustCompile(`-a(\d+(?:\.\d+)?)\s*([bBmM])`)
)

// unit 规范化单位为全大写 B/M。
func unit(u string) string { return strings.ToUpper(u) }

// largestParam 在所有 paramToken 命中里取数值最大者。
func largestParam(s string) (val float64, u string, ok bool) {
	for _, m := range paramToken.FindAllStringSubmatch(s, -1) {
		n, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			continue
		}
		if !ok || n > val {
			val, u, ok = n, m[2], true
		}
	}
	return
}

// trimZero 去掉浮点串末尾多余的 .0（8.0 → 8，6.7 → 6.7）。
func trimZero(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// ParseParamCount 从模型 ID 中解析出可读的参数量。
//
// 示例：
//
//	meta/llama-3.1-8b-instruct        -> "8B"
//	google/gemma-2-2b-it              -> "2B"
//	mistralai/mixtral-8x22b-v0.1      -> "8×22B"
//	nvidia/nemotron-3-ultra-550b-a55b -> "550B(A55B)"
//	nvidia/nv-embedqa-e5-v5           -> "436M"
//	deepseek-ai/deepseek-coder-6.7b   -> "6.7B"
//
// 无法识别时返回空串。
func ParseParamCount(id string) string {
	s := strings.ToLower(id)

	// 1. MoE 专家×参数优先：8x22b → "8×22B"
	if m := moeToken.FindStringSubmatch(s); m != nil {
		return m[1] + "×" + m[2] + unit(m[3])
	}

	// 2. 总参数（取最大值）
	val, u, ok := largestParam(s)
	if !ok {
		return ""
	}

	// 3. 激活参数：a55b → 追加 "(A55B)"
	out := trimZero(val) + unit(u)
	if am := activeToken.FindStringSubmatch(s); am != nil {
		an, _ := strconv.ParseFloat(am[1], 64)
		activeStr := trimZero(an) + unit(am[2])
		if activeStr != out {
			out += "(" + activeStr + ")"
		}
	}
	return out
}
