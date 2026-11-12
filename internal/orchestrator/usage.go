package orchestrator

import (
	"context"
	"sort"
	"time"

	"github.com/tk/t3go/internal/proto"
)

// Usage summarizes the runs of the last days, by day and provider.
func (s *Service) Usage(ctx context.Context, days int) (proto.UsageReport, error) {
	if days <= 0 || days > 365 {
		days = 30
	}
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(days - 1))
	entries, err := s.Store.UsageEntries(ctx, start)
	if err != nil {
		return proto.UsageReport{}, err
	}
	report := proto.UsageReport{DaysCount: days, Since: start.Format("2006-01-02")}
	byDay := map[string]*proto.UsageDay{}
	for i := 0; i < days; i++ {
		date := start.AddDate(0, 0, i).Format("2006-01-02")
		report.Days = append(report.Days, proto.UsageDay{Date: date})
	}
	for i := range report.Days {
		byDay[report.Days[i].Date] = &report.Days[i]
	}
	byProvider := map[string]*proto.UsageProvider{}
	for _, entry := range entries {
		tokens, cost := int64(0), float64(0)
		if entry.Usage != nil {
			tokens = entry.Usage.TotalTokens
			if tokens == 0 {
				tokens = entry.Usage.InputTokens + entry.Usage.OutputTokens
			}
			cost = entry.Usage.CostUSD
		}
		date := entry.StartedAt.In(start.Location()).Format("2006-01-02")
		if day, ok := byDay[date]; ok {
			day.Runs++
			day.Tokens += tokens
			day.Cost += cost
		}
		provider := byProvider[entry.Provider]
		if provider == nil {
			provider = &proto.UsageProvider{Provider: entry.Provider}
			byProvider[entry.Provider] = provider
		}
		provider.Runs++
		provider.Tokens += tokens
		provider.Cost += cost
		report.Totals.Runs++
		report.Totals.Tokens += tokens
		report.Totals.Cost += cost
	}
	for _, provider := range byProvider {
		report.Providers = append(report.Providers, *provider)
	}
	sort.Slice(report.Providers, func(i, j int) bool {
		if report.Providers[i].Tokens != report.Providers[j].Tokens {
			return report.Providers[i].Tokens > report.Providers[j].Tokens
		}
		return report.Providers[i].Provider < report.Providers[j].Provider
	})
	return report, nil
}
