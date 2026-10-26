'use strict';

const PAGE_SIZE = 50;

const state = {
  offset: 0,
  total: 0,
  editingId: null,
  meta: { persons: [], categories: [] },
};

const $ = (id) => document.getElementById(id);

const els = {
  sumIncome: $('sumIncome'),
  sumExpense: $('sumExpense'),
  sumBalance: $('sumBalance'),
  sumCount: $('sumCount'),
  fFrom: $('fFrom'),
  fTo: $('fTo'),
  fKind: $('fKind'),
  fPerson: $('fPerson'),
  fCategory: $('fCategory'),
  fKeyword: $('fKeyword'),
  body: $('entryBody'),
  emptyHint: $('emptyHint'),
  pageInfo: $('pageInfo'),
  btnPrev: $('btnPrev'),
  btnNext: $('btnNext'),
  overlay: $('overlay'),
  form: $('entryForm'),
  dialogTitle: $('dialogTitle'),
  amount: $('amount'),
  occurredAt: $('occurredAt'),
  person: $('person'),
  personLabel: $('personLabel'),
  category: $('category'),
  note: $('note'),
  formError: $('formError'),
  btnSave: $('btnSave'),
  personOptions: $('personOptions'),
  categoryOptions: $('categoryOptions'),
  toast: $('toast'),
};

/* ---------- 工具函数 ---------- */

function formatCents(cents) {
  const value = Number(cents) || 0;
  const negative = value < 0;
  const abs = Math.abs(value);
  const yuan = Math.floor(abs / 100).toLocaleString('zh-CN');
  const fen = String(abs % 100).padStart(2, '0');
  return (negative ? '-¥' : '¥') + yuan + '.' + fen;
}

function toNumber(value) {
  const n = Number(value);
  return Number.isFinite(n) ? n : 0;
}

function currentFilters() {
  const params = new URLSearchParams();
  const pairs = {
    from: els.fFrom.value,
    to: els.fTo.value,
    kind: els.fKind.value,
    person: els.fPerson.value,
    category: els.fCategory.value,
    keyword: els.fKeyword.value.trim(),
  };
  for (const [key, value] of Object.entries(pairs)) {
    if (value) params.set(key, value);
  }
  return params;
}

let toastTimer = null;
function toast(message, isError = false) {
  els.toast.textContent = message;
  els.toast.style.background = isError ? '#b91c1c' : '#1f2937';
  els.toast.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { els.toast.hidden = true; }, 2400);
}

async function api(path, options = {}) {
  const response = await fetch(path, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  });
  if (response.status === 204) return null;
  const text = await response.text();
  let data = null;
  if (text) {
    try { data = JSON.parse(text); } catch { data = null; }
  }
  if (!response.ok) {
    const message = (data && data.error) || `请求失败（${response.status}）`;
    throw new Error(message);
  }
  return data;
}

/* ---------- 渲染 ---------- */

function renderSummary(summary) {
  els.sumIncome.textContent = formatCents(summary.incomeCents);
  els.sumExpense.textContent = formatCents(summary.expenseCents);
  els.sumBalance.textContent = formatCents(summary.balanceCents);
  els.sumCount.textContent = String(summary.count ?? 0);
}

function renderRows(items) {
  els.body.textContent = '';

  if (!items.length) {
    els.emptyHint.hidden = false;
    return;
  }
  els.emptyHint.hidden = true;

  const frag = document.createDocumentFragment();
  for (const item of items) {
    const tr = document.createElement('tr');

    const tdTime = document.createElement('td');
    tdTime.textContent = item.occurredAt;
    tr.appendChild(tdTime);

    const tdKind = document.createElement('td');
    const tag = document.createElement('span');
    tag.className = 'tag ' + item.kind;
    tag.textContent = item.kind === 'income' ? '收入' : '支出';
    tdKind.appendChild(tag);
    tr.appendChild(tdKind);

    const tdAmount = document.createElement('td');
    tdAmount.className = 'num amount ' + item.kind;
    tdAmount.textContent = formatCents(toNumber(item.amountCents));
    tr.appendChild(tdAmount);

    const tdPerson = document.createElement('td');
    tdPerson.textContent = item.person || '—';
    tr.appendChild(tdPerson);

    const tdCategory = document.createElement('td');
    tdCategory.textContent = item.category || '—';
    tr.appendChild(tdCategory);

    const tdNote = document.createElement('td');
    tdNote.className = 'note';
    tdNote.textContent = item.note || '—';
    tr.appendChild(tdNote);

    const tdOps = document.createElement('td');
    tdOps.className = 'ops';
    const editBtn = document.createElement('button');
    editBtn.className = 'link-btn';
    editBtn.type = 'button';
    editBtn.textContent = '编辑';
    editBtn.addEventListener('click', () => openForm(item));
    const delBtn = document.createElement('button');
    delBtn.className = 'link-btn danger';
    delBtn.type = 'button';
    delBtn.textContent = '删除';
    delBtn.addEventListener('click', () => removeEntry(item));
    tdOps.append(editBtn, delBtn);
    tr.appendChild(tdOps);

    frag.appendChild(tr);
  }
  els.body.appendChild(frag);
}

