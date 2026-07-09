package auth

import "time"

const (
	PermissionIndexerReleasesRead        = "indexer.releases.read"
	PermissionIndexerReleasesOverride    = "indexer.releases.override"
	PermissionIndexerReleasesHide        = "indexer.releases.hide"
	PermissionIndexerReleasesPurge       = "indexer.releases.purge"
	PermissionIndexerRuntimeRead         = "indexer.runtime.read"
	PermissionIndexerRuntimeRun          = "indexer.runtime.run"
	PermissionIndexerRuntimePause        = "indexer.runtime.pause"
	PermissionIndexerRuntimeConfigure    = "indexer.runtime.configure"
	PermissionAdminSettingsRead          = "admin.settings.read"
	PermissionAdminSettingsWrite         = "admin.settings.write"
	PermissionAggregatorReleasesRead     = "aggregator.releases.read"
	PermissionAggregatorRuntimeRead      = "aggregator.runtime.read"
	PermissionAggregatorRuntimeConfigure = "aggregator.runtime.configure"
	PermissionGoNZBNetSearch             = "gonzbnet.search"
	PermissionGoNZBNetGet                = "gonzbnet.get"
	PermissionGoNZBNetResolveManifest    = "gonzbnet.resolve_manifest"
	PermissionGoNZBNetAdminRead          = "gonzbnet.admin.read"
	PermissionGoNZBNetAdminWrite         = "gonzbnet.admin.write"
	PermissionDownloaderRuntimeRead      = "downloader.runtime.read"
	PermissionDownloaderRuntimeConfigure = "downloader.runtime.configure"
	PermissionAuthUsersRead              = "auth.users.read"
	PermissionAuthUsersWrite             = "auth.users.write"
	PermissionAuthRolesRead              = "auth.roles.read"
	PermissionAuthRolesWrite             = "auth.roles.write"
	PermissionAuthTokensRead             = "auth.tokens.read"
	PermissionAuthTokensWrite            = "auth.tokens.write"
)

type User struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	Enabled     bool      `json:"enabled"`
	RoleIDs     []string  `json:"role_ids,omitempty"`
	Permissions []string  `json:"permissions,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Role struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Builtin     bool      `json:"builtin"`
	Permissions []string  `json:"permissions"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Token struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

type Principal struct {
	UserID      string
	Username    string
	RoleIDs     []string
	Permissions map[string]struct{}
}

func (p *Principal) Has(permission string) bool {
	if p == nil {
		return false
	}
	_, ok := p.Permissions[permission]
	return ok
}

func DefaultRoles() []Role {
	return []Role{
		{
			ID:      "viewer",
			Name:    "Viewer",
			Builtin: true,
			Permissions: []string{
				PermissionIndexerReleasesRead,
				PermissionAggregatorReleasesRead,
			},
		},
		{
			ID:      "operator",
			Name:    "Operator",
			Builtin: true,
			Permissions: []string{
				PermissionIndexerReleasesRead,
				PermissionIndexerRuntimeRead,
				PermissionIndexerRuntimeRun,
				PermissionIndexerRuntimePause,
				PermissionAdminSettingsRead,
				PermissionAggregatorReleasesRead,
				PermissionAggregatorRuntimeRead,
				PermissionDownloaderRuntimeRead,
			},
		},
		{
			ID:      "admin",
			Name:    "Admin",
			Builtin: true,
			Permissions: []string{
				PermissionIndexerReleasesRead,
				PermissionIndexerReleasesOverride,
				PermissionIndexerReleasesHide,
				PermissionIndexerReleasesPurge,
				PermissionIndexerRuntimeRead,
				PermissionIndexerRuntimeRun,
				PermissionIndexerRuntimePause,
				PermissionIndexerRuntimeConfigure,
				PermissionAdminSettingsRead,
				PermissionAdminSettingsWrite,
				PermissionAggregatorReleasesRead,
				PermissionAggregatorRuntimeRead,
				PermissionAggregatorRuntimeConfigure,
				PermissionGoNZBNetSearch,
				PermissionGoNZBNetGet,
				PermissionGoNZBNetResolveManifest,
				PermissionGoNZBNetAdminRead,
				PermissionGoNZBNetAdminWrite,
				PermissionDownloaderRuntimeRead,
				PermissionDownloaderRuntimeConfigure,
				PermissionAuthUsersRead,
				PermissionAuthUsersWrite,
				PermissionAuthRolesRead,
				PermissionAuthRolesWrite,
				PermissionAuthTokensRead,
				PermissionAuthTokensWrite,
			},
		},
	}
}
