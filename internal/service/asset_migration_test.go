package service_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/LuSrackhall/tengshou/internal/backup"
	"github.com/LuSrackhall/tengshou/internal/config"
	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/service"
	"github.com/LuSrackhall/tengshou/internal/store"
)

// openMigratableService wires a backup manager so the real (non-dry-run)
// migration can take its mandatory pre-migration backup.
func openMigratableService(t *testing.T) (*service.Service, *store.Reader) {
	t.Helper()
	svc, rd, st := openService(t)
	dir := t.TempDir()
	svc.SetDataDir(dir)
	paths, err := config.ResolvePaths(dir)
	if err != nil {
		t.Fatal(err)
	}
	svc.SetBackupManager(backup.New(paths, "test",
		func(ctx context.Context, dest string) error { return st.Snapshot(dest) },
		func(ctx context.Context) (int, error) { return st.SchemaVersion() }))
	return svc, rd
}

func mustTextField(t *testing.T, svc *service.Service, key, label string) {
	t.Helper()
	mustFieldOfType(t, svc, key, label, model.FieldTypeText)
}

func mustFieldOfType(t *testing.T, svc *service.Service, key, label, fieldType string) {
	t.Helper()
	if _, err := svc.CreateField(context.Background(), service.FieldInput{
		Entity: model.FieldEntityContract, Key: key, Label: label,
		Type: fieldType, InLedger: boolPtr(true), InTemplate: boolPtr(true),
	}); err != nil {
		t.Fatalf("create field %s: %v", key, err)
	}
}

func migrateTestContract(t *testing.T, svc *service.Service, partyID, title string, data map[string]any) model.Contract {
	t.Helper()
	return mustContract(t, svc, service.ContractInput{
		Year: 2026, Title: title, PartyBID: partyID, Data: data,
	})
}

func countAssets(t *testing.T, rd *store.Reader) int {
	t.Helper()
	list, err := rd.ListAssets(context.Background(), model.ListFilter{Limit: 500})
	if err != nil {
		t.Fatalf("list assets: %v", err)
	}
	return list.Total
}

func reloadContract(t *testing.T, rd *store.Reader, id string) model.Contract {
	t.Helper()
	contract, err := rd.GetContract(context.Background(), id)
	if err != nil {
		t.Fatalf("get contract %s: %v", id, err)
	}
	return contract
}

