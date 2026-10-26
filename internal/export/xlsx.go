package export

import (
	"archive/zip"
	"fmt"
	"io"
	"strconv"
	"strings"

	"bookkeeping/internal/ledger"
)

// WriteXLSX 生成一个最小的 .xlsx 文件（Office Open XML）。
// 只依赖标准库：文本用 inlineStr 写入，免去 sharedStrings 表。
func WriteXLSX(w io.Writer, entries []ledger.Entry) error {
	zw := zip.NewWriter(w)

	files := []struct {
		name string
		body string
	}{
		{"[Content_Types].xml", contentTypes},
		{"_rels/.rels", rels},
		{"xl/workbook.xml", workbook},
		{"xl/_rels/workbook.xml.rels", workbookRels},
		{"xl/styles.xml", styles},
		{"xl/worksheets/sheet1.xml", buildSheet(entries)},
	}
	for _, f := range files {
		fw, err := zw.Create(f.name)
		if err != nil {
			return fmt.Errorf("创建 xlsx 条目失败: %w", err)
		}
		if _, err := io.WriteString(fw, f.body); err != nil {
			return fmt.Errorf("写入 xlsx 条目失败: %w", err)
		}
	}
	return zw.Close()
}

func buildSheet(entries []ledger.Entry) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	sb.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	// 冻结首行，滚动时表头始终可见。
	sb.WriteString(`<sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>`)

	sb.WriteString(`<cols>`)
	for i, width := range []float64{20, 8, 14, 14, 14, 14, 40} {
		sb.WriteString(`<col min="` + strconv.Itoa(i+1) + `" max="` + strconv.Itoa(i+1) + `" width="` + strconv.FormatFloat(width, 'f', 1, 64) + `" customWidth="1"/>`)
	}
	sb.WriteString(`</cols>`)

	sb.WriteString(`<sheetData>`)

	sb.WriteString(`<row r="1">`)
	for i, h := range headers {
		sb.WriteString(`<c r="` + cellRef(i, 1) + `" t="inlineStr" s="1"><is><t>` + xmlEsc(h) + `</t></is></c>`)
	}
	sb.WriteString(`</row>`)

	for rowIdx, e := range entries {
		r := rowIdx + 2
		incomePerson, expensePerson := splitPerson(e)
		sb.WriteString(`<row r="` + strconv.Itoa(r) + `">`)
		sb.WriteString(textCell(0, r, e.OccurredAt, 0))
		sb.WriteString(textCell(1, r, kindLabel(e.Kind), 0))
		// 金额写数值单元格，配两位小数格式，导出后可以直接求和。
		sb.WriteString(`<c r="` + cellRef(2, r) + `" s="2"><v>` + formatAmount(e.AmountCents) + `</v></c>`)
		sb.WriteString(textCell(3, r, incomePerson, 0))
		sb.WriteString(textCell(4, r, expensePerson, 0))
		sb.WriteString(textCell(5, r, e.Category, 0))
		sb.WriteString(textCell(6, r, e.Note, 0))
		sb.WriteString(`</row>`)
	}

	sb.WriteString(`</sheetData>`)
	sb.WriteString(`<autoFilter ref="A1:G` + strconv.Itoa(len(entries)+1) + `"/>`)
	sb.WriteString(`</worksheet>`)
	return sb.String()
}

func textCell(col, row int, value string, style int) string {
	if value == "" {
		return ""
	}
	styleAttr := ""
	if style != 0 {
		styleAttr = ` s="` + strconv.Itoa(style) + `"`
	}
	return `<c r="` + cellRef(col, row) + `" t="inlineStr"` + styleAttr + `><is><t xml:space="preserve">` + xmlEsc(value) + `</t></is></c>`
}

func cellRef(col, row int) string {
	return string(rune('A'+col)) + strconv.Itoa(row)
}

func xmlEsc(s string) string {
	var sb strings.Builder
	sb.Grow(len(s) + 16)
	for _, r := range s {
		switch r {
		case '&':
			sb.WriteString("&amp;")
		case '<':
			sb.WriteString("&lt;")
		case '>':
			sb.WriteString("&gt;")
		case '"':
			sb.WriteString("&quot;")
		case '\'':
			sb.WriteString("&apos;")
		default:
			// XML 1.0 不接受的控制字符直接丢弃，否则 Excel 会判定文件损坏。
			if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
				continue
			}
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

const contentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>
</Types>`

const rels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>`

const workbook = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<sheets><sheet name="记账明细" sheetId="1" r:id="rId1"/></sheets>
</workbook>`

const workbookRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
</Relationships>`

// 样式：0 默认；1 表头（加粗 + 浅灰底）；2 金额（两位小数、千分位）。
const styles = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<numFmts count="1"><numFmt numFmtId="164" formatCode="#,##0.00"/></numFmts>
<fonts count="2">
<font><sz val="11"/><name val="微软雅黑"/></font>
<font><b/><sz val="11"/><name val="微软雅黑"/></font>
</fonts>
<fills count="3">
<fill><patternFill patternType="none"/></fill>
<fill><patternFill patternType="gray125"/></fill>
<fill><patternFill patternType="solid"><fgColor rgb="FFEFEFEF"/><bgColor indexed="64"/></patternFill></fill>
</fills>
<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>
<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>
<cellXfs count="3">
<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>
<xf numFmtId="0" fontId="1" fillId="2" borderId="0" xfId="0" applyFont="1" applyFill="1"/>
<xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
</cellXfs>
<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>
</styleSheet>`
