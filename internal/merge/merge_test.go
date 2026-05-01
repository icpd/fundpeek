package merge

import (
	"testing"

	"github.com/lewis/fundsync/internal/model"
)

func TestApplyReplacesOnlySourceImportGroups(t *testing.T) {
	data := map[string]any{
		"funds": []any{
			map[string]any{"code": "000001", "name": "existing", "dwjz": "1.0"},
		},
		"groups": []any{
			map[string]any{"id": "manual", "name": "手动", "codes": []any{"000001"}},
			map[string]any{"id": "import_yangjibao_old", "name": "旧养基宝", "codes": []any{"000002"}},
			map[string]any{"id": "import_xiaobei_keep", "name": "小倍", "codes": []any{"000003"}},
		},
		"groupHoldings": map[string]any{
			"manual":                map[string]any{"000001": map[string]any{"share": 1, "cost": 1}},
			"import_yangjibao_old":  map[string]any{"000002": map[string]any{"share": 2, "cost": 2}},
			"import_xiaobei_keep":   map[string]any{"000003": map[string]any{"share": 3, "cost": 3}},
			"import_yangjibao_old2": map[string]any{"000004": map[string]any{"share": 4, "cost": 4}},
		},
	}

	report, err := Apply(data, model.SyncInput{
		Source: model.SourceYangJiBao,
		Accounts: []model.NormalizedAccount{{
			Source:            model.SourceYangJiBao,
			ExternalAccountID: "acc1",
			Name:              "账户1",
		}, {
			Source:            model.SourceYangJiBao,
			ExternalAccountID: "empty",
			Name:              "空账户",
		}},
		Holdings: []model.NormalizedHolding{{
			Source:            model.SourceYangJiBao,
			ExternalAccountID: "acc1",
			FundCode:          "000001",
			FundName:          "new name ignored",
			Share:             10,
			CostNav:           1.23,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.UpdatedGroups != 1 || report.UpdatedHoldings != 1 || report.AddedFunds != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}

	groups := data["groups"].([]any)
	if len(groups) != 3 {
		t.Fatalf("expected 3 groups after replacing source groups, got %d", len(groups))
	}
	if !hasGroup(groups, "manual") || !hasGroup(groups, "import_xiaobei_keep") || !hasGroup(groups, "import_yangjibao_acc1") {
		t.Fatalf("unexpected groups: %#v", groups)
	}
	if hasGroup(groups, "import_yangjibao_empty") {
		t.Fatal("empty account group should not be synced")
	}
	if name := groupNameByID(groups, "import_yangjibao_acc1"); name != "账户1" {
		t.Fatalf("import group name = %q, want account name without source prefix", name)
	}

	groupHoldings := data["groupHoldings"].(map[string]any)
	if _, ok := groupHoldings["import_yangjibao_old"]; ok {
		t.Fatal("old yangjibao group holding was not removed")
	}
	if _, ok := groupHoldings["import_xiaobei_keep"]; !ok {
		t.Fatal("other source group holding was removed")
	}
}

func TestApplySkipsSourceGroupsWithNoFunds(t *testing.T) {
	data := map[string]any{
		"groups": []any{
			map[string]any{"id": "manual", "name": "手动", "codes": []any{"000001"}},
			map[string]any{"id": "import_yangjibao_old", "name": "旧养基宝", "codes": []any{"000002"}},
		},
		"groupHoldings": map[string]any{
			"manual":               map[string]any{"000001": map[string]any{"share": 1, "cost": 1}},
			"import_yangjibao_old": map[string]any{"000002": map[string]any{"share": 2, "cost": 2}},
		},
	}

	report, err := Apply(data, model.SyncInput{
		Source: model.SourceYangJiBao,
		Accounts: []model.NormalizedAccount{{
			Source:            model.SourceYangJiBao,
			ExternalAccountID: "empty",
			Name:              "空账户",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.UpdatedGroups != 0 || report.UpdatedHoldings != 0 || report.FundCount != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}

	groups := data["groups"].([]any)
	if len(groups) != 1 || !hasGroup(groups, "manual") {
		t.Fatalf("unexpected groups: %#v", groups)
	}
	if hasGroup(groups, "import_yangjibao_old") || hasGroup(groups, "import_yangjibao_empty") {
		t.Fatalf("source import groups should be removed without creating empty groups: %#v", groups)
	}

	groupHoldings := data["groupHoldings"].(map[string]any)
	if _, ok := groupHoldings["import_yangjibao_old"]; ok {
		t.Fatal("old empty source group holding was not removed")
	}
}

func hasGroup(groups []any, id string) bool {
	for _, item := range groups {
		group := item.(map[string]any)
		if group["id"] == id {
			return true
		}
	}
	return false
}

func groupNameByID(groups []any, id string) string {
	for _, item := range groups {
		group := item.(map[string]any)
		if group["id"] == id {
			name, _ := group["name"].(string)
			return name
		}
	}
	return ""
}
