package auth

import "testing"

func TestPermissionGroupsContainEverySupportedPermissionOnce(t *testing.T) {
	seen := map[string]string{}
	for _, group := range PermissionGroups() {
		for _, permission := range group.Permissions {
			if previous, ok := seen[permission]; ok {
				t.Fatalf("permission %q appears in both %q and %q", permission, previous, group.Label)
			}
			seen[permission] = group.Label
		}
	}
	if len(seen) != 40 {
		t.Fatalf("permission catalog contains %d permissions, want 40", len(seen))
	}
}

func TestDefaultRolesIncludeUploaderAndFederatedCatalogAccess(t *testing.T) {
	roles := DefaultRoles()
	byID := make(map[string]Role, len(roles))
	for _, role := range roles {
		byID[role.ID] = role
	}
	uploader, ok := byID["uploader"]
	if !ok || !uploader.Builtin || len(uploader.Permissions) != 1 || uploader.Permissions[0] != PermissionUploaderSubmissionsCreate {
		t.Fatalf("unexpected uploader role: %+v", uploader)
	}
	for _, roleID := range []string{"viewer", "operator"} {
		role := byID[roleID]
		permissions := make(map[string]struct{}, len(role.Permissions))
		for _, permission := range role.Permissions {
			permissions[permission] = struct{}{}
		}
		for _, required := range []string{PermissionGoNZBNetSearch, PermissionGoNZBNetGet, PermissionGoNZBNetResolveManifest} {
			if _, ok := permissions[required]; !ok {
				t.Fatalf("role %q is missing %q", roleID, required)
			}
		}
	}
}