function renderPager() {
  const start = state.total === 0 ? 0 : state.offset + 1;
  const end = Math.min(state.offset + PAGE_SIZE, state.total);
  els.pageInfo.textContent = `共 ${state.total} 条，当前 ${start}–${end}`;
  els.btnPrev.disabled = state.offset <= 0;
  els.btnNext.disabled = state.offset + PAGE_SIZE >= state.total;
}

function renderMeta() {
  fillSelect(els.fPerson, state.meta.persons, '全部');
  fillSelect(els.fCategory, state.meta.categories, '全部');
  fillDatalist(els.personOptions, state.meta.persons);
  fillDatalist(els.categoryOptions, state.meta.categories);
}

function fillSelect(select, values, placeholder) {
  const previous = select.value;
  select.textContent = '';
  const first = document.createElement('option');
  first.value = '';
  first.textContent = placeholder;
  select.appendChild(first);
  for (const value of values) {
    const option = document.createElement('option');
    option.value = value;
    option.textContent = value;
    select.appendChild(option);
  }
  select.value = values.includes(previous) ? previous : '';
}

function fillDatalist(datalist, values) {
  datalist.textContent = '';
  for (const value of values) {
    const option = document.createElement('option');
    option.value = value;
    datalist.appendChild(option);
  }
}

/* ---------- 数据加载 ---------- */

async function refresh() {
  const params = currentFilters();
  const listParams = new URLSearchParams(params);
  listParams.set('limit', String(PAGE_SIZE));
  listParams.set('offset', String(state.offset));

  try {
    const [list, summary] = await Promise.all([
      api('/api/entries?' + listParams.toString()),
      api('/api/summary?' + params.toString()),
    ]);
    state.total = toNumber(list.total);
    renderRows(list.items || []);
    renderSummary(summary);
    renderPager();
  } catch (err) {
    toast(err.message, true);
  }
}

async function loadMeta() {
  try {
    state.meta = await api('/api/meta');
    renderMeta();
  } catch (err) {
    toast(err.message, true);
  }
}

/* ---------- 表单 ---------- */

