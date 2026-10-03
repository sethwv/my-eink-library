package users

import (
	"database/sql"
	"fmt"
)

// Permission identifies an application capability. Roles supply defaults and
// per-user overrides refine them without making handlers understand roles.
type Permission string

const (
	PermissionManageUsers         Permission = "manage_users"
	PermissionManageServer        Permission = "manage_server"
	PermissionOwnShelves          Permission = "own_shelves"
	PermissionManageShelves       Permission = "manage_shelves"
	PermissionCreatePublicShelves Permission = "create_public_shelves"
	PermissionBookmarkLink        Permission = "bookmark_link"
)

type PermissionState struct {
	Permission Permission
	Label      string
	Default    bool
	Override   string
}

var permissions = []struct {
	permission Permission
	label      string
}{
	{PermissionManageUsers, "Manage users"},
	{PermissionManageServer, "Manage server"},
	{PermissionOwnShelves, "Own shelves"},
	{PermissionManageShelves, "Manage all shelves"},
	{PermissionCreatePublicShelves, "Create public shelves"},
	{PermissionBookmarkLink, "Bookmark link"},
}

func validPermission(permission Permission) bool {
	for _, p := range permissions {
		if p.permission == permission {
			return true
		}
	}
	return false
}

func roleHasPermission(role string, permission Permission) bool {
	switch permission {
	case PermissionManageUsers:
		return role == RoleAdmin || role == RoleUserManager
	case PermissionManageServer:
		return role == RoleAdmin || role == RoleServerManager
	case PermissionOwnShelves:
		return validRole(role)
	case PermissionManageShelves:
		return role == RoleAdmin
	case PermissionCreatePublicShelves:
		return role == RoleAdmin
	case PermissionBookmarkLink:
		return validRole(role)
	}
	return false
}

// Can reports whether an enabled user has the effective permission.
func (s *Store) Can(username string, permission Permission) bool {
	if !validPermission(permission) {
		return false
	}
	var id int64
	var role string
	err := s.sql.QueryRow(`SELECT id, role FROM users WHERE username = ? AND enabled = 1`, username).Scan(&id, &role)
	if err != nil {
		return false
	}
	var granted int
	err = s.sql.QueryRow(`SELECT granted FROM user_permission_overrides WHERE user_id = ? AND permission = ?`, id, permission).Scan(&granted)
	if err == nil {
		return granted != 0
	}
	return err == sql.ErrNoRows && roleHasPermission(role, permission)
}

// PermissionStates returns registered permissions with their role default and
// any explicit override, suitable for the user-management UI.
func (s *Store) PermissionStates(id int64) ([]PermissionState, error) {
	var role string
	if err := s.sql.QueryRow(`SELECT role FROM users WHERE id = ?`, id).Scan(&role); err != nil {
		return nil, err
	}
	states := make([]PermissionState, 0, len(permissions))
	for _, p := range permissions {
		state := PermissionState{Permission: p.permission, Label: p.label, Default: roleHasPermission(role, p.permission)}
		var granted int
		err := s.sql.QueryRow(`SELECT granted FROM user_permission_overrides WHERE user_id = ? AND permission = ?`, id, p.permission).Scan(&granted)
		if err == nil {
			if granted != 0 {
				state.Override = "grant"
			} else {
				state.Override = "revoke"
			}
		} else if err != sql.ErrNoRows {
			return nil, err
		}
		states = append(states, state)
	}
	return states, nil
}

// SetPermissionOverride records a per-user grant or revocation. Passing nil
// clears the override and restores the role default.
func (s *Store) SetPermissionOverride(id int64, permission Permission, granted *bool) error {
	return s.SetPermissionOverrides(id, map[Permission]*bool{permission: granted})
}

// SetPermissionOverrides updates a user's explicit permission decisions in one
// transaction. nil restores a role-derived default.
func (s *Store) SetPermissionOverrides(id int64, overrides map[Permission]*bool) error {
	for permission := range overrides {
		if !validPermission(permission) {
			return fmt.Errorf("invalid permission %q", permission)
		}
	}
	tx, err := s.sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for permission, granted := range overrides {
		if granted == nil {
			if _, err := tx.Exec(`DELETE FROM user_permission_overrides WHERE user_id = ? AND permission = ?`, id, permission); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.Exec(`INSERT INTO user_permission_overrides (user_id, permission, granted) VALUES (?, ?, ?) ON CONFLICT(user_id, permission) DO UPDATE SET granted = excluded.granted`, id, permission, boolToInt(*granted)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
