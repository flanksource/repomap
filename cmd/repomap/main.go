package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/shutdown"
	"github.com/flanksource/commons/logger"
	"github.com/spf13/cobra"
)

var (
	version    = "dev"
	commit     = "unknown"
	date       = "unknown"
	workingDir string
)

var rootCmd = &cobra.Command{
	Use:   "repomap",
	Short: "Repository structure analysis and mapping",
	Long: `Repomap analyzes repository structure, classifying files by language,
scope, and technology. It parses Kubernetes YAML manifests, detects
version changes, and evaluates configurable severity rules using CEL.

When run without a subcommand, defaults to 'scan'.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		clicky.Flags.UseFlags()
	},
}

func init() {
	clicky.BindAllFlags(rootCmd.PersistentFlags(), "tasks", "format")
	logger.Configure(logger.Flags{LogToStderr: true, Color: true})
	rootCmd.PersistentFlags().StringVar(&workingDir, "cwd", "", "Working directory")

	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version, commit hash, build date, and Go version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("repomap %s (commit: %s, built: %s, go: %s)\n",
				version, commit, date, runtime.Version())
		},
	})
}

func main() {
	defer shutdown.RecoverAndShutdown()

	rootCmd.SetArgs(defaultToScan(os.Args[1:]))

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// defaultToScan rewrites CLI args so that `scan` is the implicit subcommand: a
// bare invocation, or one that leads with scan flags or a bare path, becomes
// `scan ...`. Help and completion are left untouched so `repomap --help` (and
// `-h`) shows the root command with its full subcommand list rather than scan's
// help.
func defaultToScan(args []string) []string {
	if len(args) == 0 {
		return []string{"scan"}
	}
	if args[0] == "-h" || args[0] == "--help" || args[0] == "help" || args[0] == "completion" {
		return args
	}
	if args[0] == "" || args[0][0] == '-' {
		return append([]string{"scan"}, args...)
	}
	if cmd, _, _ := rootCmd.Find(args); cmd == rootCmd {
		return append([]string{"scan"}, args...)
	}
	return args
}
