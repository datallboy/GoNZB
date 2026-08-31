package auth

type PermissionGroup struct {
	Label       string   `json:"label"`
	Permissions []string `json:"permissions"`
}

// PermissionGroups is the canonical permission catalog presented by the API
// and used to validate custom roles.
func PermissionGroups() []PermissionGroup {
	return []PermissionGroup{
		{Label: "Catalog access", Permissions: []string{
			PermissionIndexerReleasesRead,
			PermissionAggregatorReleasesRead,
			PermissionIndexerReleasesOverride,
			PermissionIndexerReleasesHide,
			PermissionIndexerReleasesPurge,
		}},
		{Label: "Indexer runtime", Permissions: []string{
			PermissionIndexerRuntimeRead,
			PermissionIndexerRuntimeRun,
			PermissionIndexerRuntimePause,
			PermissionIndexerRuntimeConfigure,
		}},
		{Label: "Aggregator runtime", Permissions: []string{
			PermissionAggregatorRuntimeRead,
			PermissionAggregatorRuntimeConfigure,
		}},
		{Label: "GoNZBNet catalog", Permissions: []string{
			PermissionGoNZBNetSearch,
			PermissionGoNZBNetGet,
			PermissionGoNZBNetResolveManifest,
			PermissionGoNZBNetViewTrustScore,
			PermissionGoNZBNetViewSourceNode,
			PermissionGoNZBNetViewCoverage,
		}},
		{Label: "GoNZBNet administration", Permissions: []string{
			PermissionGoNZBNetAdminRead,
			PermissionGoNZBNetAdminWrite,
			PermissionGoNZBNetAdminPeers,
			PermissionGoNZBNetAdminPools,
			PermissionGoNZBNetAdminModeration,
			PermissionGoNZBNetAdminKeys,
			PermissionGoNZBNetAdminCoverage,
			PermissionGoNZBNetAdminScanner,
			PermissionGoNZBNetAdminValidator,
			PermissionGoNZBNetAdminScheduler,
		}},
		{Label: "Uploader", Permissions: []string{
			PermissionUploaderSubmissionsRead,
			PermissionUploaderSubmissionsCreate,
			PermissionUploaderSubmissionsReview,
			PermissionUploaderPublicationsManage,
		}},
		{Label: "Download clients", Permissions: []string{
			PermissionDownloadClientsSend,
		}},
		{Label: "Runtime settings", Permissions: []string{
			PermissionAdminSettingsRead,
			PermissionAdminSettingsWrite,
		}},
		{Label: "Auth users", Permissions: []string{
			PermissionAuthUsersRead,
			PermissionAuthUsersWrite,
		}},
		{Label: "Auth roles", Permissions: []string{
			PermissionAuthRolesRead,
			PermissionAuthRolesWrite,
		}},
		{Label: "Auth tokens", Permissions: []string{
			PermissionAuthTokensRead,
			PermissionAuthTokensWrite,
		}},
	}
}

func knownPermissions() map[string]struct{} {
	out := make(map[string]struct{})
	for _, group := range PermissionGroups() {
		for _, permission := range group.Permissions {
			out[permission] = struct{}{}
		}
	}
	return out
}
