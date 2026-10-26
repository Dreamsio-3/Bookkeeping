-- 回滚 001：删除明细表及其索引
DROP INDEX IF EXISTS idx_entries_category;
DROP INDEX IF EXISTS idx_entries_person;
DROP INDEX IF EXISTS idx_entries_kind;
DROP INDEX IF EXISTS idx_entries_occurred_at;
DROP TABLE IF EXISTS entries;
