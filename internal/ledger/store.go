package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	KindIncome  = "income"
	KindExpense = "expense"

	// 时间统一按本地时间字符串存储，便于直接读库排查。
	TimeLayout = "2006-01-02 15:04:05"
	// 前端提交时允许省略秒。
	TimeLayoutShort = "2006-01-02T15:04"
)

var (
	ErrNotFound     = errors.New("记录不存在")
	ErrInvalidInput = errors.New("参数不合法")
)

// Entry 是一条记账明细。ID 用字符串传输，避免 JS 大整数精度问题。
type Entry struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	AmountCents int64  `json:"amountCents"`
	OccurredAt  string `json:"occurredAt"`
	Person      string `json:"person"`
	Category    string `json:"category"`
	Note        string `json:"note"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

// EntryInput 是新增/修改时接收的字段。
type EntryInput struct {
	Kind        string `json:"kind"`
	AmountCents int64  `json:"amountCents"`
	OccurredAt  string `json:"occurredAt"`
	Person      string `json:"person"`
	Category    string `json:"category"`
	Note        string `json:"note"`
}

// Filter 是列表与导出共用的筛选条件。
type Filter struct {
	From     string // YYYY-MM-DD，含当天
	To       string // YYYY-MM-DD，含当天
	Kind     string
	Person   string
	Category string
	Keyword  string
	Limit    int
	Offset   int
}

// Summary 是当前筛选范围内的合计。
type Summary struct {
	IncomeCents  int64 `json:"incomeCents"`
	ExpenseCents int64 `json:"expenseCents"`
	BalanceCents int64 `json:"balanceCents"`
	Count        int64 `json:"count"`
}

type Store struct {
	db *sql.DB
}

func New(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Create(ctx context.Context, in EntryInput) (Entry, error) {
	clean, err := normalize(in)
	if err != nil {
		return Entry{}, err
	}
	now := time.Now().Format(TimeLayout)

	res, err := s.db.ExecContext(ctx, `
		INSERT INTO entries (kind, amount_cents, occurred_at, person, category, note, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		clean.Kind, clean.AmountCents, clean.OccurredAt, clean.Person, clean.Category, clean.Note, now, now)
	if err != nil {
		return Entry{}, fmt.Errorf("保存记录失败: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Entry{}, fmt.Errorf("读取新记录编号失败: %w", err)
	}
	return s.Get(ctx, strconv.FormatInt(id, 10))
}

