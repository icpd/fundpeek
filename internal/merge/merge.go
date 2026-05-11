package merge

import (
	"fmt"
	"sort"
	"time"

	"github.com/icpd/fundpeek/internal/model"
)

type Report struct {
	Source          string
	AccountCount    int
	FundCount       int
	AddedFunds      int
	UpdatedGroups   int
	UpdatedHoldings int
	EstimatedShares int
}

func Apply(data map[string]any, input model.SyncInput) (Report, error) {
	if data == nil {
		data = map[string]any{}
	}
	report := Report{Source: input.Source, AccountCount: len(input.Accounts)}

	groupByAccount := map[string]model.NormalizedAccount{}
	for _, account := range input.Accounts {
		id := account.ExternalAccountID
		if id == "" {
			id = "default"
		}
		groupByAccount[id] = account
	}
	for _, holding := range input.Holdings {
		id := holding.ExternalAccountID
		if id == "" {
			id = "default"
		}
		if _, ok := groupByAccount[id]; !ok {
			groupByAccount[id] = model.NormalizedAccount{
				Source:            input.Source,
				ExternalAccountID: id,
				Name:              "默认账户",
			}
		}
	}

	funds, added := mergeFunds(data["funds"], input.Holdings)
	data["funds"] = funds
	report.AddedFunds = added
	report.FundCount = len(uniqueFunds(input.Holdings))

	groups, groupIDs := mergeGroups(data["groups"], input.Source, groupByAccount, input.Holdings)
	data["groups"] = groups
	report.UpdatedGroups = len(groupIDs)

	groupHoldings := mergeGroupHoldings(data["groupHoldings"], input.Source, input.Holdings)
	data["groupHoldings"] = groupHoldings
	for _, holding := range input.Holdings {
		report.UpdatedHoldings++
		if holding.EstimatedShare {
			report.EstimatedShares++
		}
	}
	return report, nil
}

func mergeFunds(existing any, holdings []model.NormalizedHolding) ([]any, int) {
	out := toSlice(existing)
	seen := map[string]int{}
	for i, item := range out {
		m := toMap(item)
		if code, _ := m["code"].(string); code != "" {
			seen[code] = i
			out[i] = m
		}
	}
	added := 0
	for _, holding := range holdings {
		if holding.FundCode == "" {
			continue
		}
		if i, ok := seen[holding.FundCode]; ok {
			m := toMap(out[i])
			if _, ok := m["name"].(string); !ok && holding.FundName != "" {
				m["name"] = holding.FundName
			}
			out[i] = m
			continue
		}
		out = append(out, map[string]any{
			"code":    holding.FundCode,
			"name":    holding.FundName,
			"addedAt": time.Now().UnixMilli(),
		})
		seen[holding.FundCode] = len(out) - 1
		added++
	}
	return out, added
}

func mergeGroups(existing any, source string, accounts map[string]model.NormalizedAccount, holdings []model.NormalizedHolding) ([]any, map[string]bool) {
	out := make([]any, 0)
	for _, item := range toSlice(existing) {
		m := toMap(item)
		id, _ := m["id"].(string)
		if !isImportGroup(source, id) {
			out = append(out, m)
		}
	}

	codesByAccount := map[string]map[string]bool{}
	for _, holding := range holdings {
		accountID := holding.ExternalAccountID
		if accountID == "" {
			accountID = "default"
		}
		if codesByAccount[accountID] == nil {
			codesByAccount[accountID] = map[string]bool{}
		}
		if holding.FundCode != "" {
			codesByAccount[accountID][holding.FundCode] = true
		}
	}

	groupIDs := map[string]bool{}
	accountIDs := make([]string, 0, len(accounts))
	for id := range accounts {
		accountIDs = append(accountIDs, id)
	}
	sort.Strings(accountIDs)
	for _, accountID := range accountIDs {
		codes := sortedCodes(codesByAccount[accountID])
		if len(codes) == 0 {
			continue
		}
		account := accounts[accountID]
		id := groupID(source, accountID)
		groupIDs[id] = true
		out = append(out, map[string]any{
			"id":    id,
			"name":  groupName(account.Name),
			"codes": codes,
		})
	}
	return out, groupIDs
}

func mergeGroupHoldings(existing any, source string, holdings []model.NormalizedHolding) map[string]any {
	out := toMap(existing)
	for key := range out {
		if isImportGroup(source, key) {
			delete(out, key)
		}
	}
	for _, holding := range holdings {
		if holding.FundCode == "" {
			continue
		}
		accountID := holding.ExternalAccountID
		if accountID == "" {
			accountID = "default"
		}
		gid := groupID(source, accountID)
		group := toMap(out[gid])
		group[holding.FundCode] = map[string]any{
			"share": holding.Share,
			"cost":  holding.CostNav,
		}
		out[gid] = group
	}
	return out
}

func groupID(source, accountID string) string {
	if accountID == "" {
		accountID = "default"
	}
	return fmt.Sprintf("import_%s_%s", source, accountID)
}

func isImportGroup(source, id string) bool {
	return len(id) > len("import_"+source+"_") && id[:len("import_"+source+"_")] == "import_"+source+"_"
}

func groupName(accountName string) string {
	if accountName == "" {
		accountName = "默认账户"
	}
	return accountName
}

func sortedCodes(set map[string]bool) []string {
	codes := make([]string, 0, len(set))
	for code := range set {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

func uniqueFunds(holdings []model.NormalizedHolding) map[string]bool {
	out := map[string]bool{}
	for _, holding := range holdings {
		if holding.FundCode != "" {
			out[holding.FundCode] = true
		}
	}
	return out
}

func toSlice(value any) []any {
	if value == nil {
		return []any{}
	}
	if out, ok := value.([]any); ok {
		return out
	}
	return []any{}
}

func toMap(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	if out, ok := value.(map[string]any); ok {
		return out
	}
	return map[string]any{}
}
