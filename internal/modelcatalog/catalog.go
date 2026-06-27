package modelcatalog

// Meta 模型策展元数据（context_length / description 需人工维护，
// param_count 留空时由启发式 ParseParamCount 补；capability 留空时由 Classify 补）。
type Meta struct {
	Capability    string
	ContextLength int
	ParamCount    string
	Description   string
}

// Spec 模型扩展规格（v0.7 模型广场三级"参数说明"页）。
// 与内置策展表分离，避免破坏既有 Meta 位置初始化；远程注册表同步覆盖此表。
type Spec struct {
	MaxTokens       int
	PricingIn       string
	PricingOut      string
	License         string
	InputModalities []string
	ReleaseDate     string
	CardURL         string
}

// specCatalog 内置扩展规格（可选，留空由远程注册表 SyncRegistry 补）。
var specCatalog = map[string]Spec{}

// catalog 主流模型的深度策展目录。
//
// 覆盖 NVIDIA 上常见的高频模型：llama 全家桶 / deepseek / qwen3 / mistral /
// gemma / phi / nemotron / 嵌入族 / 代码族等。未命中条目由 Classify +
// ParseParamCount 启发式兜底（能力分类仍准确，仅 context_length/description 可能缺）。
//
// context_length 取自各模型公开模型卡（build.nvidia.com），为典型上下文窗口。
var catalog = map[string]Meta{
	// ─── Meta Llama ─────────────────────────────────────────────────────
	"meta/llama-3.1-8b-instruct":            {CapChat, 131072, "8B", "Meta Llama 3.1 8B 指令微调对话模型"},
	"meta/llama-3.1-70b-instruct":           {CapChat, 131072, "70B", "Meta Llama 3.1 70B 指令微调对话模型"},
	"meta/llama-3.3-70b-instruct":           {CapChat, 131072, "70B", "Meta Llama 3.3 70B，3.1 的增强版对话模型"},
	"meta/llama-3.2-1b-instruct":            {CapChat, 131072, "1B", "Meta Llama 3.2 1B 轻量对话模型"},
	"meta/llama-3.2-3b-instruct":            {CapChat, 131072, "3B", "Meta Llama 3.2 3B 轻量对话模型"},
	"meta/llama-3.2-11b-vision-instruct":    {CapVision, 131072, "11B", "Meta Llama 3.2 11B 多模态视觉模型"},
	"meta/llama-3.2-90b-vision-instruct":    {CapVision, 131072, "90B", "Meta Llama 3.2 90B 大型多模态视觉模型"},
	"meta/llama-4-maverick-17b-128e-instruct": {CapChat, 1048576, "17B", "Meta Llama 4 Maverick，17B 激活 / 128 专家 MoE，1M 上下文"},
	"meta/llama2-70b":                       {CapChat, 4096, "70B", "Meta Llama 2 70B 基础对话模型"},
	"meta/llama3-chatqa-1.5-70b":            {CapChat, 44032, "70B", "NVIDIA ChatQA 1.5 基于 Llama3 的检索增强对话模型"},
	"meta/codellama-70b":                    {CapCode, 16384, "70B", "Meta Code Llama 70B 代码生成模型"},
	"meta/llama-guard-4-12b":                {CapSafety, 131072, "12B", "Meta Llama Guard 4 内容安全护栏模型"},

	// ─── DeepSeek ───────────────────────────────────────────────────────
	"deepseek-ai/deepseek-coder-6.7b-instruct": {CapCode, 16384, "6.7B", "DeepSeek Coder 6.7B 指令微调代码模型"},
	"deepseek-ai/deepseek-v4-flash":           {CapChat, 131072, "", "DeepSeek V4 Flash 快速对话模型"},
	"deepseek-ai/deepseek-v4-pro":             {CapReasoning, 131072, "", "DeepSeek V4 Pro 深度推理模型"},

	// ─── Qwen ────────────────────────────────────────────────────────────
	"qwen/qwen3-next-80b-a3b-instruct": {CapReasoning, 131072, "80B", "Qwen3-Next 80B(A3B MoE) 推理对话模型"},
	"qwen/qwen3.5-122b-a10b":            {CapChat, 131072, "122B", "Qwen3.5 122B(A10B MoE) 对话模型"},
	"qwen/qwen3.5-397b-a17b":            {CapChat, 131072, "397B", "Qwen3.5 397B(A17B MoE) 大型对话模型"},

	// ─── Mistral ─────────────────────────────────────────────────────────
	"mistralai/mistral-7b-instruct-v0.3":        {CapChat, 32768, "7B", "Mistral 7B Instruct v0.3 对话模型"},
	"mistralai/mistral-large":                    {CapChat, 131072, "123B", "Mistral Large 对话模型"},
	"mistralai/mistral-large-2-instruct":         {CapChat, 131072, "123B", "Mistral Large 2 指令微调对话模型"},
	"mistralai/mistral-large-3-675b-instruct-2512": {CapChat, 131072, "675B", "Mistral Large 3 675B 大型对话模型"},
	"mistralai/mixtral-8x7b-instruct-v0.1":       {CapChat, 32768, "8×7B", "Mixtral 8x7B MoE 指令模型"},
	"mistralai/mixtral-8x22b-v0.1":               {CapChat, 64000, "8×22B", "Mixtral 8x22B 大型 MoE 模型"},
	"mistralai/codestral-22b-instruct-v0.1":      {CapCode, 32768, "22B", "Codestral 22B 代码生成模型"},
	"mistralai/ministral-14b-instruct-2512":      {CapChat, 131072, "14B", "Ministral 14B 边缘对话模型"},
	"mistralai/mistral-nemotron":                 {CapChat, 131072, "", "Mistral-Nemotron NVIDIA 优化的 Mistral 对话模型"},
	"mistralai/mistral-small-4-119b-2603":        {CapChat, 131072, "119B", "Mistral Small 4 119B 对话模型"},
	"mistralai/mistral-medium-3.5-128b":          {CapChat, 131072, "128B", "Mistral Medium 3.5 128B 对话模型"},
	"nv-mistralai/mistral-nemo-12b-instruct":     {CapChat, 131072, "12B", "Mistral NeMo 12B NVIDIA 合作对话模型"},

	// ─── Google Gemma / Phi (Microsoft) ────────────────────────────────
	"google/gemma-2-2b-it":               {CapChat, 8192, "2B", "Gemma 2 2B 指令对话模型"},
	"google/gemma-2b":                    {CapChat, 8192, "2B", "Gemma 2B 基础对话模型"},
	"google/gemma-3-4b-it":               {CapChat, 131072, "4B", "Gemma 3 4B 指令对话模型"},
	"google/gemma-3-12b-it":              {CapChat, 131072, "12B", "Gemma 3 12B 指令对话模型"},
	"google/gemma-4-31b-it":              {CapChat, 131072, "31B", "Gemma 4 31B 指令对话模型"},
	"google/codegemma-7b":                {CapCode, 8192, "7B", "CodeGemma 7B 代码生成模型"},
	"google/codegemma-1.1-7b":            {CapCode, 8192, "7B", "CodeGemma 1.1 7B 代码生成模型"},
	"microsoft/phi-3.5-moe-instruct":      {CapChat, 131072, "42B", "Phi-3.5 MoE 42B 混合专家模型"},
	"microsoft/phi-4-mini-instruct":       {CapChat, 131072, "3.8B", "Phi-4 Mini 3.8B 轻量对话模型"},
	"microsoft/phi-3-vision-128k-instruct": {CapVision, 131072, "4B", "Phi-3 Vision 128K 多模态视觉模型"},
	"microsoft/phi-4-multimodal-instruct": {CapVision, 131072, "5B", "Phi-4 多模态视觉/音频模型"},

	// ─── NVIDIA Nemotron / 自研 ──────────────────────────────────────────
	"nvidia/llama-3.1-nemotron-70b-instruct":      {CapChat, 131072, "70B", "Nemotron 70B 基于 llama-3.1 优化的对话模型"},
	"nvidia/llama-3.1-nemotron-ultra-253b-v1":     {CapChat, 131072, "253B", "Nemotron Ultra 253B 大型对话模型"},
	"nvidia/llama-3.1-nemotron-nano-8b-v1":        {CapReasoning, 131072, "8B", "Nemotron Nano 8B 推理模型"},
	"nvidia/llama-3.3-nemotron-super-49b-v1":      {CapReasoning, 131072, "49B", "Nemotron Super 49B 推理模型"},
	"nvidia/llama-3.3-nemotron-super-49b-v1.5":    {CapReasoning, 131072, "49B", "Nemotron Super 49B v1.5 推理模型"},
	"nvidia/nvidia-nemotron-nano-9b-v2":           {CapReasoning, 131072, "9B", "Nemotron Nano 9B v2 推理模型"},
	"nvidia/nemotron-4-340b-instruct":             {CapChat, 4096, "340B", "Nemotron-4 340B 对话模型"},
	"nvidia/nemotron-mini-4b-instruct":            {CapChat, 4096, "4B", "Nemotron Mini 4B 轻量对话模型"},
	"nvidia/nemotron-3-super-120b-a12b":            {CapChat, 48000, "120B", "Nemotron-3 Super 120B(A12B) MoE 对话模型"},
	"nvidia/nemotron-3-ultra-550b-a55b":            {CapChat, 48000, "550B", "Nemotron-3 Ultra 550B(A55B) 大型 MoE 对话模型"},
	"nvidia/nemotron-3-nano-30b-a3b":               {CapChat, 48000, "30B", "Nemotron-3 Nano 30B(A3B) MoE 对话模型"},
	"nvidia/nemotron-4-340b-reward":                {CapReward, 4096, "340B", "Nemotron-4 340B 奖励 / 偏好模型"},
	"nvidia/nemotron-parse":                        {CapParsing, 32000, "", "NVIDIA Nemotron 文档解析模型"},
	"nvidia/nemoretriever-parse":                   {CapParsing, 32000, "", "NVIDIA NeMo Retriever 文档解析模型"},
	"nvidia/riva-translate-4b-instruct":           {CapTranslation, 4096, "4B", "Riva 翻译 4B 模型"},
	"nvidia/riva-translate-4b-instruct-v1.1":       {CapTranslation, 4096, "4B", "Riva 翻译 4B v1.1 模型"},
	"nvidia/vila":                                 {CapVision, 131072, "7B", "NVIDIA VILA 多模态视觉对话模型"},
	"nvidia/neva-22b":                              {CapVision, 4096, "22B", "NVIDIA NeVa 22B 视觉语言模型"},
	"nvidia/nvclip":                                {CapVision, 32768, "", "NVIDIA NV-CLIP 视觉嵌入模型"},
	"nvidia/ai-synthetic-video-detector":           {CapSafety, 32000, "", "NVIDIA 合成视频检测模型"},
	"nvidia/llama-3.1-nemoguard-8b-content-safety": {CapSafety, 131072, "8B", "NeMoGuard 8B 内容安全护栏"},
	"nvidia/llama-3.1-nemoguard-8b-topic-control":  {CapSafety, 131072, "8B", "NeMoGuard 8B 主题控制护栏"},
	"nvidia/gliner-pii":                            {CapSafety, 8192, "", "GLiNER PII 个人信息识别模型"},

	// ─── 嵌入 / 检索族 ───────────────────────────────────────────────────
	"nvidia/nv-embed-v1":                          {CapEmbedding, 32768, "7B", "NV-Embed v1 文本嵌入模型"},
	"nvidia/embed-qa-4":                           {CapEmbedding, 512, "436M", "NV-Embed-QA 4 检索增强嵌入模型"},
	"nvidia/nv-embedqa-e5-v5":                     {CapEmbedding, 512, "436M", "NV-EmbedQA E5 v5 嵌入模型"},
	"nvidia/nv-embedqa-mistral-7b-v2":             {CapEmbedding, 4096, "7B", "NV-EmbedQA Mistral 7B v2 嵌入模型"},
	"nvidia/llama-3.2-nv-embedqa-1b-v1":           {CapEmbedding, 8192, "1B", "NV-EmbedQA 1B 基于 Llama 3.2 嵌入模型"},
	"nvidia/llama-3.2-nemoretriever-1b-vlm-embed-v1": {CapEmbedding, 8192, "1B", "NeMo Retriever 1B VLM 嵌入模型"},
	"nvidia/llama-nemotron-embed-1b-v2":           {CapEmbedding, 8192, "1B", "Nemotron Embed 1B v2 嵌入模型"},
	"nvidia/llama-nemotron-embed-vl-1b-v2":        {CapEmbedding, 8192, "1B", "Nemotron Embed VL 1B v2 多模态嵌入"},
	"nvidia/nv-embedcode-7b-v1":                    {CapEmbedding, 8192, "7B", "NV-EmbedCode 7B 代码嵌入模型"},
	"baai/bge-m3":                                 {CapEmbedding, 32768, "568M", "BAAI BGE-M3 多语言嵌入模型"},
	"snowflake/arctic-embed-l":                     {CapEmbedding, 8192, "335M", "Snowflake Arctic Embed L 嵌入模型"},
	"nvidia/llama-3.2-nemoretriever-500m-rerank-v2": {CapRerank, 8192, "500M", "NeMo Retriever 500M 重排序模型"},

	// ─── 其他主流 ─────────────────────────────────────────────────────────
	"ibm/granite-3.0-8b-instruct":      {CapChat, 4096, "8B", "IBM Granite 3.0 8B 对话模型"},
	"ibm/granite-34b-code-instruct":    {CapCode, 8192, "34B", "IBM Granite 34B 代码模型"},
	"ai21labs/jamba-1.5-large-instruct": {CapChat, 256000, "12B", "Jamba 1.5 Large Mamba/Transformer 混合架构"},
	"moonshotai/kimi-k2.6":              {CapChat, 200000, "1T", "Kimi K2.6 1T 参数 MoE 对话模型"},
	"databricks/dbrx-instruct":          {CapChat, 32768, "132B", "DBRX 132B MoE 对话模型"},
	"01-ai/yi-large":                    {CapChat, 32768, "34B", "零一万物 Yi-Large 对话模型"},
	"openai/gpt-oss-120b":               {CapChat, 131072, "120B", "OpenAI GPT-OSS 120B 开源对话模型"},
	"openai/gpt-oss-20b":                {CapChat, 131072, "20B", "OpenAI GPT-OSS 20B 开源对话模型"},
	"bytedance/seed-oss-36b-instruct":   {CapChat, 131072, "36B", "字节 Seed-OSS 36B 对话模型"},
	"writer/palmyra-creative-122b":      {CapChat, 9000, "122B", "Writer Palmyra Creative 122B 创意写作模型"},
}
