package main

import "fmt"

const (
	databaseCommand = "database"
	databaseMigrate = "migrate"
	databaseAudit   = "audit"
)

func parseDatabaseCommand(args []string) (string, bool, error) {
	if len(args) == 0 || args[0] != databaseCommand {
		return "", false, nil
	}
	if len(args) != 2 || (args[1] != databaseMigrate && args[1] != databaseAudit) {
		return "", true, fmt.Errorf("usage: selecto-ecommerce database [migrate|audit]")
	}
	return args[1], true, nil
}
