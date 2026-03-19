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
	clicky.BindAllFlags(rootCmd.PersistentFlags(), "format")
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

	// Default to scan when no subcommand is given
	if args := os.Args[1:]; len(args) == 0 || args[0] == "" || args[0][0] == '-' {
		rootCmd.SetArgs(append([]string{"scan"}, args...))
	} else if args[0] != "help" && args[0] != "completion" {
		if cmd, _, _ := rootCmd.Find(args); cmd == rootCmd {
			rootCmd.SetArgs(append([]string{"scan"}, args...))
		}
	}

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
