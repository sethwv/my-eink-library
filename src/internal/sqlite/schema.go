// Package sqlite contains small helpers for maintaining legacy SQLite schemas.
package sqlite

import (
	"database/sql"
	"fmt"
)

// Column describes a controlled additive SQLite column migration.
type Column struct {
	Name string
	DDL  string
}

// Columns returns the column names currently defined on table. Callers must
// pass compile-time table names, never user input.
func Columns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	existing := map[string]bool{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		existing[name] = true
	}
	return existing, rows.Err()
}

// EnsureColumns adds missing columns from columns. Table, names, and DDL must
// be package-controlled constants, making the formatted schema statements safe.
func EnsureColumns(db *sql.DB, table string, columns []Column) (map[string]bool, error) {
	existing, err := Columns(db, table)
	if err != nil {
		return nil, err
	}
	for _, column := range columns {
		if existing[column.Name] {
			continue
		}
		if _, err := db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column.Name, column.DDL)); err != nil {
			return nil, err
		}
		existing[column.Name] = true
	}
	return existing, nil
}
