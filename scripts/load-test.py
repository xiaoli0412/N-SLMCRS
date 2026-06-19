#!/usr/bin/env python3
# N-SLMCRS 网关并发负载测试脚本。
#
# 验证：
#   - 多密钥 N 路并发先到先得
#   - 限流（40 RPM）下的成功率
#   - 流式 / 非流式响应
#
# 用法：
#   pip install httpx
#   python scripts/load-test.py --url http://localhost:8787 --token sk-nv-xxx --n 50 --model deepseek-ai/deepseek-r1
#
# 参数：
#   --url      网关地址
#   --token    下游凭证
#   --n        总请求数（默认 30）
#   --concurrency  并发数（默认 10）
#   --model    模型 ID
#   --stream   流式请求
#   --prompt   自定义 prompt

import argparse
import asyncio
import time
from collections import Counter

try:
    import httpx
except ImportError:
    raise SystemExit("缺少 httpx，请先: pip install httpx")


async def one(client: httpx.AsyncClient, url: str, token: str, model: str, prompt: str, stream: bool, idx: int) -> dict:
    payload = {
        "model": model,
        "messages": [{"role": "user", "content": prompt}],
        "max_tokens": 64,
        "stream": stream,
        "temperature": 0.5,
    }
    headers = {"Authorization": f"Bearer {token}", "Content-Type": "application/json"}
    t0 = time.perf_counter()
    try:
        if stream:
            async with client.stream("POST", f"{url}/v1/chat/completions", json=payload, headers=headers, timeout=120) as r:
                ok = r.status_code == 200
                first_chunk = None
                async for line in r.aiter_lines():
                    if line.startswith("data:") and "[DONE]" not in line:
                        first_chunk = time.perf_counter() - t0
                        break
                await r.aread()
                return {"idx": idx, "ok": ok, "status": r.status_code, "ms": (time.perf_counter() - t0) * 1000, "ttft": first_chunk}
        else:
            r = await client.post(f"{url}/v1/chat/completions", json=payload, headers=headers, timeout=120)
            return {"idx": idx, "ok": r.status_code == 200, "status": r.status_code, "ms": (time.perf_counter() - t0) * 1000}
    except Exception as e:
        return {"idx": idx, "ok": False, "status": -1, "ms": (time.perf_counter() - t0) * 1000, "err": str(e)}


async def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--url", default="http://localhost:8787")
    ap.add_argument("--token", required=True)
    ap.add_argument("--model", default="meta/llama-3.1-8b-instruct")
    ap.add_argument("--n", type=int, default=30)
    ap.add_argument("--concurrency", type=int, default=10)
    ap.add_argument("--stream", action="store_true")
    ap.add_argument("--prompt", default="用一句话介绍 NVIDIA。")
    args = ap.parse_args()

    sem = asyncio.Semaphore(args.concurrency)
    results = []

    async with httpx.AsyncClient() as client:

        async def bound(i: int):
            async with sem:
                r = await one(client, args.url, args.token, args.model, args.prompt, args.stream, i)
                results.append(r)
                status_mark = "✓" if r["ok"] else "✗"
                ttft = f" ttft={r.get('ttft')*1000:.0f}ms" if r.get("ttft") else ""
                print(f"[{i+1:>3}] {status_mark} status={r['status']} latency={r['ms']:.0f}ms{ttft}")

        print(f"==== 发起 {args.n} 个请求（并发 {args.concurrency}，模型 {args.model}，stream={args.stream}）====")
        t0 = time.perf_counter()
        await asyncio.gather(*[bound(i) for i in range(args.n)])
        total = time.perf_counter() - t0

    ok = sum(1 for r in results if r["ok"])
    fail = len(results) - ok
    statuses = Counter(r["status"] for r in results)
    latencies = sorted(r["ms"] for r in results if r["ok"])
    p50 = latencies[len(latencies) // 2] if latencies else 0
    p95 = latencies[int(len(latencies) * 0.95)] if latencies else 0
    print("\n==== 汇总 ====")
    print(f"  总请求:   {len(results)}")
    print(f"  成功:     {ok} ({ok/len(results)*100:.1f}%)")
    print(f"  失败:     {fail}")
    print(f"  状态码:   {dict(statuses)}")
    print(f"  P50 延迟: {p50:.0f}ms")
    print(f"  P95 延迟: {p95:.0f}ms")
    print(f"  总耗时:   {total:.1f}s")
    print(f"  有效 RPM: {len(results)/total*60:.1f}")


if __name__ == "__main__":
    asyncio.run(main())
