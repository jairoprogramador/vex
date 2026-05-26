package cmd

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var (
	fireColor    = color.New(color.FgHiRed, color.Bold)
	structColor  = color.New(color.FgHiGreen, color.Bold)
	versionColor = color.New(color.FgWhite)
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Prints the version of the CLI.",
	Run: func(cmd *cobra.Command, args []string) {

		fireColor.Println("                 )")
		fireColor.Println("              ( /(  ")
		fireColor.Println(" (   (   (    )\\()) ")
		fireColor.Println(" )\\  )\\  )\\  ((_)\\ ")
		fireColor.Println("((_)((_)((_) __((_) ")
		structColor.Println("\\ \\ / /| __|\\ \\/ / ")
		structColor.Println(" \\ V / | _|  >  <  ")
		structColor.Println("  \\_/  |___|/_/\\_\\ ")

		fmt.Println()

		versionStr := fmt.Sprintf("CLI Vex: v%s", version)
		versionColor.Println(versionStr)
		fmt.Println()
	},
}
