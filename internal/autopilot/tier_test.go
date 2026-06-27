package autopilot

import "testing"

func TestClassifyTier(t *testing.T) {
	cases := []struct {
		inflight int64
		want     Tier
	}{
		{0, TierUnknown},
		{1, TierLow},
		{8, TierLow},      // 阈值上界含
		{9, TierMid},
		{25, TierMid},     // 阈值上界含
		{26, TierHigh},
		{75, TierHigh},    // 阈值上界含
		{76, TierPeak},
		{500, TierPeak},
	}
	for _, c := range cases {
		if got := ClassifyTier(c.inflight); got != c.want {
			t.Errorf("ClassifyTier(%d) = %v, want %v", c.inflight, got, c.want)
		}
	}
}

func TestTierString(t *testing.T) {
	want := map[Tier]string{
		TierUnknown: "unknown",
		TierLow:     "low(5)",
		TierMid:     "mid(10)",
		TierHigh:    "high(50)",
		TierPeak:    "peak(100)",
	}
	for tier, s := range want {
		if tier.String() != s {
			t.Errorf("Tier(%v).String() = %q, want %q", tier, tier.String(), s)
		}
	}
}

func TestTierConcurrencyByKeys(t *testing.T) {
	// 无可用 key → 至少 1
	if got := tierConcurrencyByKeys(TierHigh, 0, 100); got != 1 {
		t.Errorf("无 key 时并发 = %d, want 1", got)
	}

	// Low：min(availKeys,5)，受 MaxConcurrency 夹取
	if got := tierConcurrencyByKeys(TierLow, 3, 100); got != 3 {
		t.Errorf("Low 3key = %d, want 3", got)
	}
	if got := tierConcurrencyByKeys(TierLow, 10, 100); got != 5 {
		t.Errorf("Low 10key = %d, want 5 (档位上限)", got)
	}
	// MaxConcurrency 夹取
	if got := tierConcurrencyByKeys(TierLow, 10, 3); got != 3 {
		t.Errorf("Low 10key max=3 = %d, want 3", got)
	}

	// Mid：min(availKeys,10)
	if got := tierConcurrencyByKeys(TierMid, 20, 100); got != 10 {
		t.Errorf("Mid 20key = %d, want 10", got)
	}

	// High：每路 key 压 2，封顶 50
	if got := tierConcurrencyByKeys(TierHigh, 10, 100); got != 20 {
		t.Errorf("High 10key = %d, want 20", got)
	}
	if got := tierConcurrencyByKeys(TierHigh, 40, 100); got != 50 {
		t.Errorf("High 40key = %d, want 50 (档位封顶)", got)
	}

	// Peak：每路 key 压 4，封顶 100
	if got := tierConcurrencyByKeys(TierPeak, 5, 100); got != 20 {
		t.Errorf("Peak 5key = %d, want 20", got)
	}
	if got := tierConcurrencyByKeys(TierPeak, 50, 100); got != 100 {
		t.Errorf("Peak 50key = %d, want 100 (档位封顶)", got)
	}
	if got := tierConcurrencyByKeys(TierPeak, 50, 30); got != 30 {
		t.Errorf("Peak 50key max=30 = %d, want 30 (MaxConcurrency 夹取)", got)
	}

	// Unknown：min(availKeys, maxConcurrency)
	if got := tierConcurrencyByKeys(TierUnknown, 4, 100); got != 4 {
		t.Errorf("Unknown 4key = %d, want 4", got)
	}
}