function localNow() {
  const now = new Date();
  const pad = (n) => String(n).padStart(2, '0');
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}T${pad(now.getHours())}:${pad(now.getMinutes())}`;
}

function currentKind() {
  const checked = els.form.querySelector('input[name="kind"]:checked');
  return checked ? checked.value : 'expense';
}

function syncPersonLabel() {
  els.personLabel.textContent = currentKind() === 'income' ? '收入人' : '支出人';
}

function openForm(item = null) {
  state.editingId = item ? item.id : null;
  els.formError.hidden = true;
  els.dialogTitle.textContent = item ? '编辑记录' : '记一笔';

  const kind = item ? item.kind : 'expense';
  const radio = els.form.querySelector(`input[name="kind"][value="${kind}"]`);
  if (radio) radio.checked = true;

  els.amount.value = item ? (toNumber(item.amountCents) / 100).toFixed(2) : '';
  els.occurredAt.value = item ? item.occurredAt.replace(' ', 'T').slice(0, 16) : localNow();
  els.person.value = item ? item.person : '';
  els.category.value = item ? item.category : '';
  els.note.value = item ? item.note : '';

  syncPersonLabel();
  els.overlay.hidden = false;
  els.amount.focus();
}

function closeForm() {
  els.overlay.hidden = true;
  state.editingId = null;
  els.form.reset();
}

function parseAmountToCents(raw) {
  const cleaned = String(raw).trim().replace(/[,，\s]/g, '');
  if (!/^\d+(\.\d{1,2})?$/.test(cleaned)) {
    throw new Error('金额格式不正确，最多两位小数');
  }
  const cents = Math.round(Number(cleaned) * 100);
  if (!Number.isFinite(cents) || cents <= 0) {
    throw new Error('金额必须大于 0');
  }
  return cents;
}

async function submitForm(event) {
  event.preventDefault();
  els.formError.hidden = true;

  let cents;
  try {
    cents = parseAmountToCents(els.amount.value);
  } catch (err) {
    showFormError(err.message);
    return;
  }

  if (!els.occurredAt.value) {
    showFormError('请选择时间');
    return;
  }

  const payload = {
    kind: currentKind(),
    amountCents: cents,
    occurredAt: els.occurredAt.value,
    person: els.person.value.trim(),
    category: els.category.value.trim(),
    note: els.note.value.trim(),
  };

  els.btnSave.disabled = true;
  try {
    if (state.editingId) {
      await api('/api/entries/' + encodeURIComponent(state.editingId), {
        method: 'PUT',
        body: JSON.stringify(payload),
      });
      toast('已保存修改');
    } else {
      await api('/api/entries', {
        method: 'POST',
        body: JSON.stringify(payload),
      });
      toast('已记一笔');
    }
    closeForm();
    await loadMeta();
    await refresh();
  } catch (err) {
    showFormError(err.message);
  } finally {
    els.btnSave.disabled = false;
  }
}

function showFormError(message) {
  els.formError.textContent = message;
  els.formError.hidden = false;
}

async function removeEntry(item) {
  const label = item.note ? `「${item.note}」` : formatCents(toNumber(item.amountCents));
  if (!window.confirm(`确定删除 ${item.occurredAt} 的 ${label} 吗？删除后不可恢复。`)) {
    return;
  }
  try {
    await api('/api/entries/' + encodeURIComponent(item.id), { method: 'DELETE' });
    if (state.offset > 0 && state.offset >= state.total - 1) {
      state.offset = Math.max(0, state.offset - PAGE_SIZE);
    }
    toast('已删除');
    await loadMeta();
    await refresh();
  } catch (err) {
    toast(err.message, true);
  }
}

function exportFile(format) {
  const params = currentFilters();
  params.set('format', format);
  // 用隐藏链接触发下载，避免整页跳转。
  const a = document.createElement('a');
  a.href = '/api/export?' + params.toString();
  a.rel = 'noopener';
  document.body.appendChild(a);
  a.click();
  a.remove();
}

/* ---------- 事件绑定 ---------- */

function bind() {
  $('btnNew').addEventListener('click', () => openForm());
  $('btnCancel').addEventListener('click', closeForm);
  $('btnExportCsv').addEventListener('click', () => exportFile('csv'));
  $('btnExportXlsx').addEventListener('click', () => exportFile('xlsx'));

  els.form.addEventListener('submit', submitForm);
  els.form.querySelectorAll('input[name="kind"]').forEach((input) => {
    input.addEventListener('change', syncPersonLabel);
  });

  for (const el of [els.fFrom, els.fTo, els.fKind, els.fPerson, els.fCategory]) {
    el.addEventListener('change', () => { state.offset = 0; refresh(); });
  }

  let keywordTimer = null;
  els.fKeyword.addEventListener('input', () => {
    clearTimeout(keywordTimer);
    keywordTimer = setTimeout(() => { state.offset = 0; refresh(); }, 300);
  });

  $('btnReset').addEventListener('click', () => {
    els.fFrom.value = '';
    els.fTo.value = '';
    els.fKind.value = '';
    els.fPerson.value = '';
    els.fCategory.value = '';
    els.fKeyword.value = '';
    state.offset = 0;
    refresh();
  });

  els.btnPrev.addEventListener('click', () => {
    state.offset = Math.max(0, state.offset - PAGE_SIZE);
    refresh();
  });
  els.btnNext.addEventListener('click', () => {
    if (state.offset + PAGE_SIZE >= state.total) return;
    state.offset += PAGE_SIZE;
    refresh();
  });

  els.overlay.addEventListener('mousedown', (event) => {
    // 点击面板外的空白处也关闭，但要避免在面板内拖选文字时误关。
    if (event.target === els.overlay) closeForm();
  });

  document.addEventListener('keydown', (event) => {
    if (event.key === 'Escape' && !els.overlay.hidden) closeForm();
  });
}

async function init() {
  bind();
  await loadMeta();
  await refresh();
}

init();
