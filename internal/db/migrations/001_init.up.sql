-- 记账明细表：金额统一用「分」存储，避免浮点误差
CREATE TABLE IF NOT EXISTS entries (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    kind         TEXT    NOT NULL CHECK (kind IN ('income', 'expense')),
    amount_cents INTEGER NOT NULL CHECK (amount_cents > 0),
    occurred_at  TEXT    NOT NULL,
    person       TEXT    NOT NULL DEFAULT '',
    category     TEXT    NOT NULL DEFAULT '',
    note         TEXT    NOT NULL DEFAULT '',
    created_at   TEXT    NOT NULL,
    updated_at   TEXT    NOT NULL
);

-- 列表默认按时间倒序
CREATE INDEX IF NOT EXISTS idx_entries_occurred_at ON entries (occurred_at DESC, id DESC);

-- 类型 + 时间组合筛选
CREATE INDEX IF NOT EXISTS idx_entries_kind ON entries (kind, occurred_at);

-- 按人员、分类筛选
CREATE INDEX IF NOT EXISTS idx_entries_person ON entries (person);
CREATE INDEX IF NOT EXISTS idx_entries_category ON entries (category);
