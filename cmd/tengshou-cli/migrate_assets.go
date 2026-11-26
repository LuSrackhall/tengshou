package main

import (
	"context"
	"fmt"

	"github.com/LuSrackhall/tengshou/internal/service"
)

// runMigrateAssets migrates legacy contract address fields into property
// archives. A dry run only prints the plan and writes nothing (no backup, no
// archives, no contract changes).
func runMigrateAssets(dataDir, fontPath string, fontIndex int, dryRun bool) error {
	a, err := openApp(dataDir, fontPath, fontIndex)
	if err != nil {
		return err
	}
	defer a.Close()
	report, err := a.Service.MigrateAssets(context.Background(), dryRun)
	if err != nil {
		return err
	}
	printAssetMigrationReport(report)
	return nil
}

func printAssetMigrationReport(report service.AssetMigrationReport) {
	if report.DryRun {
		fmt.Println("存量资产迁移试运行（不会写入任何数据）：")
	} else {
		fmt.Println("存量资产迁移完成：")
	}
	if report.BackupPath != "" {
		fmt.Printf("  迁移前备份    %s\n", report.BackupPath)
	}
	fmt.Printf("  扫描合同      %d 份\n", report.ScannedContracts)
	fmt.Printf("  新建房产档案  %d 个\n", len(report.CreatedAssets))
	for _, asset := range report.CreatedAssets {
		fmt.Printf("    + %s（来源字段「%s」）\n", asset.Address, asset.Source)
	}
	fmt.Printf("  关联合同      %d 份（其中合并到已有档案 %d 份）\n",
		report.LinkedContracts, report.MergedContracts)
	fmt.Printf("  已有资产关联  %d 份（无需处理）\n", report.AlreadyLinked)
	fmt.Printf("  跳过          %d 份\n", len(report.Skipped))
	for _, item := range report.Skipped {
		fmt.Printf("    - %s「%s」：%s\n", item.ContractNo, item.Title, item.Reason)
	}
	if report.DryRun && (len(report.CreatedAssets) > 0 || report.LinkedContracts > 0) {
		fmt.Println("  提示：去掉 --dry-run 后才会创建档案、关联合同（并先自动备份）。")
	}
}
