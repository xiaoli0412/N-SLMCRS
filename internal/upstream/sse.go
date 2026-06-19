package upstream

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
)

// SSEChunk 一个 SSE data: 行的解析结果。
type SSEChunk struct {
	Data  []byte
	IsDone bool // data: [DONE]
}

// ReadSSEChunks 从 http.Response.Body 读取 SSE chunks，通过 ch 传递。
// 调用方消费 ch，读完或出错后关闭 response.Body。
func ReadSSEChunks(ctx context.Context, resp *http.Response, ch chan<- SSEChunk) {
	defer resp.Body.Close()
	defer close(ch)

	reader := bufio.NewReader(resp.Body)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				return
			}
			// 最后一行可能没有 \n
			if line != "" {
				processSSELine(line, ch)
			}
			return
		}
		processSSELine(line, ch)
	}
}

func processSSELine(line string, ch chan<- SSEChunk) {
	// SSE 格式：空行分隔事件，data: 前缀
	if len(line) == 0 || line == "\r\n" || line == "\n" {
		return
	}
	// 去除 \r\n
	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}

	if len(line) < 6 { // "data: " 最短
		return
	}
	if line[:6] == "data: " {
		data := line[6:]
		if data == "[DONE]" {
			ch <- SSEChunk{IsDone: true}
			return
		}
		ch <- SSEChunk{Data: []byte(data)}
	}
}

// StreamToWriter 将 SSE chunks 从 ch 写入 http.ResponseWriter（流式转发到客户端）。
// 返回写入的总字节数和第一个 chunk 的大小（用于先到先得判定）。
func StreamToWriter(w io.Writer, ch <-chan SSEChunk, flush func()) (totalBytes int, firstChunkSize int, done bool) {
	// SSE 响应头
	for chunk := range ch {
		if chunk.IsDone {
			// 发送 [DONE]
			fmt.Fprintf(w, "data: [DONE]\n\n")
			if flush != nil {
				flush()
			}
			totalBytes += 12
			done = true
			return
		}
		line := fmt.Sprintf("data: %s\n\n", chunk.Data)
		n, _ := w.Write([]byte(line))
		totalBytes += n
		if firstChunkSize == 0 {
			firstChunkSize = n
		}
		if flush != nil {
			flush()
		}
	}
	done = true
	return
}

// FirstChunkDetector 检测流式响应的第一个有效 content chunk（用于先到先得判定）。
// 返回 true 表示已检测到首块，调用方可锁定该上游、取消其余。
func FirstChunkDetector(ch <-chan SSEChunk, firstContent chan<- struct{}) {
	defer close(firstContent)
	for chunk := range ch {
		if chunk.IsDone {
			return
		}
		// 检测是否包含 content（delta.content），而非 role 声明
		if containsContent(chunk.Data) {
			firstContent <- struct{}{}
			return
		}
	}
}

// containsContent 简单检测 SSE chunk JSON 中是否包含 "content" 字段。
// 这是判断「模型真正开始输出」的启发式，比等待完整首块更精确。
func containsContent(data []byte) bool {
	return bytesContains(data, []byte(`"content"`))
}

func bytesContains(s, sub []byte) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i] == sub[0] && string(s[i:i+len(sub)]) == string(sub) {
			return true
		}
	}
	return false
}
