package main

import (
	"fmt"
	"os"
	"path/filepath"
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
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		clicky.Flags.UseFlags()
	},
}

func getWorkingDir() (string, error) {
	if workingDir != "" {
		absPath, err := filepath.Abs(workingDir)
		if err != nil {
			return "", fmt.Errorf("failed to resolve working directory: %w", err)
		}
		info, err := os.Stat(absPath)
		if err != nil {
			return "", fmt.Errorf("working directory does not exist: %w", err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("working directory is not a directory: %s", absPath)
		}
		return absPath, nil
	}
	return os.Getwd()
}

func init() {
	clicky.BindAllFlags(rootCmd.PersistentFlags(), "format")
	logger.Configure(logger.Flags{LogToStderr: true, Color: true})
	rootCmd.PersistentFlags().StringVar(&workingDir, "cwd", "", "Working directory")

	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("repomap %s (commit: %s, built: %s, go: %s)\n",
				version, commit, date, runtime.Version())
		},
	})
}

func main() {
	defer shutdown.RecoverAndShutdown()
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
