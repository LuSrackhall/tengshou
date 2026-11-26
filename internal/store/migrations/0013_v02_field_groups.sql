-- 合同表单分组（te-bzqhx）：字段定义新增可选分组。
--
-- 空串表示未分组：内置字段由界面按固定规则归组（基本信息 / 租期 /
-- 租金与押金），未分组的动态字段落「其他约定」。列默认空，存量字段
-- 与存量合同数据都不需要回填，表单照旧完整展示。
ALTER TABLE field_definitions ADD COLUMN group_name TEXT NOT NULL DEFAULT '';
