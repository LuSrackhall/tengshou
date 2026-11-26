-- 模板字段值白底遮盖（te-rccn0）：模板版本新增一个渲染开关。
--
-- 开启后套印时先在每个字段标定框内铺白底再写字，用来遮住 Word 模板里
-- 保留下来的【…】占位符。默认 0 关闭，存量模板与新导入模板都保持原样；
-- 开关随「保存为新版本」一起持久化，旧版本不受影响。
ALTER TABLE templates ADD COLUMN mask_field_values INTEGER NOT NULL DEFAULT 0;
