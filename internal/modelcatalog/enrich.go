package modelcatalog

// EnrichedModel enrich 的返回：补齐后的模型富元数据。
// v0.7 携带扩展规格（max_tokens / 定价 / 许可证 / 模态 / 模型卡 URL 等）。
type EnrichedModel struct {
	Capability    string
	ParamCount    string
	ContextLength int
	Description   string
	// 扩展规格（来自 specCatalog 或远程注册表，留空前端显示"—"）
	MaxTokens       int
	PricingIn       string
	PricingOut      string
	License         string
	InputModalities []string
	ReleaseDate     string
	CardURL         string
}

// defaultDesc 按能力返回兜底描述（未策展模型）。
func defaultDesc(capability string) string {
	switch capability {
	case CapEmbedding:
		return "NVIDIA NIM 文本向量嵌入模型"
	case CapRerank:
		return "NVIDIA NIM 检索重排序模型"
	case CapVision:
		return "NVIDIA NIM 多模态视觉理解模型"
	case CapCode:
		return "NVIDIA NIM 代码生成 / 补全模型"
	case CapReasoning:
		return "NVIDIA NIM 深度推理模型"
	case CapSafety:
		return "NVIDIA NIM 内容安全 / 护栏模型"
	case CapReward:
		return "NVIDIA NIM 奖励 / 偏好对齐模型"
	case CapTranslation:
		return "NVIDIA Riva 翻译模型"
	case CapParsing:
		return "NVIDIA NIM 文档解析模型"
	default:
		return "NVIDIA NIM 对话补全模型"
	}
}

// Enrich 按模型 ID 补齐富元数据。
//
// 策略：策展表精确命中优先（能力/上下文/描述/参数取策展值，留空字段回退启发式）；
// 未命中则全部走启发式（Classify + ParseParamCount + 按能力默认描述）。
//
// 该函数为纯函数，对同一输入确定性返回，可被同步器幂等调用。
func Enrich(id string) EnrichedModel {
	sp := specCatalog[id] // 扩展规格（可能为零值，由远程注册表运行时补）
	if m, ok := catalog[id]; ok {
		cap := m.Capability
		if cap == "" {
			cap = Classify(id)
		}
		param := m.ParamCount
		if param == "" {
			param = ParseParamCount(id)
		}
		desc := m.Description
		if desc == "" {
			desc = defaultDesc(cap)
		}
		return EnrichedModel{
			Capability:       cap,
			ParamCount:       param,
			ContextLength:    m.ContextLength,
			Description:      desc,
			MaxTokens:        sp.MaxTokens,
			PricingIn:        sp.PricingIn,
			PricingOut:       sp.PricingOut,
			License:          sp.License,
			InputModalities:  sp.InputModalities,
			ReleaseDate:      sp.ReleaseDate,
			CardURL:          sp.CardURL,
		}
	}

	cap := Classify(id)
	return EnrichedModel{
		Capability:    cap,
		ParamCount:    ParseParamCount(id),
		ContextLength: 0,
		Description:   defaultDesc(cap),
	}
}

// Lookup 策展表直接查询（测试与诊断用，不走启发式）。
func Lookup(id string) (Meta, bool) {
	m, ok := catalog[id]
	return m, ok
}
