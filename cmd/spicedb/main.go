package main

import "github.com/jzelinskie/cobrautil"

var persistentPreRunE = cobrautil.CommandStack(
	cobrautil.SyncViperPreRunE("spicedb"),
	cobrautil.ZeroLogPreRunE,
	cobrautil.OpenTelemetryPreRunE,
)

func main() {
	rootCmd := newRootCmd()
	registerMigrateCmd(rootCmd)
	registerHeadCmd(rootCmd)
	registerDeveloperServiceCmd(rootCmd)
	rootCmd.Execute()
}
