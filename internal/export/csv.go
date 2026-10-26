package export

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	"bookkeeping/internal/ledger"
)

// 列顺序固定，导出后可直接用 Excel 打开。
var headers = []string{"时间", "类型", "金额（元）", "收入人", "支出人", "分类", "备注"}

// WriteCSV 输出带 BOM 的 UTF-8 CSV，Excel 直接双击不会乱码。
func WriteCSV(w io.Writer, entries []ledger.Entry) error {
	if _, err := w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return err
	}

	cw := csv.NewWriter(w)
	// Excel 偏好 CRLF。
	cw.UseCRLF = true

	if err := cw.Write(headers); err != nil {
		return err
	}
	for _, e := range entries {
		incomePerson, expensePerson := splitPerson(e)
		row := []string{
			e.OccurredAt,
			kindLabel(e.Kind),
			formatAmount(e.AmountCents),
			incomePerson,
			expensePerson,
			e.Category,
			e.Note,
		}
		for i := range row {
			row[i] = guardFormula(row[i])
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func kindLabel(kind string) string {
	if kind == ledger.KindIncome {
		return "收入"
	}
	return "支出"
}

func splitPerson(e ledger.Entry) (string, string) {
	if e.Kind == ledger.KindIncome {
		return e.Person, ""
	}
	return "", e.Person
}

// formatAmount 把分转成两位小数，全程整数运算，不会出现 0.1+0.2 的误差。
func formatAmount(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return sign + strconv.FormatInt(cents/100, 10) + "." + fmt.Sprintf("%02d", cents%100)
}

// guardFormula 防止单元格内容被 Excel 当作公式执行。
func guardFormula(value string) string {
	trimmed := strings.TrimLeft(value, " \t")
	if trimmed == "" {
		return value
	}
	switch trimmed[0] {
	case '=', '+', '-', '@':
		return "'" + value
	}
	return value
}
