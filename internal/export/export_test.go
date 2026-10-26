package export

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"io"
	"strings"
	"testing"

	"bookkeeping/internal/ledger"
)

func sampleEntries() []ledger.Entry {
	return []ledger.Entry{
		{ID: "1", Kind: ledger.KindIncome, AmountCents: 123456, OccurredAt: "2026-03-01 09:00:00", Person: "张三", Category: "工资", Note: "三月"},
		{ID: "2", Kind: ledger.KindExpense, AmountCents: 1, OccurredAt: "2026-03-02 12:00:00", Person: "李四", Category: "餐饮", Note: `带"引号"和,逗号`},
		{ID: "3", Kind: ledger.KindExpense, AmountCents: 999999999, OccurredAt: "2026-03-03 18:00:00", Person: "", Category: "", Note: "=cmd|' /C calc'!A0"},
	}
}

func TestWriteCSV(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteCSV(&buf, sampleEntries()); err != nil {
		t.Fatalf("导出 CSV 失败: %v", err)
	}

	raw := buf.Bytes()
	if !bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatal("CSV 应以 UTF-8 BOM 开头，否则 Excel 会乱码")
	}

	records, err := csv.NewReader(bytes.NewReader(raw[3:])).ReadAll()
	if err != nil {
		t.Fatalf("解析 CSV 失败: %v", err)
	}
	if len(records) != 4 {
		t.Fatalf("应有 1 行表头 + 3 行数据，实际 %d 行", len(records))
	}

	if strings.Join(records[0], "|") != "时间|类型|金额（元）|收入人|支出人|分类|备注" {
		t.Fatalf("表头不符: %v", records[0])
	}

	// 收入行：金额进「收入人」列方向，支出人留空。
	if records[1][3] != "张三" || records[1][4] != "" {
		t.Fatalf("收入人/支出人列分配错误: %v", records[1])
	}
	if records[1][2] != "1234.56" {
		t.Fatalf("金额格式错误: %q", records[1][2])
	}

	// 支出行。
	if records[2][3] != "" || records[2][4] != "李四" {
		t.Fatalf("支出人列错误: %v", records[2])
	}
	if records[2][2] != "0.01" {
		t.Fatalf("0.01 元应导出为 0.01，实际 %q", records[2][2])
	}

	// 以 = 开头的内容前面要加单引号，防止被当作公式执行。
	if !strings.HasPrefix(records[3][6], "'=") {
		t.Fatalf("可疑公式应被转义，实际 %q", records[3][6])
	}
}

func TestWriteCSVEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteCSV(&buf, nil); err != nil {
		t.Fatalf("导出空表失败: %v", err)
	}
	records, err := csv.NewReader(bytes.NewReader(buf.Bytes()[3:])).ReadAll()
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("空数据应只有表头，实际 %d 行", len(records))
	}
}

func TestWriteXLSX(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteXLSX(&buf, sampleEntries()); err != nil {
		t.Fatalf("导出 Excel 失败: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("xlsx 不是合法 zip: %v", err)
	}

	got := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("打开 %s 失败: %v", f.Name, err)
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", f.Name, err)
		}
		got[f.Name] = string(body)
	}

	for _, required := range []string{
		"[Content_Types].xml", "_rels/.rels", "xl/workbook.xml",
		"xl/_rels/workbook.xml.rels", "xl/styles.xml", "xl/worksheets/sheet1.xml",
	} {
		if _, ok := got[required]; !ok {
			t.Fatalf("缺少必需条目 %s", required)
		}
	}

	sheet := got["xl/worksheets/sheet1.xml"]
	if !strings.HasPrefix(sheet, `<?xml version="1.0"`) {
		t.Fatal("工作表应以 XML 声明开头")
	}
	if !strings.Contains(sheet, "时间") || !strings.Contains(sheet, "备注") {
		t.Fatal("表头文字缺失")
	}
	if !strings.Contains(sheet, "1234.56") {
		t.Fatal("金额数值单元格缺失")
	}
	// 金额必须是数值单元格，才能直接在 Excel 里求和。
	if !strings.Contains(sheet, `<c r="C2" s="2"><v>`) {
		t.Fatal("金额应写为带数字格式的数值单元格")
	}
	// 公式只可能出现在 <f> 元素里；所有用户内容都以 inlineStr 文本写入，
	// 即使内容以 = 开头也不会被 Excel 执行。
	if strings.Contains(sheet, "<f>") {
		t.Fatal("导出文件不应包含任何公式元素")
	}
	if !strings.Contains(sheet, `<is><t xml:space="preserve">=cmd|&apos; /C calc&apos;!A0</t></is>`) {
		t.Fatal("可疑内容应作为纯文本单元格写入")
	}
	if !strings.Contains(sheet, "&quot;引号&quot;") {
		t.Fatal("双引号应被 XML 转义")
	}
}

func TestXLSXEscapesControlChars(t *testing.T) {
	entries := []ledger.Entry{{Kind: ledger.KindExpense, AmountCents: 100, OccurredAt: "2026-01-01 00:00:00", Note: "a\x00b\x07c"}}
	var buf bytes.Buffer
	if err := WriteXLSX(&buf, entries); err != nil {
		t.Fatalf("导出失败: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("xlsx 非法: %v", err)
	}
	for _, f := range zr.File {
		if f.Name != "xl/worksheets/sheet1.xml" {
			continue
		}
		rc, _ := f.Open()
		body, _ := io.ReadAll(rc)
		rc.Close()
		if strings.ContainsAny(string(body), "\x00\x07") {
			t.Fatal("XML 非法控制字符必须被移除，否则 Excel 报文件损坏")
		}
	}
}

func TestFormatAmount(t *testing.T) {
	cases := map[int64]string{
		0:         "0.00",
		1:         "0.01",
		99:        "0.99",
		100:       "1.00",
		123456789: "1234567.89",
		-250:      "-2.50",
	}
	for in, want := range cases {
		if got := formatAmount(in); got != want {
			t.Fatalf("formatAmount(%d) = %q，期望 %q", in, got, want)
		}
	}
}
