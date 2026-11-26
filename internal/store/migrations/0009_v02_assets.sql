-- v0.2 资产（房产）档案：最小房产字段来自 te-l0hh0 决策 D1。
--
-- asset_type 是判别字段（v0.2 仅 house），后续车位/设备等类型只加类型专属
-- 字段与校验，不动合同/收款主干。在租/空置状态由合同派生（存在「生效中且
-- 未终止」的合同即为在租），不落库，避免手工维护与合同状态漂移。
CREATE TABLE IF NOT EXISTS assets (
    id          TEXT PRIMARY KEY,
    asset_type  TEXT NOT NULL DEFAULT 'house' CHECK (asset_type <> ''),
    name        TEXT NOT NULL,
    address     TEXT NOT NULL DEFAULT '',
    room_no     TEXT NOT NULL DEFAULT '',
    area_sqm    REAL,
    note        TEXT NOT NULL DEFAULT '',
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_assets_name ON assets (name);
CREATE INDEX IF NOT EXISTS idx_assets_type ON assets (asset_type, name);
