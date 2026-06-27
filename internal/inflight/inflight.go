// Package inflight 提供全局在途请求计数器（v0.7）。
//
// Auto-Pilot 据此实时感知客户端并发量级别（5/10/50/100 档），
// 结合可用 key 数动态调整调度并发度/权重。零依赖、原子操作。
package inflight

import "sync/atomic"

// gauge 全局在途请求计数（原子）。单进程内唯一。
var gauge atomic.Int64

// Inc 在途请求 +1（请求进入调度时调用）。
func Inc() { gauge.Add(1) }

// Dec 在途请求 -1（请求结束/取消时调用，绝不低于 0）。
func Dec() {
	v := gauge.Add(-1)
	if v < 0 {
		// 防御：偶发双重 Dec 不让计数变负影响档位判定
		gauge.Store(0)
	}
}

// Get 返回当前在途请求数。
func Get() int64 { return gauge.Load() }
