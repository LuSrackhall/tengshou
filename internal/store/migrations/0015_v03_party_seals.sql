-- 租户档案公章与签名章（私章）图片（te-48hkl）。
--
-- seal_asset_id 指向 seal_assets.id：企业租户的公章长期存一份，合同默认
-- 复用时通过该字段取用；「停用」只需清空字段，公章资产本身保留。
-- signature_seal_asset_id 指向 signature_assets.id：签名章图片与手写签名
-- 并列，作为合同签名位置的二选一来源。
-- *_source 记录图片来源（lan_enroll = 手机自助录入，app_upload = 桌面导入）。
-- 存量档案全部为空，行为不变。
ALTER TABLE parties ADD COLUMN seal_asset_id TEXT NOT NULL DEFAULT '';
ALTER TABLE parties ADD COLUMN seal_source TEXT NOT NULL DEFAULT '';
ALTER TABLE parties ADD COLUMN signature_seal_asset_id TEXT NOT NULL DEFAULT '';
ALTER TABLE parties ADD COLUMN signature_seal_source TEXT NOT NULL DEFAULT '';
