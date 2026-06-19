package autopilot

import "fmt"

// reason 系列生成人类可读的根因描述（写审计日志、前端展示）。
func reasonConcurrency(ewmaRate float64, target int) string {
	return fmt.Sprintf("EWMA成功率 %.1f%%，PID调整目标并发度→%d", ewmaRate*100, target)
}

func reasonWeightBoost(k KeySnap) string {
	return fmt.Sprintf("密钥 %s 成功率 %.1f%%/连续失败%d，降权至 %.1f", k.Mask, k.SuccessRate*100, k.ConsecFail, 0.1)
}

func reasonCircuit(k KeySnap) string {
	return fmt.Sprintf("密钥 %s 连续失败 %d 次，建议短熔断(60s)保护", k.Mask, k.ConsecFail)
}

func reasonForecastThrottle(predictedRPM, capacity float64, target int) string {
	return fmt.Sprintf("Holt-Winters预测RPM %.0f逼近容量%.0f(%.0f%%)，预降并发→%d",
		predictedRPM, capacity, 100*predictedRPM/capacity, target)
}

func reasonForecastCooldown(predictedRPM, capacity float64, seconds int) string {
	return fmt.Sprintf("预测RPM %.0f将超容量%.0f，预冷密钥%ds", predictedRPM, capacity, seconds)
}

func reasonLLMStub(summary string) string {
	return "LLM(stub)：" + summary
}
