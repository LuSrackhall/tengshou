-- B2 data foundation: contract number/year, configurable field definitions
-- and the append-only change log.

ALTER TABLE contracts ADD COLUMN contract_no TEXT NOT NULL DEFAULT '';
ALTER TABLE contracts ADD COLUMN year INTEGER NOT NULL DEFAULT 0;
ALTER TABLE contracts ADD COLUMN type TEXT NOT NULL DEFAULT '';
ALTER TABLE contracts ADD COLUMN amount REAL NOT NULL DEFAULT 0;

-- Backfill existing rows so the uniqueness guarantee and year views hold on
-- databases created by B1.
UPDATE contracts SET year = CAST(substr(NULLIF(signed_on, ''), 1, 4) AS INTEGER) WHERE year = 0;
UPDATE contracts SET year = CAST(substr(created_at, 1, 4) AS INTEGER) WHERE year = 0;
UPDATE contracts SET contract_no = 'LEGACY-' || id WHERE contract_no = '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_contracts_no ON contracts (contract_no) WHERE contract_no <> '';
CREATE INDEX IF NOT EXISTS idx_contracts_year ON contracts (year, created_at);

CREATE TABLE IF NOT EXISTS field_definitions (
    id            TEXT PRIMARY KEY,
    entity        TEXT NOT NULL CHECK (entity IN ('contract', 'party')),
    key           TEXT NOT NULL,
    label         TEXT NOT NULL,
    type          TEXT NOT NULL,
    required      INTEGER NOT NULL DEFAULT 0,
    default_value TEXT NOT NULL DEFAULT '',
    options_json  TEXT NOT NULL DEFAULT '[]',
    in_ledger     INTEGER NOT NULL DEFAULT 1,
    in_template   INTEGER NOT NULL DEFAULT 1,
    built_in      INTEGER NOT NULL DEFAULT 0,
    archived      INTEGER NOT NULL DEFAULT 0,
    sort_order    INTEGER NOT NULL DEFAULT 0,
    note          TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    UNIQUE (entity, key)
);

CREATE INDEX IF NOT EXISTS idx_fields_entity ON field_definitions (entity, archived, sort_order);

CREATE TABLE IF NOT EXISTS change_log (
    id           TEXT PRIMARY KEY,
    entity       TEXT NOT NULL,
    entity_id    TEXT NOT NULL,
    action       TEXT NOT NULL,
    actor        TEXT NOT NULL DEFAULT '',
    summary      TEXT NOT NULL DEFAULT '',
    changes_json TEXT NOT NULL DEFAULT '[]',
    created_at   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_change_log_entity ON change_log (entity, entity_id, created_at);
CREATE INDEX IF NOT EXISTS idx_change_log_created ON change_log (created_at);

-- Built-in contract fields. They are bound to first-class columns but drive
-- form rendering, Excel headers and ledger ordering exactly like user fields.
INSERT INTO field_definitions
    (id, entity, key, label, type, required, default_value, options_json,
     in_ledger, in_template, built_in, archived, sort_order, note, created_at, updated_at)
VALUES
    ('fd_contract_no',  'contract', 'contract_no', '合同编号', 'text',   1, '', '[]', 1, 1, 1, 0, 10,  '系统自动生成或手工填写；创建后不可修改', '2026-11-22T00:00:00Z', '2026-11-22T00:00:00Z'),
    ('fd_year',         'contract', 'year',        '年度',     'number', 1, '', '[]', 1, 1, 1, 0, 20,  '按年度管理；可跨年查询', '2026-11-22T00:00:00Z', '2026-11-22T00:00:00Z'),
    ('fd_title',        'contract', 'title',       '合同名称', 'text',   1, '', '[]', 1, 1, 1, 0, 30,  '', '2026-11-22T00:00:00Z', '2026-11-22T00:00:00Z'),
    ('fd_type',         'contract', 'type',        '合同类型', 'text',   0, '', '[]', 1, 1, 1, 0, 40,  '', '2026-11-22T00:00:00Z', '2026-11-22T00:00:00Z'),
    ('fd_party_b',      'contract', 'party_b',     '相对方',   'party',  1, '', '[]', 1, 1, 1, 0, 50,  '填写档案中的名称；新名称会自动建立档案', '2026-11-22T00:00:00Z', '2026-11-22T00:00:00Z'),
    ('fd_amount',       'contract', 'amount',      '金额（元）', 'money', 0, '', '[]', 1, 1, 1, 0, 60,  '', '2026-11-22T00:00:00Z', '2026-11-22T00:00:00Z'),
    ('fd_signed_on',    'contract', 'signed_on',   '签订日期', 'date',   0, '', '[]', 1, 1, 1, 0, 70,  '', '2026-11-22T00:00:00Z', '2026-11-22T00:00:00Z'),
    ('fd_valid_from',   'contract', 'valid_from',  '生效日期', 'date',   0, '', '[]', 1, 1, 1, 0, 80,  '', '2026-11-22T00:00:00Z', '2026-11-22T00:00:00Z'),
    ('fd_valid_to',     'contract', 'valid_to',    '到期日期', 'date',   0, '', '[]', 1, 1, 1, 0, 90,  '', '2026-11-22T00:00:00Z', '2026-11-22T00:00:00Z'),
    ('fd_status',       'contract', 'status',      '状态',     'status', 0, '草稿', '["草稿","生效","归档"]', 1, 1, 1, 0, 100, '', '2026-11-22T00:00:00Z', '2026-11-22T00:00:00Z');

-- Built-in party (相对方) fields.
INSERT INTO field_definitions
    (id, entity, key, label, type, required, default_value, options_json,
     in_ledger, in_template, built_in, archived, sort_order, note, created_at, updated_at)
VALUES
    ('fd_party_name',     'party', 'name',      '名称',   'text',     1, '', '[]', 1, 1, 1, 0, 10, '', '2026-11-22T00:00:00Z', '2026-11-22T00:00:00Z'),
    ('fd_party_kind',     'party', 'kind',      '类型',   'select',   0, '个人', '["个人","单位"]', 1, 1, 1, 0, 20, '', '2026-11-22T00:00:00Z', '2026-11-22T00:00:00Z'),
    ('fd_party_id_number','party', 'id_number', '证件号', 'text',     0, '', '[]', 1, 1, 1, 0, 30, '身份证号 / 统一社会信用代码', '2026-11-22T00:00:00Z', '2026-11-22T00:00:00Z'),
    ('fd_party_phone',    'party', 'phone',     '电话',   'text',     0, '', '[]', 1, 1, 1, 0, 40, '', '2026-11-22T00:00:00Z', '2026-11-22T00:00:00Z'),
    ('fd_party_address',  'party', 'address',   '地址',   'text',     0, '', '[]', 1, 1, 1, 0, 50, '', '2026-11-22T00:00:00Z', '2026-11-22T00:00:00Z'),
    ('fd_party_note',     'party', 'note',      '备注',   'longtext', 0, '', '[]', 1, 1, 1, 0, 60, '', '2026-11-22T00:00:00Z', '2026-11-22T00:00:00Z');
