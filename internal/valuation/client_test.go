package valuation

import "testing"

func TestParseFundGZ(t *testing.T) {
	got, err := ParseFundGZ(`jsonpgz({"fundcode":"000001","name":"华夏成长混合","jzrq":"2026-05-08","dwjz":"1.1960","gsz":"1.2343","gszzl":"3.20","gztime":"2026-05-11 14:12"});`)
	if err != nil {
		t.Fatal(err)
	}
	if got.Code != "000001" || got.Name != "华夏成长混合" {
		t.Fatalf("unexpected identity: %#v", got)
	}
	if !got.HasGSZZL || got.GSZZL != 3.20 {
		t.Fatalf("GSZZL = %v/%f, want true/3.20", got.HasGSZZL, got.GSZZL)
	}
	if !got.HasGSZ || got.GSZ != 1.2343 {
		t.Fatalf("GSZ = %v/%f, want true/1.2343", got.HasGSZ, got.GSZ)
	}
}

func TestParseNetValues(t *testing.T) {
	body := `var apidata={ content:"<table><tbody><tr><td>2026-05-08</td><td class='tor bold'>1.1960</td><td>3.7690</td><td>-1.48%</td></tr><tr><td>2026-05-07</td><td>1.2140</td><td>3.7870</td><td>1.51%</td></tr></tbody></table>",records:2,pages:1,curpage:1};`
	got := ParseNetValues(body)
	if len(got) != 2 {
		t.Fatalf("len(ParseNetValues) = %d, want 2: %#v", len(got), got)
	}
	if got[0].Date != "2026-05-07" || got[1].Date != "2026-05-08" {
		t.Fatalf("dates not sorted ascending: %#v", got)
	}
	if !got[1].HasGrowth || got[1].Growth != -1.48 {
		t.Fatalf("latest growth = %v/%f, want true/-1.48", got[1].HasGrowth, got[1].Growth)
	}
}
