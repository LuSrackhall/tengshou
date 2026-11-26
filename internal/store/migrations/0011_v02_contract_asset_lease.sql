-- v0.2 合同三方选择：甲方（已有 party_a_id）+ 乙方租户（已有 party_b_id）+
-- 资产（本迁移新增 asset_id），并补上正式的租赁起止日期。
--
-- asset_id 允许为空：v0.2 之前建立的合同没有资产档案引用，照常显示与编辑，
-- 用户重新选择后才写入。租期与 v0.1 的「生效日期/到期日期」分开：后者描述
-- 合同自身的生效与失效，租期描述资产占用（后续珠的占用硬校验按租期判断）。
ALTER TABLE contracts ADD COLUMN asset_id TEXT;
ALTER TABLE contracts ADD COLUMN lease_start TEXT NOT NULL DEFAULT '';
ALTER TABLE contracts ADD COLUMN lease_end TEXT NOT NULL DEFAULT '';

-- 资产在租/空置派生与租期冲突查询都按 (asset_id, status) 过滤。
CREATE INDEX IF NOT EXISTS idx_contracts_asset ON contracts (asset_id, status);

-- 内置字段：租赁起租日 / 租赁到期日。插在签订日期（70）之后，台账与 Excel
-- 的日期列次序保持「签订 → 租期 → 生效/到期」。用 WHERE NOT EXISTS 兼容
-- 用户已自建同名键的旧库（唯一约束 (entity,key) 不会被迁移打断）。
INSERT INTO field_definitions
    (id, entity, key, label, type, required, default_value, options_json,
     in_ledger, in_template, built_in, archived, sort_order, note, created_at, updated_at)
SELECT 'fd_lease_start', 'contract', 'lease_start', '租赁起租日', 'date', 0, '', '[]', 1, 1, 1, 0, 72, '租期开始日；新建合同时到期日默认晚一年', '2026-11-26T00:00:00Z', '2026-11-26T00:00:00Z'
WHERE NOT EXISTS (
    SELECT 1 FROM field_definitions WHERE entity = 'contract' AND key = 'lease_start'
);

INSERT INTO field_definitions
    (id, entity, key, label, type, required, default_value, options_json,
     in_ledger, in_template, built_in, archived, sort_order, note, created_at, updated_at)
SELECT 'fd_lease_end', 'contract', 'lease_end', '租赁到期日', 'date', 0, '', '[]', 1, 1, 1, 0, 74, '租期结束日；到期不会自动终止合同或释放资产', '2026-11-26T00:00:00Z', '2026-11-26T00:00:00Z'
WHERE NOT EXISTS (
    SELECT 1 FROM field_definitions WHERE entity = 'contract' AND key = 'lease_end'
);
