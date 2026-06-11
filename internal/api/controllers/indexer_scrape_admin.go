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
	next := app.CloneRuntimeSettings(runtime)
	next.Indexing.ProviderGroupInventory = inventory
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
	return c.JSON(http.StatusOK, map[string]any{
		"items": previewWildcardGroups(runtime.Indexing),
		"count": len(previewWildcardGroups(runtime.Indexing)),
	})
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
	next.Indexing.MaterializedGroups = materializeWildcardGroups(next.Indexing)
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

func buildScrapeAdminResponse(ctx context.Context, appCtx *app.Context, runtime *app.RuntimeSettings) map[string]any {
	if runtime == nil {
		runtime = app.DefaultRuntimeSettings()
	}
	indexing := runtime.Indexing
	if indexing == nil {
		indexing = app.DefaultRuntimeSettings().Indexing
	}
	preview := previewWildcardGroups(indexing)
	crossposts := loadCrosspostPopularity(ctx, appCtx, indexing)
	return map[string]any{
		"explicit_groups":          ensureExplicitGroups(indexing.ExplicitGroups),
		"wildcard_rules":           ensureWildcardRules(indexing.WildcardRules),
		"provider_group_inventory": ensureProviderInventory(indexing.ProviderGroupInventory),
		"materialized_groups":      ensureMaterializedGroups(indexing.MaterializedGroups),
		"effective_groups":         ensureExplicitGroups(app.EffectiveScrapeGroups(indexing)),
		"preview_groups":           ensurePreviewGroups(preview),
		"crosspost_popularity":     crossposts,
	}
}

func loadCrosspostPopularity(ctx context.Context, appCtx *app.Context, indexing *app.IndexingRuntimeSettings) []scrapeCrosspostPopularityItem {
	if appCtx == nil || appCtx.PGIndexStore == nil {
		return []scrapeCrosspostPopularityItem{}
	}
	items, err := appCtx.PGIndexStore.GetIndexerCrosspostNewsgroupPopularity(ctx, 100)
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

func previewWildcardGroups(indexing *app.IndexingRuntimeSettings) []scrapePreviewItem {
	if indexing == nil {
		return nil
	}
	aggregated := map[string]*scrapePreviewItem{}
	for _, item := range indexing.ProviderGroupInventory {
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
	return out
}

func materializeWildcardGroups(indexing *app.IndexingRuntimeSettings) []app.IndexingMaterializedGroupRuntimeSettings {
	preview := previewWildcardGroups(indexing)
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
