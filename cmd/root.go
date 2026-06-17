package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var configPath string

var rootCmd = &cobra.Command{
	Use:   "netmon",
	Short: "Gateway 連線監控工具",
}

// Execute 執行 CLI 根命令。
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&configPath, "config", ".env", "環境變數設定檔路徑")
	rootCmd.AddCommand(serveCmd)
}
