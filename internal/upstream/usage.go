package upstream

import (
	"sort"
	"strings"
	"time"
)

// UsageRun is one run's usage for the summary.
type UsageRun struct {
	Provider  string
	Model     string
	StartedAt time.Time
	Input     int64
	Output    int64
	Total     int64
	CostUSD   float64
	HasUsage  bool
}

var usageProviders = map[string]bool{"claude": true, "codex": true, "opencode": true, "grok": true, "cursor": true, "antigravity": true, "pi": true, "muse": true}

// UsageSummary aggregates runs into the upstream usage summary shapes.
func UsageSummary(env Environment, sinceDay, untilDay, timeZone string, runs []UsageRun) map[string]any {
	location := time.UTC
	if timeZone != "" {
		if loaded, err := time.LoadLocation(timeZone); err == nil {
			location = loaded
		}
	}
	type key struct{ day, provider, model string }
	type bucket struct {
		input, output int64
		cost          float64
		records       int
		unpriced      int
	}
	buckets := map[key]*bucket{}
	sources := map[string]map[string]any{}
	for _, run := range runs {
		if !run.HasUsage || !usageProviders[run.Provider] {
			continue
		}
		day := run.StartedAt.In(location).Format("2006-01-02")
		k := key{day: day, provider: run.Provider, model: nonEmpty(run.Model, "unknown")}
		b := buckets[k]
		if b == nil {
			b = &bucket{}
			buckets[k] = b
		}
		b.input += run.Input
		b.output += run.Output
		b.cost += run.CostUSD
		b.records++
		if run.CostUSD == 0 {
			b.unpriced++
		}
		source := sources[run.Provider]
		if source == nil {
			source = map[string]any{
				"fingerprint": map[string]any{"hostId": env.ID, "provider": run.Provider,
					"resolvedHomePath": env.Home, "volumeId": ""},
				"status": "ok", "scannedFiles": 0, "skippedFiles": 0, "malformedRecords": 0,
				"distinctSessions": 0, "message": nil,
			}
			sources[run.Provider] = source
		}
		source["distinctSessions"] = source["distinctSessions"].(int) + 1
	}
	rows := make([]any, 0, len(buckets))
	keys := make([]key, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].day != keys[j].day {
			return keys[i].day > keys[j].day
		}
		if keys[i].provider != keys[j].provider {
			return keys[i].provider < keys[j].provider
		}
		return keys[i].model < keys[j].model
	})
	for _, k := range keys {
		b := buckets[k]
		costSource := "providerReported"
		if b.cost == 0 {
			costSource = "unpriced"
		}
		rows = append(rows, map[string]any{
			"day": k.day, "provider": k.provider, "model": k.model,
			"totals": map[string]any{"uncachedInputTokens": b.input, "cachedInputTokens": 0,
				"cacheCreationTokens": 0, "outputTokens": b.output, "reasoningTokens": 0},
			"costUsd": b.cost, "cacheSavingsUsd": 0, "costSource": costSource,
			"records": b.records, "unpricedRecords": b.unpriced, "sessions": b.records,
		})
	}
	sourceRows := make([]any, 0, len(sources))
	for _, provider := range []string{"claude", "codex", "opencode", "grok", "cursor", "antigravity", "pi", "muse"} {
		if row, ok := sources[provider]; ok {
			sourceRows = append(sourceRows, row)
		}
	}
	zone := timeZone
	if strings.TrimSpace(zone) == "" {
		zone = "UTC"
	}
	return map[string]any{
		"contractVersion": 6,
		"readAt":          isoTimestamp(time.Now()),
		"timeZone":        zone,
		"sinceDay":        sinceDay,
		"untilDay":        untilDay,
		"buckets":         rows,
		"sources":         sourceRows,
		"pricing": map[string]any{"status": "unavailable", "source": "t3go",
			"fetchedAt": nil, "knownModels": 0},
		"scanDurationMs": 0,
	}
}
