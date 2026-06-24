package controllers

import (
	"context"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/nntp"
	"github.com/datallboy/gonzb/internal/store/pgindex"
	"github.com/labstack/echo/v5"
)

type IndexerScrapeAdminController struct {
	appCtx *app.Context
}

func NewIndexerScrapeAdminController(appCtx *app.Context) *IndexerScrapeAdminController {
	return &IndexerScrapeAdminController{appCtx: appCtx}
}

func (ctrl *IndexerScrapeAdminController) GetConfig(c *echo.Context) error {
	if ctrl == nil || ctrl.appCtx == nil || ctrl.appCtx.SettingsAdmin == nil {
		return jsonError(c, http.StatusServiceUnavailable, "scrape admin api is unavailable")
	}
	runtime, err := ctrl.appCtx.SettingsAdmin.Get(c.Request().Context())
	if err != nil {
		return jsonError(c, settingsErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusOK, buildScrapeAdminResponse(c.Request().Context(), ctrl.appCtx, runtime))
}

func (ctrl *IndexerScrapeAdminController) UpdateConfig(c *echo.Context) error {
	if ctrl == nil || ctrl.appCtx == nil || ctrl.appCtx.SettingsAdmin == nil {
		return jsonError(c, http.StatusServiceUnavailable, "scrape admin api is unavailable")
	}
	var body scrapeAdminConfigPatch
	if err := decodeJSONBody(c, &body); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}

	current, err := ctrl.appCtx.SettingsAdmin.Get(c.Request().Context())
	if err != nil {
		return jsonError(c, settingsErrorStatus(err), err.Error())
	}
	if current.Indexing == nil {
		current.Indexing = app.DefaultRuntimeSettings().Indexing
	}
	next := app.CloneRuntimeSettings(current)
	next.Indexing.ExplicitGroups = append([]app.IndexingScrapeGroupRuntimeSettings(nil), body.ExplicitGroups...)
	next.Indexing.WildcardRules = append([]app.IndexingWildcardRuleRuntimeSettings(nil), body.WildcardRules...)
	next.Indexing.MaterializedGroups = append([]app.IndexingMaterializedGroupRuntimeSettings(nil), body.MaterializedGroups...)

	updated, err := ctrl.appCtx.SettingsAdmin.Update(c.Request().Context(), &app.RuntimeSettingsPatch{Indexing: next.Indexing})
	if err != nil {
		return jsonError(c, settingsErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusOK, buildScrapeAdminResponse(c.Request().Context(), ctrl.appCtx, updated))
}

func (ctrl *IndexerScrapeAdminController) ScanProviders(c *echo.Context) error {
	if ctrl == nil || ctrl.appCtx == nil || ctrl.appCtx.SettingsAdmin == nil {
		return jsonError(c, http.StatusServiceUnavailable, "scrape admin api is unavailable")
	}
	runtime, err := ctrl.appCtx.SettingsAdmin.Get(c.Request().Context())
	if err != nil {
		return jsonError(c, settingsErrorStatus(err), err.Error())
	}
	inventory, err := scanProviderInventory(c.Request().Context(), ctrl.appCtx, runtime)
	if err != nil {
		return jsonError(c, http.StatusBadGateway, err.Error())
	}
	if ctrl.appCtx.PGIndexStore != nil {
		if err := ctrl.appCtx.PGIndexStore.ReplaceIndexerProviderGroupInventory(c.Request().Context(), toPGProviderInventory(inventory)); err != nil {
			return jsonError(c, http.StatusInternalServerError, err.Error())
		}
	}
	next := app.CloneRuntimeSettings(runtime)
	if ctrl.appCtx.PGIndexStore == nil {
		next.Indexing.ProviderGroupInventory = inventory
	} else {
		next.Indexing.ProviderGroupInventory = nil
	}
	updated, err := ctrl.appCtx.SettingsAdmin.Update(c.Request().Context(), &app.RuntimeSettingsPatch{Indexing: next.Indexing})
	if err != nil {
		return jsonError(c, settingsErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusOK, buildScrapeAdminResponse(c.Request().Context(), ctrl.appCtx, updated))
}

func (ctrl *IndexerScrapeAdminController) PreviewWildcardGroups(c *echo.Context) error {
	if ctrl == nil || ctrl.appCtx == nil || ctrl.appCtx.SettingsAdmin == nil {
		return jsonError(c, http.StatusServiceUnavailable, "scrape admin api is unavailable")
	}
	runtime, err := ctrl.appCtx.SettingsAdmin.Get(c.Request().Context())
	if err != nil {
		return jsonError(c, settingsErrorStatus(err), err.Error())
	}
	limit, offset, err := parsePaginationParams(c, 50, 500)
	if err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	items, total := previewWildcardGroupsPage(c.Request().Context(), ctrl.appCtx, runtime.Indexing, queryParamTrimmed(c, "q"), limit, offset)
	return c.JSON(http.StatusOK, map[string]any{
		"items":  items,
		"count":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (ctrl *IndexerScrapeAdminController) ProviderInventory(c *echo.Context) error {
	if ctrl == nil || ctrl.appCtx == nil || ctrl.appCtx.SettingsAdmin == nil {
		return jsonError(c, http.StatusServiceUnavailable, "scrape admin api is unavailable")
	}
	limit, offset, err := parsePaginationParams(c, 50, 500)
	if err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	query := queryParamTrimmed(c, "q")
	sortKey := queryParamTrimmed(c, "sort")
	direction := queryParamTrimmed(c, "direction")
	if ctrl.appCtx.PGIndexStore != nil {
		page, err := ctrl.appCtx.PGIndexStore.ListIndexerProviderGroupInventoryPage(c.Request().Context(), query, limit, offset, sortKey, direction)
		if err != nil {
			return jsonError(c, http.StatusInternalServerError, err.Error())
		}
		return c.JSON(http.StatusOK, map[string]any{
			"items":  page.Items,
			"count":  page.Count,
			"limit":  page.Limit,
			"offset": page.Offset,
		})
	}

	runtime, err := ctrl.appCtx.SettingsAdmin.Get(c.Request().Context())
	if err != nil {
		return jsonError(c, settingsErrorStatus(err), err.Error())
	}
	indexing := runtime.Indexing
	if indexing == nil {
		indexing = app.DefaultRuntimeSettings().Indexing
	}
	items, total := providerInventoryPage(indexing.ProviderGroupInventory, query, limit, offset, sortKey, direction)
	return c.JSON(http.StatusOK, map[string]any{
		"items":  items,
		"count":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (ctrl *IndexerScrapeAdminController) CrosspostPopularity(c *echo.Context) error {
	if ctrl == nil || ctrl.appCtx == nil || ctrl.appCtx.SettingsAdmin == nil {
		return jsonError(c, http.StatusServiceUnavailable, "scrape admin api is unavailable")
	}
	limit, _, err := parsePaginationParams(c, 100, 500)
	if err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	runtime, err := ctrl.appCtx.SettingsAdmin.Get(c.Request().Context())
	if err != nil {
		return jsonError(c, settingsErrorStatus(err), err.Error())
	}
	indexing := runtime.Indexing
	if indexing == nil {
		indexing = app.DefaultRuntimeSettings().Indexing
	}
	return c.JSON(http.StatusOK, map[string]any{
		"items": loadCrosspostPopularity(c.Request().Context(), ctrl.appCtx, indexing, limit),
		"limit": limit,
	})
}

func providerInventoryPage(items []app.IndexingProviderGroupInventoryRuntimeSettings, query string, limit, offset int, sortKey, direction string) ([]app.IndexingProviderGroupInventoryRuntimeSettings, int) {
	query = strings.ToLower(strings.TrimSpace(query))
	filtered := make([]app.IndexingProviderGroupInventoryRuntimeSettings, 0, len(items))
	for _, item := range items {
		if query != "" &&
			!strings.Contains(strings.ToLower(item.GroupName), query) &&
			!strings.Contains(strings.ToLower(item.ProviderID), query) &&
			!strings.Contains(strings.ToLower(item.ProviderName), query) {
			continue
		}
		filtered = append(filtered, item)
	}
	slices.SortFunc(filtered, func(a, b app.IndexingProviderGroupInventoryRuntimeSettings) int {
		cmp := compareProviderInventoryRuntimeSettings(a, b, sortKey)
		if strings.EqualFold(strings.TrimSpace(direction), "desc") {
			cmp = -cmp
		}
		if cmp != 0 {
			return cmp
		}
		if tie := strings.Compare(strings.ToLower(a.GroupName), strings.ToLower(b.GroupName)); tie != 0 {
			return tie
		}
		return strings.Compare(strings.ToLower(a.ProviderID), strings.ToLower(b.ProviderID))
	})
	total := len(filtered)
	if offset >= total {
		return []app.IndexingProviderGroupInventoryRuntimeSettings{}, total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return filtered[offset:end], total
}

func compareProviderInventoryRuntimeSettings(a, b app.IndexingProviderGroupInventoryRuntimeSettings, sortKey string) int {
	switch strings.ToLower(strings.TrimSpace(sortKey)) {
	case "provider_name":
		return strings.Compare(strings.ToLower(a.ProviderName), strings.ToLower(b.ProviderName))
	case "status":
		return strings.Compare(strings.ToLower(a.Status), strings.ToLower(b.Status))
	case "high":
		return compareInt64(a.High, b.High)
	case "scanned_at":
		return strings.Compare(a.ScannedAt, b.ScannedAt)
	case "estimated_articles":
		return compareInt64(estimatedProviderInventoryArticles(a.High, a.Low), estimatedProviderInventoryArticles(b.High, b.Low))
	case "group_name":
		fallthrough
	default:
		return strings.Compare(strings.ToLower(a.GroupName), strings.ToLower(b.GroupName))
	}
}

func estimatedProviderInventoryArticles(high, low int64) int64 {
	if high < low {
		return 0
	}
	return high - low + 1
}

func compareInt64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func (ctrl *IndexerScrapeAdminController) ApplyWildcardGroups(c *echo.Context) error {
	if ctrl == nil || ctrl.appCtx == nil || ctrl.appCtx.SettingsAdmin == nil {
		return jsonError(c, http.StatusServiceUnavailable, "scrape admin api is unavailable")
	}
	runtime, err := ctrl.appCtx.SettingsAdmin.Get(c.Request().Context())
	if err != nil {
		return jsonError(c, settingsErrorStatus(err), err.Error())
	}
	next := app.CloneRuntimeSettings(runtime)
	next.Indexing.MaterializedGroups = materializeWildcardGroups(c.Request().Context(), ctrl.appCtx, next.Indexing)
	if ctrl.appCtx.PGIndexStore != nil {
		next.Indexing.ProviderGroupInventory = nil
	}
	updated, err := ctrl.appCtx.SettingsAdmin.Update(c.Request().Context(), &app.RuntimeSettingsPatch{Indexing: next.Indexing})
	if err != nil {
		return jsonError(c, settingsErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusOK, buildScrapeAdminResponse(c.Request().Context(), ctrl.appCtx, updated))
}

type scrapeAdminConfigPatch struct {
	ExplicitGroups     []app.IndexingScrapeGroupRuntimeSettings       `json:"explicit_groups"`
	WildcardRules      []app.IndexingWildcardRuleRuntimeSettings      `json:"wildcard_rules"`
	MaterializedGroups []app.IndexingMaterializedGroupRuntimeSettings `json:"materialized_groups"`
}

type scrapePreviewItem struct {
	GroupName   string   `json:"group_name"`
	ProviderIDs []string `json:"provider_ids"`
	RuleIDs     []string `json:"rule_ids"`
}

func scanProviderInventory(ctx context.Context, appCtx *app.Context, runtime *app.RuntimeSettings) ([]app.IndexingProviderGroupInventoryRuntimeSettings, error) {
	if runtime == nil {
		return nil, nil
	}
	servers := app.IndexerNNTPServers(runtime)
	if len(servers) == 0 {
		return []app.IndexingProviderGroupInventoryRuntimeSettings{}, nil
	}
	configServers := app.ToConfigServers(servers)
	scannedAt := time.Now().UTC().Format(time.RFC3339)
	out := make([]app.IndexingProviderGroupInventoryRuntimeSettings, 0, 1024)
	for _, cfg := range configServers {
		provider := nntp.NewNNTPProviderWithLogger(cfg, appCtx.Logger)
		listings, err := provider.ListGroups(ctx, "")
		_ = provider.Close()
		if err != nil {
			return nil, err
		}
		for _, item := range listings {
			out = append(out, app.IndexingProviderGroupInventoryRuntimeSettings{
				ProviderID:   strings.TrimSpace(cfg.ID),
				ProviderName: firstNonBlank(strings.TrimSpace(cfg.Host), strings.TrimSpace(cfg.ID)),
				GroupName:    strings.TrimSpace(item.Group),
				High:         item.High,
				Low:          item.Low,
				Status:       strings.TrimSpace(item.Status),
				ScannedAt:    scannedAt,
			})
		}
	}
	return out, nil
}

type scrapeCrosspostPopularityItem struct {
	GroupName                string     `json:"group_name"`
	ObservedArticleCount     int64      `json:"observed_article_count"`
	DistinctMessageCount     int64      `json:"distinct_message_count"`
	DistinctSourceGroupCount int64      `json:"distinct_source_group_count"`
	EffectiveGroup           bool       `json:"effective_group"`
	LastSeenAt               *time.Time `json:"last_seen_at,omitempty"`
}

func loadProviderInventoryStats(ctx context.Context, appCtx *app.Context, indexing *app.IndexingRuntimeSettings) pgindex.IndexerProviderGroupInventoryStats {
	if appCtx != nil && appCtx.PGIndexStore != nil {
		stats, err := appCtx.PGIndexStore.GetIndexerProviderGroupInventoryStats(ctx)
		if err == nil {
			return stats
		}
	}
	if indexing == nil {
		return pgindex.IndexerProviderGroupInventoryStats{}
	}
	return pgindex.IndexerProviderGroupInventoryStats{
		Count:      len(indexing.ProviderGroupInventory),
		LatestScan: latestProviderInventoryScan(indexing.ProviderGroupInventory),
	}
}

func buildScrapeAdminResponse(ctx context.Context, appCtx *app.Context, runtime *app.RuntimeSettings) map[string]any {
	if runtime == nil {
		runtime = app.DefaultRuntimeSettings()
	}
	indexing := runtime.Indexing
	if indexing == nil {
		indexing = app.DefaultRuntimeSettings().Indexing
	}
	stats := loadProviderInventoryStats(ctx, appCtx, indexing)
	return map[string]any{
		"explicit_groups":                ensureExplicitGroups(indexing.ExplicitGroups),
		"wildcard_rules":                 ensureWildcardRules(indexing.WildcardRules),
		"provider_group_inventory":       []app.IndexingProviderGroupInventoryRuntimeSettings{},
		"provider_inventory_count":       stats.Count,
		"provider_inventory_latest_scan": stats.LatestScan,
		"materialized_groups":            ensureMaterializedGroups(indexing.MaterializedGroups),
		"effective_groups":               ensureExplicitGroups(app.EffectiveScrapeGroups(indexing)),
		"preview_groups":                 []scrapePreviewItem{},
		"preview_total":                  0,
		"crosspost_popularity":           []scrapeCrosspostPopularityItem{},
	}
}

func loadCrosspostPopularity(ctx context.Context, appCtx *app.Context, indexing *app.IndexingRuntimeSettings, limit int) []scrapeCrosspostPopularityItem {
	if appCtx == nil || appCtx.PGIndexStore == nil {
		return []scrapeCrosspostPopularityItem{}
	}
	items, err := appCtx.PGIndexStore.GetIndexerCrosspostNewsgroupPopularity(ctx, limit)
	if err != nil {
		return []scrapeCrosspostPopularityItem{}
	}
	effectiveNames := make([]string, 0)
	if indexing != nil {
		for _, group := range app.EffectiveScrapeGroups(indexing) {
			name := strings.TrimSpace(strings.ToLower(group.GroupName))
			if name == "" || slices.Contains(effectiveNames, name) {
				continue
			}
			effectiveNames = append(effectiveNames, name)
		}
	}
	out := make([]scrapeCrosspostPopularityItem, 0, len(items))
	for _, item := range items {
		out = append(out, scrapeCrosspostPopularityItem{
			GroupName:                item.GroupName,
			ObservedArticleCount:     item.ObservedArticleCount,
			DistinctMessageCount:     item.DistinctMessageCount,
			DistinctSourceGroupCount: item.DistinctSourceGroupCount,
			EffectiveGroup:           slices.Contains(effectiveNames, strings.ToLower(strings.TrimSpace(item.GroupName))),
			LastSeenAt:               item.LastSeenAt,
		})
	}
	return out
}

func previewWildcardGroups(ctx context.Context, appCtx *app.Context, indexing *app.IndexingRuntimeSettings, query string) []scrapePreviewItem {
	if indexing == nil {
		return nil
	}
	inventory := indexing.ProviderGroupInventory
	if appCtx != nil && appCtx.PGIndexStore != nil {
		rows, err := appCtx.PGIndexStore.ListIndexerProviderGroupInventoryCandidates(ctx, query, wildcardPatternHints(indexing.WildcardRules))
		if err == nil {
			inventory = fromPGProviderInventory(rows)
		}
	}
	aggregated := map[string]*scrapePreviewItem{}
	for _, item := range inventory {
		group := item.GroupName
		if group == "" {
			continue
		}
		var matchingRuleIDs []string
		for _, rule := range indexing.WildcardRules {
			if !rule.Enabled || !matchesWildcardRule(rule.Pattern, group) {
				continue
			}
			matchingRuleIDs = append(matchingRuleIDs, rule.ID)
		}
		if len(matchingRuleIDs) == 0 {
			continue
		}
		row := aggregated[group]
		if row == nil {
			row = &scrapePreviewItem{GroupName: group}
			aggregated[group] = row
		}
		row.ProviderIDs = appendUniqueString(row.ProviderIDs, item.ProviderID)
		row.RuleIDs = appendUniqueString(row.RuleIDs, matchingRuleIDs...)
	}
	out := make([]scrapePreviewItem, 0, len(aggregated))
	for _, item := range aggregated {
		out = append(out, *item)
	}
	slices.SortFunc(out, func(a, b scrapePreviewItem) int {
		return strings.Compare(strings.ToLower(a.GroupName), strings.ToLower(b.GroupName))
	})
	return out
}

func previewWildcardGroupsPage(ctx context.Context, appCtx *app.Context, indexing *app.IndexingRuntimeSettings, query string, limit, offset int) ([]scrapePreviewItem, int) {
	all := previewWildcardGroups(ctx, appCtx, indexing, query)
	query = strings.ToLower(strings.TrimSpace(query))
	filtered := make([]scrapePreviewItem, 0, len(all))
	for _, item := range all {
		if query != "" &&
			!strings.Contains(strings.ToLower(item.GroupName), query) &&
			!containsAnyFold(item.ProviderIDs, query) &&
			!containsAnyFold(item.RuleIDs, query) {
			continue
		}
		filtered = append(filtered, item)
	}
	total := len(filtered)
	if offset >= total {
		return []scrapePreviewItem{}, total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return filtered[offset:end], total
}

func latestProviderInventoryScan(items []app.IndexingProviderGroupInventoryRuntimeSettings) string {
	latest := ""
	for _, item := range items {
		scannedAt := strings.TrimSpace(item.ScannedAt)
		if scannedAt > latest {
			latest = scannedAt
		}
	}
	return latest
}

func containsAnyFold(items []string, needle string) bool {
	for _, item := range items {
		if strings.Contains(strings.ToLower(strings.TrimSpace(item)), needle) {
			return true
		}
	}
	return false
}

func wildcardPatternHints(rules []app.IndexingWildcardRuleRuntimeSettings) []string {
	out := make([]string, 0, len(rules))
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		best := ""
		for _, part := range strings.FieldsFunc(strings.ToLower(strings.TrimSpace(rule.Pattern)), func(r rune) bool {
			return r == '*' || r == '?' || r == '[' || r == ']' || r == '!' || r == '-' || r == '.'
		}) {
			if len(part) > len(best) {
				best = part
			}
		}
		if len(best) >= 2 {
			out = appendUniqueString(out, best)
		}
	}
	return out
}

func materializeWildcardGroups(ctx context.Context, appCtx *app.Context, indexing *app.IndexingRuntimeSettings) []app.IndexingMaterializedGroupRuntimeSettings {
	preview := previewWildcardGroups(ctx, appCtx, indexing, "")
	existing := make(map[string]app.IndexingMaterializedGroupRuntimeSettings, len(indexing.MaterializedGroups))
	for _, item := range indexing.MaterializedGroups {
		existing[item.GroupName] = item
	}
	out := make([]app.IndexingMaterializedGroupRuntimeSettings, 0, len(preview))
	for _, item := range preview {
		row := existing[item.GroupName]
		row.GroupName = item.GroupName
		row.Enabled = true
		row.ProviderIDs = append([]string(nil), item.ProviderIDs...)
		row.RuleIDs = append([]string(nil), item.RuleIDs...)
		out = append(out, row)
	}
	return out
}

func toPGProviderInventory(in []app.IndexingProviderGroupInventoryRuntimeSettings) []pgindex.IndexerProviderGroupInventoryItem {
	out := make([]pgindex.IndexerProviderGroupInventoryItem, 0, len(in))
	for _, item := range in {
		out = append(out, pgindex.IndexerProviderGroupInventoryItem{
			ProviderID:   item.ProviderID,
			ProviderName: item.ProviderName,
			GroupName:    item.GroupName,
			High:         item.High,
			Low:          item.Low,
			Status:       item.Status,
			ScannedAt:    item.ScannedAt,
		})
	}
	return out
}

func fromPGProviderInventory(in []pgindex.IndexerProviderGroupInventoryItem) []app.IndexingProviderGroupInventoryRuntimeSettings {
	out := make([]app.IndexingProviderGroupInventoryRuntimeSettings, 0, len(in))
	for _, item := range in {
		out = append(out, app.IndexingProviderGroupInventoryRuntimeSettings{
			ProviderID:   item.ProviderID,
			ProviderName: item.ProviderName,
			GroupName:    item.GroupName,
			High:         item.High,
			Low:          item.Low,
			Status:       item.Status,
			ScannedAt:    item.ScannedAt,
		})
	}
	return out
}

func matchesWildcardRule(pattern, group string) bool {
	pattern = strings.TrimSpace(pattern)
	group = strings.TrimSpace(group)
	if pattern == "" || group == "" {
		return false
	}
	if !strings.ContainsAny(pattern, "*?[]") {
		return strings.EqualFold(pattern, group)
	}
	ok, err := path.Match(strings.ToLower(pattern), strings.ToLower(group))
	return err == nil && ok
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func appendUniqueString(dst []string, values ...string) []string {
	seen := make(map[string]struct{}, len(dst))
	for _, item := range dst {
		if item != "" {
			seen[item] = struct{}{}
		}
	}
	for _, item := range values {
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		dst = append(dst, item)
	}
	return dst
}

func ensureExplicitGroups(in []app.IndexingScrapeGroupRuntimeSettings) []app.IndexingScrapeGroupRuntimeSettings {
	if in == nil {
		return []app.IndexingScrapeGroupRuntimeSettings{}
	}
	return in
}

func ensureWildcardRules(in []app.IndexingWildcardRuleRuntimeSettings) []app.IndexingWildcardRuleRuntimeSettings {
	if in == nil {
		return []app.IndexingWildcardRuleRuntimeSettings{}
	}
	return in
}

func ensureProviderInventory(in []app.IndexingProviderGroupInventoryRuntimeSettings) []app.IndexingProviderGroupInventoryRuntimeSettings {
	if in == nil {
		return []app.IndexingProviderGroupInventoryRuntimeSettings{}
	}
	return in
}

func ensureMaterializedGroups(in []app.IndexingMaterializedGroupRuntimeSettings) []app.IndexingMaterializedGroupRuntimeSettings {
	if in == nil {
		return []app.IndexingMaterializedGroupRuntimeSettings{}
	}
	return in
}

func ensurePreviewGroups(in []scrapePreviewItem) []scrapePreviewItem {
	if in == nil {
		return []scrapePreviewItem{}
	}
	return in
}
