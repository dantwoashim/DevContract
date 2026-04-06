// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

var versionShort bool

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		if versionShort {
			fmt.Println(Version)
			return
		}

		fmt.Printf("  EnvSync %s\n", Version)
		fmt.Printf("    Commit:  %s\n", GitCommit)
		fmt.Printf("    Built:   %s\n", BuildDate)
		fmt.Printf("    Go:      %s\n", runtime.Version())
		fmt.Printf("    OS/Arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	},
}

func init() {
	versionCmd.Flags().BoolVar(&versionShort, "short", false, "Print version only")
	rootCmd.AddCommand(versionCmd)
}