func (s *Store) Update(ctx context.Context, id string, in EntryInput) (Entry, error) {
	clean, err := normalize(in)
	if err != nil {
		return Entry{}, err
	}
	if _, err := strconv.ParseInt(id, 10, 64); err != nil {
		return Entry{}, ErrNotFound
	}

	res, err := s.db.ExecContext(ctx, `
		UPDATE entries
		   SET kind = ?, amount_cents = ?, occurred_at = ?, person = ?, category = ?, note = ?, updated_at = ?
		 WHERE id = ?`,
		clean.Kind, clean.AmountCents, clean.OccurredAt, clean.Person, clean.Category, clean.Note,
		time.Now().Format(TimeLayout), id)
	if err != nil {
		return Entry{}, fmt.Errorf("更新记录失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Entry{}, ErrNotFound
	}
	return s.Get(ctx, id)
}

func (s *Store) Delete(ctx context.Context, id string) error {
	if _, err := strconv.ParseInt(id, 10, 64); err != nil {
		return ErrNotFound
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM entries WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("删除记录失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Get(ctx context.Context, id string) (Entry, error) {
	if _, err := strconv.ParseInt(id, 10, 64); err != nil {
		return Entry{}, ErrNotFound
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, kind, amount_cents, occurred_at, person, category, note, created_at, updated_at
		  FROM entries WHERE id = ?`, id)

	var e Entry
	var rawID int64
	err := row.Scan(&rawID, &e.Kind, &e.AmountCents, &e.OccurredAt, &e.Person, &e.Category, &e.Note, &e.CreatedAt, &e.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, ErrNotFound
	}
	if err != nil {
		return Entry{}, fmt.Errorf("读取记录失败: %w", err)
	}
	e.ID = strconv.FormatInt(rawID, 10)
	return e, nil
}

// List 返回一页数据和当前筛选条件下的记录总数。
func (s *Store) List(ctx context.Context, f Filter) ([]Entry, int64, error) {
	where, args, err := buildWhere(f)
	if err != nil {
		return nil, 0, err
	}

	var total int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entries"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("统计记录数失败: %w", err)
	}

	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, kind, amount_cents, occurred_at, person, category, note, created_at, updated_at
		  FROM entries`+where+`
		 ORDER BY occurred_at DESC, id DESC
		 LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("查询记录失败: %w", err)
	}
	defer rows.Close()

	entries := make([]Entry, 0, limit)
	for rows.Next() {
		var e Entry
		var rawID int64
		if err := rows.Scan(&rawID, &e.Kind, &e.AmountCents, &e.OccurredAt, &e.Person, &e.Category, &e.Note, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("读取记录失败: %w", err)
		}
		e.ID = strconv.FormatInt(rawID, 10)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("遍历记录失败: %w", err)
	}
	return entries, total, nil
}

// All 返回符合筛选条件的全部记录，用于导出。
func (s *Store) All(ctx context.Context, f Filter) ([]Entry, error) {
	where, args, err := buildWhere(f)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, kind, amount_cents, occurred_at, person, category, note, created_at, updated_at
		  FROM entries`+where+`
		 ORDER BY occurred_at ASC, id ASC`, args...)
	if err != nil {
		return nil, fmt.Errorf("查询记录失败: %w", err)
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		var rawID int64
		if err := rows.Scan(&rawID, &e.Kind, &e.AmountCents, &e.OccurredAt, &e.Person, &e.Category, &e.Note, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("读取记录失败: %w", err)
		}
		e.ID = strconv.FormatInt(rawID, 10)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历记录失败: %w", err)
	}
	return entries, nil
}

func (s *Store) Summary(ctx context.Context, f Filter) (Summary, error) {
	where, args, err := buildWhere(f)
	if err != nil {
		return Summary{}, err
	}
	var sum Summary
	err = s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN kind = 'income'  THEN amount_cents ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN kind = 'expense' THEN amount_cents ELSE 0 END), 0),
			COUNT(*)
		  FROM entries`+where, args...).Scan(&sum.IncomeCents, &sum.ExpenseCents, &sum.Count)
	if err != nil {
		return Summary{}, fmt.Errorf("统计合计失败: %w", err)
	}
	sum.BalanceCents = sum.IncomeCents - sum.ExpenseCents
	return sum, nil
}

// Meta 提供筛选项的候选值和人员/分类的历史取值，前端不硬编码。
type Meta struct {
	Persons    []string `json:"persons"`
	Categories []string `json:"categories"`
}

func (s *Store) Meta(ctx context.Context) (Meta, error) {
	persons, err := s.distinct(ctx, "person")
	if err != nil {
		return Meta{}, err
	}
	categories, err := s.distinct(ctx, "category")
	if err != nil {
		return Meta{}, err
	}
	return Meta{Persons: persons, Categories: categories}, nil
}

func (s *Store) distinct(ctx context.Context, column string) ([]string, error) {
	// column 只来自本文件内的常量，不接受外部输入。
	rows, err := s.db.QueryContext(ctx,
		"SELECT DISTINCT "+column+" FROM entries WHERE "+column+" <> '' ORDER BY "+column)
	if err != nil {
		return nil, fmt.Errorf("读取筛选项失败: %w", err)
	}
	defer rows.Close()

	values := make([]string, 0, 32)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("读取筛选项失败: %w", err)
		}
		values = append(values, v)
	}
	return values, rows.Err()
}

func buildWhere(f Filter) (string, []any, error) {
	var clauses []string
	var args []any

	if f.From != "" {
		from, err := normDay(f.From)
		if err != nil {
			return "", nil, err
		}
		clauses = append(clauses, "occurred_at >= ?")
		args = append(args, from+" 00:00:00")
	}
	if f.To != "" {
		to, err := normDay(f.To)
		if err != nil {
			return "", nil, err
		}
		// occurred_at 是 "YYYY-MM-DD HH:MM:SS"，用小于次日零点实现「含当天」。
		next, err := time.ParseInLocation("2006-01-02", to, time.Local)
		if err != nil {
			return "", nil, err
		}
		clauses = append(clauses, "occurred_at < ?")
		args = append(args, next.AddDate(0, 0, 1).Format("2006-01-02")+" 00:00:00")
	}
	if f.Kind != "" {
		if f.Kind != KindIncome && f.Kind != KindExpense {
			return "", nil, fmt.Errorf("%w: 类型只能是 income 或 expense", ErrInvalidInput)
		}
		clauses = append(clauses, "kind = ?")
		args = append(args, f.Kind)
	}
	if f.Person != "" {
		clauses = append(clauses, "person = ?")
		args = append(args, strings.TrimSpace(f.Person))
	}
	if f.Category != "" {
		clauses = append(clauses, "category = ?")
		args = append(args, strings.TrimSpace(f.Category))
	}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		pattern := "%" + escapeLike(kw) + "%"
		clauses = append(clauses, `(note LIKE ? ESCAPE '\' OR person LIKE ? ESCAPE '\' OR category LIKE ? ESCAPE '\')`)
		args = append(args, pattern, pattern, pattern)
	}

	if len(clauses) == 0 {
		return "", args, nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args, nil
}

// escapeLike 让用户输入中的 % 和 _ 按字面匹配。
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func normDay(raw string) (string, error) {
	t, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(raw), time.Local)
	if err != nil {
		return "", fmt.Errorf("%w: 日期应为 YYYY-MM-DD", ErrInvalidInput)
	}
	return t.Format("2006-01-02"), nil
}

// NormalizeTime 把前端传来的时间统一成数据库格式。
func NormalizeTime(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%w: 请选择时间", ErrInvalidInput)
	}
	layouts := []string{TimeLayout, TimeLayoutShort, "2006-01-02T15:04:05", "2006-01-02 15:04", "2006-01-02"}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return t.Format(TimeLayout), nil
		}
	}
	return "", fmt.Errorf("%w: 时间格式应为 YYYY-MM-DD HH:MM", ErrInvalidInput)
}

