package xiaobei

import (
	"math"
	"testing"
)

func TestCostNAVFromAmountEarnings(t *testing.T) {
	cost := costNAVFromAmountEarnings(1200, 200, 1000, 1.2)
	if diff := math.Abs(cost - 1.0); diff > 0.000001 {
		t.Fatalf("cost nav = %f, want 1.0", cost)
	}
}

func TestCostNAVFromAmountEarningsFallsBackForInvalidPrincipal(t *testing.T) {
	cost := costNAVFromAmountEarnings(100, 200, 1000, 1.2)
	if diff := math.Abs(cost - 1.2); diff > 0.000001 {
		t.Fatalf("cost nav = %f, want fallback nav 1.2", cost)
	}
}
