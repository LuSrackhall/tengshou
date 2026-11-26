-- v0.2 合同生命周期：状态机新增「已终止」，并记录终止日期 / 原因 / 备注。
--
-- 已终止（terminated）与归档（archived）区分：终止表示租约提前结束（提前退租 /
-- 作废 / 其他 / 续签联动），合同保留在台账并可筛选，但不再占用房产；归档只是
-- 数据层面的归档。到期本身不会自动改状态（te-l0hh0 D4）。
--
-- SQLite 无法修改已有 CHECK 约束，按官方 12 步流程重建 contracts 表；迁移在
-- 关闭外键检查的会话里执行（store.migrate 负责开关并在结束后做外键校验），
-- 重建时逐列拷贝，子表（签名 / 输出 PDF / 公章关联）的引用关系保持不变。
CREATE TABLE contracts_lifecycle (
    id                 TEXT PRIMARY KEY,
    title              TEXT NOT NULL,
    template_id        TEXT REFERENCES templates (id) ON DELETE SET NULL,
    party_a_id         TEXT REFERENCES parties (id) ON DELETE SET NULL,
    party_b_id         TEXT REFERENCES parties (id) ON DELETE SET NULL,
    status             TEXT NOT NULL DEFAULT 'draft'
                       CHECK (status IN ('draft', 'active', 'terminated', 'archived')),
    signed_on          TEXT NOT NULL DEFAULT '',
    valid_from         TEXT NOT NULL DEFAULT '',
    valid_to           TEXT NOT NULL DEFAULT '',
    data_json          TEXT NOT NULL DEFAULT '{}',
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL,
    contract_no        TEXT NOT NULL DEFAULT '',
    year               INTEGER NOT NULL DEFAULT 0,
    type               TEXT NOT NULL DEFAULT '',
    amount             REAL NOT NULL DEFAULT 0,
    source_contract_id TEXT NOT NULL DEFAULT '',
    asset_id           TEXT,
    lease_start        TEXT NOT NULL DEFAULT '',
    lease_end          TEXT NOT NULL DEFAULT '',
    terminated_on      TEXT NOT NULL DEFAULT '',
    termination_reason TEXT NOT NULL DEFAULT '',
    termination_note   TEXT NOT NULL DEFAULT ''
);

INSERT INTO contracts_lifecycle (
    id, title, template_id, party_a_id, party_b_id, status, signed_on, valid_from,
    valid_to, data_json, created_at, updated_at, contract_no, year, type, amount,
    source_contract_id, asset_id, lease_start, lease_end
)
SELECT
    id, title, template_id, party_a_id, party_b_id, status, signed_on, valid_from,
    valid_to, data_json, created_at, updated_at, contract_no, year, type, amount,
    source_contract_id, asset_id, lease_start, lease_end
FROM contracts;

DROP TABLE contracts;
ALTER TABLE contracts_lifecycle RENAME TO contracts;

-- 重建 contracts 上的全部索引（随旧表一起被删除）。
CREATE INDEX IF NOT EXISTS idx_contracts_party_a ON contracts (party_a_id);
CREATE INDEX IF NOT EXISTS idx_contracts_party_b ON contracts (party_b_id);
CREATE INDEX IF NOT EXISTS idx_contracts_template ON contracts (template_id);
CREATE INDEX IF NOT EXISTS idx_contracts_status ON contracts (status);
CREATE INDEX IF NOT EXISTS idx_contracts_created ON contracts (created_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_contracts_no ON contracts (contract_no) WHERE contract_no <> '';
CREATE INDEX IF NOT EXISTS idx_contracts_year ON contracts (year, created_at);
CREATE INDEX IF NOT EXISTS idx_contracts_source ON contracts (source_contract_id);
CREATE INDEX IF NOT EXISTS idx_contracts_asset ON contracts (asset_id, status);
-- 到期提醒按「生效中 + 租期到期日」查询。
CREATE INDEX IF NOT EXISTS idx_contracts_lease_end ON contracts (status, lease_end);

-- 内置「状态」字段的选项补上「已终止」；只在仍是系统默认选项时更新，避免
-- 覆盖用户在字段页的自定义（内置字段本身不允许改选项，这里是防御性写法）。
UPDATE field_definitions
SET options_json = '["草稿","生效","已终止","归档"]'
WHERE entity = 'contract' AND key = 'status' AND built_in = 1
  AND options_json = '["草稿","生效","归档"]';