func TestMigrateAssetsCreatesLinksMergesAndSkips(t *testing.T) {
	svc, rd := openMigratableService(t)
	ctx := context.Background()
	party := mustParty(t, svc, "张三")

	mustTextField(t, svc, "house_address", "房屋地址")
	mustTextField(t, svc, "alt_address", "房屋坐落")
	mustTextField(t, svc, "remark", "备用信息")
	mustTextField(t, svc, "room_no", "房号")
	mustTextField(t, svc, "mail_address", "通讯地址")
	mustFieldOfType(t, svc, "clause_1", "第一条", model.FieldTypeLongText)

	existing, _, err := svc.SaveAsset(ctx, service.AssetInput{
		Name: "上海房产", Address: "上海市浦东新区世纪大道100号A栋101室",
	})
	if err != nil {
		t.Fatalf("save existing asset: %v", err)
	}

	// Two contracts with the same property address in different spacing.
	c1 := migrateTestContract(t, svc, party.ID, "北京房屋租赁合同 A", map[string]any{
		"house_address": "北京市朝阳区幸福路 1 号院 3 号楼 502 室",
	})
	c2 := migrateTestContract(t, svc, party.ID, "北京房屋租赁合同 B", map[string]any{
		"house_address": "北京市朝阳区幸福路1号院3号楼502室",
	})
	// This address already exists in the archive and must be reused.
	c3 := migrateTestContract(t, svc, party.ID, "上海房屋租赁合同", map[string]any{
		"house_address": "上海市浦东新区世纪大道100号A栋101室",
	})
	// No address keyword: content shape is enough.
	c4 := migrateTestContract(t, svc, party.ID, "海淀房屋租赁合同", map[string]any{
		"remark": "北京市海淀区中关村大街1号3层",
	})
	// No address-like field at all.
	c5 := migrateTestContract(t, svc, party.ID, "无地址合同", map[string]any{})
	// Two conflicting addresses: never guessed.
	c6 := migrateTestContract(t, svc, party.ID, "多地址合同", map[string]any{
		"house_address": "北京市东城区甲1号",
		"alt_address":   "北京市西城区乙2号",
	})
	// Already linked before the migration.
	mustContract(t, svc, service.ContractInput{
		Year: 2026, Title: "已关联合同", PartyBID: party.ID, AssetID: existing.ID,
	})
	// A bare room number is not a full address.
	c8 := migrateTestContract(t, svc, party.ID, "房号合同", map[string]any{"room_no": "302"})
	// Contact addresses are deliberately ignored.
	c9 := migrateTestContract(t, svc, party.ID, "通讯地址合同", map[string]any{
		"mail_address": "北京市朝阳区某某路9号1单元101",
	})
	// A clause quoting a different address must not override the dedicated
	// address field; the contract links to the same archive as c2.
	c10 := migrateTestContract(t, svc, party.ID, "条款引用地址合同", map[string]any{
		"house_address": "北京市朝阳区幸福路1号院3号楼502室",
		"clause_1":      "甲方将位于北京市海淀区中关村大街99号的房屋出租给乙方，租期一年。",
	})
	// A clause alone is not an address value.
	c11 := migrateTestContract(t, svc, party.ID, "仅条款合同", map[string]any{
		"clause_1": "甲方将位于北京市海淀区中关村大街99号的房屋出租给乙方，租期一年。",
	})

	// Dry run: report the plan without writing anything.
	dry, err := svc.MigrateAssets(ctx, true)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !dry.DryRun || dry.BackupPath != "" {
		t.Fatalf("dry run report = %+v", dry)
	}
	if dry.ScannedContracts != 11 || dry.LinkedContracts != 5 ||
		dry.MergedContracts != 3 || dry.AlreadyLinked != 1 || len(dry.CreatedAssets) != 2 ||
		len(dry.Skipped) != 5 {
		t.Fatalf("dry run counts = %+v", dry)
	}
	if got := countAssets(t, rd); got != 1 {
		t.Fatalf("dry run created assets: count = %d", got)
	}
	if got := reloadContract(t, rd, c1.ID).AssetID; got != "" {
		t.Fatalf("dry run linked c1: asset_id = %q", got)
	}

	// Real run: backup first, then create two archives and link five contracts.
	report, err := svc.MigrateAssets(ctx, false)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if report.BackupPath == "" {
		t.Fatal("migration did not record a backup path")
	}
	if info, err := os.Stat(report.BackupPath); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("backup file %q: %v", report.BackupPath, err)
	}
	if report.LinkedContracts != 5 || report.MergedContracts != 3 ||
		report.AlreadyLinked != 1 || len(report.CreatedAssets) != 2 || len(report.Skipped) != 5 {
		t.Fatalf("migration counts = %+v", report)
	}
	if got := countAssets(t, rd); got != 3 {
		t.Fatalf("asset count after migration = %d, want 3", got)
	}

	linkedC1 := reloadContract(t, rd, c1.ID)
	linkedC2 := reloadContract(t, rd, c2.ID)
	linkedC3 := reloadContract(t, rd, c3.ID)
	linkedC4 := reloadContract(t, rd, c4.ID)
	linkedC10 := reloadContract(t, rd, c10.ID)
	if linkedC1.AssetID == "" || linkedC1.AssetID != linkedC2.AssetID {
		t.Fatalf("duplicate address did not share one archive: c1=%q c2=%q",
			linkedC1.AssetID, linkedC2.AssetID)
	}
	if linkedC3.AssetID != existing.ID {
		t.Fatalf("existing archive was not reused: asset_id = %q, want %q",
			linkedC3.AssetID, existing.ID)
	}
	if linkedC4.AssetID == "" || linkedC4.AssetID == linkedC1.AssetID {
		t.Fatalf("shape-only address not migrated: %q", linkedC4.AssetID)
	}
	if linkedC10.AssetID != linkedC1.AssetID {
		t.Fatalf("dedicated address field did not win over the clause: %q", linkedC10.AssetID)
	}
	for _, skipped := range []model.Contract{c5, c6, c8, c9, c11} {
		if got := reloadContract(t, rd, skipped.ID).AssetID; got != "" {
			t.Fatalf("skipped contract %s got asset_id %q", skipped.Title, got)
		}
	}

	// The original custom-field values are preserved for read-only
	// compatibility; only the asset link is added.
	if got := reloadContract(t, rd, c1.ID).Data["house_address"]; got != "北京市朝阳区幸福路 1 号院 3 号楼 502 室" {
		t.Fatalf("original field value changed: %#v", got)
	}

	created, err := rd.GetAsset(ctx, linkedC1.AssetID)
	if err != nil {
		t.Fatal(err)
	}
	if created.Address != "北京市朝阳区幸福路 1 号院 3 号楼 502 室" || !created.Enabled {
		t.Fatalf("created asset = %+v", created)
	}

	// One run-level audit entry with entity=migration; the backup is audited
	// separately by the shared backup path.
	migrations, err := rd.ListChangeLogs(ctx, store.ChangeFilter{
		Entity: model.ChangeEntityMigration, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if migrations.Total != 1 || migrations.Items[0].Action != model.ChangeActionImport ||
		!strings.Contains(migrations.Items[0].Summary, "存量资产迁移") {
		t.Fatalf("migration audit = %+v", migrations.Items)
	}
	backups, err := rd.ListChangeLogs(ctx, store.ChangeFilter{
		Entity: model.ChangeEntityBackup, Limit: 10,
	})
	if err != nil || backups.Total == 0 {
		t.Fatalf("backup audit total = %d err=%v", backups.Total, err)
	}

	// Re-running is a no-op: already-linked contracts are counted, not merged,
	// and no new audit entry is written.
	again, err := svc.MigrateAssets(ctx, false)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if len(again.CreatedAssets) != 0 || again.LinkedContracts != 0 ||
		again.MergedContracts != 0 || again.AlreadyLinked != 6 || len(again.Skipped) != 5 {
		t.Fatalf("second run counts = %+v", again)
	}
	if again.BackupPath != "" {
		t.Fatalf("no-op second run created a backup: %s", again.BackupPath)
	}
	if got := countAssets(t, rd); got != 3 {
		t.Fatalf("second run created assets: count = %d", got)
	}
	migrations, err = rd.ListChangeLogs(ctx, store.ChangeFilter{
		Entity: model.ChangeEntityMigration, Limit: 10,
	})
	if err != nil || migrations.Total != 1 {
		t.Fatalf("second run audit total = %d err=%v", migrations.Total, err)
	}
}

func TestMigrateAssetsDryRunNeedsNoBackupAndRealRunFailsWithoutIt(t *testing.T) {
	svc, _, _ := openService(t)
	party := mustParty(t, svc, "李四")
	mustTextField(t, svc, "house_address", "房屋地址")
	migrateTestContract(t, svc, party.ID, "租房合同", map[string]any{
		"house_address": "北京市朝阳区幸福路1号",
	})

	if _, err := svc.MigrateAssets(context.Background(), true); err != nil {
		t.Fatalf("dry run without backup manager: %v", err)
	}
	if _, err := svc.MigrateAssets(context.Background(), false); err == nil ||
		!strings.Contains(err.Error(), "迁移前自动备份失败") {
		t.Fatalf("real run without backup manager = %v, want 迁移前自动备份失败", err)
	}
}
