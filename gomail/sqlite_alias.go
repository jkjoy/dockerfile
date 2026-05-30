package main

import (
	"database/sql"
	"sync"

	modernsqlite "modernc.org/sqlite"
)

var registerSQLiteAliasOnce sync.Once

func init() {
	registerSQLiteDriverAliases()
}

func registerSQLiteDriverAliases() {
	registerSQLiteAliasOnce.Do(func() {
		sql.Register("sqlite3", &modernsqlite.Driver{})
	})
}