func normalize(in EntryInput) (EntryInput, error) {
	out := EntryInput{
		Kind:        strings.TrimSpace(in.Kind),
		AmountCents: in.AmountCents,
		Person:      strings.TrimSpace(in.Person),
		Category:    strings.TrimSpace(in.Category),
		Note:        strings.TrimSpace(in.Note),
	}

	if out.Kind != KindIncome && out.Kind != KindExpense {
		return out, fmt.Errorf("%w: 类型只能是 income 或 expense", ErrInvalidInput)
	}
	if out.AmountCents <= 0 {
		return out, fmt.Errorf("%w: 金额必须大于 0", ErrInvalidInput)
	}
	// 上限 10 亿元，既防止误输入也避免导出时溢出。
	if out.AmountCents > 100_000_000_000 {
		return out, fmt.Errorf("%w: 金额过大", ErrInvalidInput)
	}

	occurredAt, err := NormalizeTime(in.OccurredAt)
	if err != nil {
		return out, err
	}
	out.OccurredAt = occurredAt

	if err := checkLength("收入人/支出人", out.Person, 50); err != nil {
		return out, err
	}
	if err := checkLength("分类", out.Category, 50); err != nil {
		return out, err
	}
	if err := checkLength("备注", out.Note, 500); err != nil {
		return out, err
	}
	return out, nil
}

func checkLength(label, value string, max int) error {
	if len([]rune(value)) > max {
		return fmt.Errorf("%w: %s最多 %d 个字", ErrInvalidInput, label, max)
	}
	return nil
}

// ParseAmount 把 "1,234.56" 这样的输入解析成分。
func ParseAmount(raw string) (int64, error) {
	s := strings.TrimSpace(raw)
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, "，", "")
	if s == "" {
		return 0, fmt.Errorf("%w: 请填写金额", ErrInvalidInput)
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("%w: 金额格式不正确", ErrInvalidInput)
	}
	if f <= 0 {
		return 0, fmt.Errorf("%w: 金额必须大于 0", ErrInvalidInput)
	}
	return int64(math.Round(f * 100)), nil
}
