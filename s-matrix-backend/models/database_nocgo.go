//go:build !cgo

package models

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	_ "modernc.org/sqlite"
)

func openSQLiteDialector(path string) gorm.Dialector {
	return sqlite.New(sqlite.Config{
		DriverName: "sqlite",
		DSN:        path,
	})
}
