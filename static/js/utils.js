/**
 * 投放管理系统 — 公共工具函数
 * 所有页面共享，避免重复定义
 */

// ── 时间格式化 ──
function fmtDate(d) {
  if (!d) return '-';
  const t = new Date(d), pad = n => String(n).padStart(2, '0');
  return `${t.getFullYear()}-${pad(t.getMonth() + 1)}-${pad(t.getDate())} ${pad(t.getHours())}:${pad(t.getMinutes())}:${pad(t.getSeconds())}`;
}

// ── 数字千分位格式化 ──
function fmtNum(n) {
  if (n == null || n === '') return '-';
  return Number(n).toLocaleString('zh-CN');
}

// ── 状态文本映射 ──
function statusText(s) {
  const map = {
    init: '初始化', pending: '待支付', review: '审核中', submitting: '提交中',
    active: '加热中', running: '执行中', failed: '失败', closed: '已关闭',
    completed: '已完成', cancelled: '已取消', error: '异常',
    detected: '待审核', confirmed: '已确认', rejected: '已拒绝', queued: '排队中',
  };
  return map[s] || s || '-';
}

// ── 分页号码列表生成 ──
function pageList(currentPage, totalPages) {
  const tp = totalPages, cp = currentPage, pages = [];
  if (tp <= 7) { for (let i = 1; i <= tp; i++) pages.push(i); return pages; }
  pages.push(1);
  if (cp > 3) pages.push('...');
  for (let i = Math.max(2, cp - 1); i <= Math.min(tp - 1, cp + 1); i++) pages.push(i);
  if (cp < tp - 2) pages.push('...');
  pages.push(tp);
  return pages;
}

// ── 平台名称映射 ──
function platformName(p) {
  const map = { weixin: '微信', douyin: '抖音' };
  return map[p] || p || '-';
}

// ── Alpine 全局 Toast Store ──
document.addEventListener('alpine:init', () => {
  Alpine.store('toast', {
    msg: '',
    show(m) {
      this.msg = m;
      setTimeout(() => this.msg = '', 3000);
    }
  });
});
