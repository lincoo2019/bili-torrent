/* bili-torrent 前端逻辑 */
(() => {
  'use strict';

  const $ = (sel) => document.querySelector(sel);
  const $$ = (sel) => Array.from(document.querySelectorAll(sel));

  const state = {
    videos: [],
    types: [],
    sources: [],
    stats: { total: 0, made: 0, unmade: 0 },
    torrents: [],
    indexes: [],
    selected: new Set(),     // 选中的视频 key
    statusFilter: '',
    typeFilter: '',
    sourceFilter: '',
    search: '',
    activeView: 'videos',
    config: null,            // GET /api/config 结果（设置页使用）
  };

  // ---------- 工具 ----------
  async function api(path, opts = {}) {
    const res = await fetch(path, {
      headers: { 'Content-Type': 'application/json' },
      ...opts,
    });
    if (!res.ok) {
      let msg = `HTTP ${res.status}`;
      try { msg = (await res.json()).error || msg; } catch (_) {}
      throw new Error(msg);
    }
    return res.json();
  }

  function fmtSize(n) {
    if (n == null) return '-';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let i = 0, v = n;
    while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
    return v.toFixed(v >= 100 ? 0 : 1) + ' ' + units[i];
  }

  function esc(s) {
    return String(s == null ? '' : s)
      .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  function toast(msg, kind = 'ok') {
    const wrap = $('#toast-wrap');
    const el = document.createElement('div');
    el.className = `toast ${kind}`;
    el.textContent = msg;
    wrap.appendChild(el);
    setTimeout(() => el.remove(), 4000);
  }

  const modal = {
    show(title, body) {
      $('#modal-title').textContent = title;
      $('#modal-body').textContent = body;
      $('#modal-mask').hidden = false;
    },
    hide() { $('#modal-mask').hidden = true; },
  };

  function copyText(text) {
    return navigator.clipboard.writeText(text).catch(() => {
      const ta = document.createElement('textarea');
      ta.value = text;
      document.body.appendChild(ta);
      ta.select();
      document.execCommand('copy');
      ta.remove();
    });
  }

  // ---------- 数据加载 ----------
  async function loadVideos() {
    const params = new URLSearchParams();
    if (state.statusFilter) params.set('status', state.statusFilter);
    if (state.typeFilter) params.set('type', state.typeFilter);
    if (state.sourceFilter) params.set('source', state.sourceFilter);
    if (state.search) params.set('q', state.search);
    const data = await api('/api/videos?' + params.toString());
    state.videos = data.videos || [];
    state.types = data.types || [];
    state.sources = data.sources || [];
    state.stats = data.stats || { total: 0, made: 0, unmade: 0 };
    // 清理失效的选中项
    const keys = new Set(state.videos.map((v) => v.key));
    for (const k of [...state.selected]) if (!keys.has(k)) state.selected.delete(k);
    renderVideos();
    updateFilters();
    updateCounts();
  }

  async function loadTorrents() {
    const data = await api('/api/torrents');
    state.torrents = data.torrents || [];
    renderTorrents();
    updateCounts();
  }

  async function loadIndexes() {
    const data = await api('/api/indexes');
    state.indexes = data.indexes || [];
    renderIndexes();
    updateCounts();
  }

  async function refreshAll() {
    try {
      await Promise.all([loadVideos(), loadTorrents(), loadIndexes()]);
    } catch (e) {
      toast('加载失败: ' + e.message, 'err');
    }
  }

  // ---------- 渲染：视频卡片 ----------
  function renderVideos() {
    const grid = $('#card-grid');
    grid.innerHTML = '';
    $('#videos-empty').hidden = state.videos.length > 0;
    for (const v of state.videos) {
      grid.appendChild(cardEl(v));
    }
  }

  function cardEl(v) {
    const card = document.createElement('div');
    card.className = 'card' + (state.selected.has(v.key) ? ' selected' : '');
    card.dataset.key = v.key;

    const made = !!v.torrent;
    const poster = v.poster_url
      ? `<img src="${esc(v.poster_url)}" loading="lazy" alt="" onerror="this.parentElement.innerHTML=placeholderHTML()"/>`
      : `<div class="placeholder">▶</div>`;

    // 视频类型标签（杜比视界特殊高亮）
    const tags = (v.types || []).map((t) => {
      let cls = 'tag';
      if (t.includes('杜比视界')) cls += ' dv';
      if (/HDR|HLG|Vivid/i.test(t)) cls += ' hdr';
      return `<span class="${cls}">${esc(t)}</span>`;
    }).join('');

    const meta = [];
    if (v.upper_name) meta.push(`<span class="truncate">👤 ${esc(v.upper_name)}</span>`);
    if (v.source) meta.push(`<span class="truncate">📁 ${esc(v.source)}</span>`);
    if (v.season && v.episode) meta.push(`<span>第${v.episode}集</span>`);
    else if (v.page_title) meta.push(`<span class="truncate">${esc(v.page_title)}</span>`);

    const actions = made
      ? `<button class="btn small" data-act="download" data-hash="${esc(v.torrent.info_hash)}">下载种子</button>
         <button class="btn small" data-act="view-index" data-key="${esc(v.key)}">索引</button>`
      : `<button class="btn small primary" data-act="create" data-folder="${esc(v.folder)}">制作种子</button>`;
    const badge = made
      ? '<span class="status-badge made">已制作</span>'
      : '<span class="status-badge unmade">未制作</span>';

    card.innerHTML = `
      <input type="checkbox" class="sel" data-act="select" ${state.selected.has(v.key) ? 'checked' : ''} title="选择" />
      <div class="poster">
        ${poster}
        ${badge}
        <a class="bvid-badge" href="${esc(v.url)}" target="_blank" title="在 B 站打开">${esc(v.bvid)}</a>
      </div>
      <div class="card-body">
        <div class="card-title" title="${esc(v.title)}">${esc(v.title)}</div>
        <div class="card-meta">${meta.map((m) => `<div class="row">${m}</div>`).join('')}</div>
        ${tags ? `<div class="tags">${tags}</div>` : ''}
        <div class="card-foot">
          <span>${fmtSize(v.size)}</span>
          <div class="card-actions">${actions}</div>
        </div>
      </div>`;
    return card;
  }

  // 渲染占位图（img 加载失败时）
  window.placeholderHTML = () => '<div class="placeholder">▶</div>';

  // 类型标签筛选（与状态筛选独立组合）
  function renderTypeChips() {
    const box = $('#type-chips');
    box.innerHTML = '';
    for (const t of state.types) {
      const chip = document.createElement('button');
      chip.className = 'chip type-chip' + (t === state.typeFilter ? ' active' : '');
      chip.textContent = t;
      chip.dataset.type = t;
      chip.title = '筛选 ' + t;
      box.appendChild(chip);
    }
  }

  function updateFilters() {
    const srcSel = $('#filter-source');
    const curSrc = srcSel.value;
    srcSel.innerHTML = '<option value="">全部来源</option>' +
      state.sources.map((s) => `<option value="${esc(s)}">${esc(s)}</option>`).join('');
    srcSel.value = state.sources.includes(curSrc) ? curSrc : '';
    renderTypeChips();
  }

  function updateCounts() {
    $('#st-total').textContent = state.stats.total;
    $('#st-unmade').textContent = state.stats.unmade;
    $('#st-made').textContent = state.stats.made;
    $('#count-videos').textContent = state.stats.total;
    $('#count-torrents').textContent = state.torrents.length;
    $('#count-indexes').textContent = state.indexes.length;
    $('#tab-videos .count, #count-videos').textContent = state.stats.total;
  }

  // ---------- 渲染：种子表格 ----------
  function renderTorrents() {
    const tbody = $('#torrent-tbody');
    tbody.innerHTML = '';
    $('#torrents-empty').hidden = state.torrents.length > 0;
    for (const t of state.torrents) {
      const tr = document.createElement('tr');
      tr.innerHTML = `
        <td title="${esc(t.folder)}">${esc(t.name)}</td>
        <td class="mono hash" data-act="copy-hash" data-hash="${esc(t.info_hash)}" title="点击复制">${esc(t.info_hash.slice(0, 16))}…</td>
        <td>${fmtSize(t.size)}</td>
        <td>${t.file_count}</td>
        <td>${t.index_count}</td>
        <td>${esc((t.created_at || '').replace('T', ' ').slice(0, 19))}</td>
        <td>
          <button class="btn small" data-act="download" data-hash="${esc(t.info_hash)}">下载</button>
          <button class="btn small" data-act="view-index" data-hash="${esc(t.info_hash)}">索引</button>
        </td>`;
      tbody.appendChild(tr);
    }
  }

  // ---------- 渲染：索引列表 ----------
  function renderIndexes() {
    const list = $('#index-list');
    list.innerHTML = '';
    $('#indexes-empty').hidden = state.indexes.length > 0;
    for (const idx of state.indexes) {
      const item = document.createElement('div');
      item.className = 'index-item';
      item.innerHTML = `
        <span class="k">${esc(idx.bvid)}${idx.cid ? '/' + esc(idx.cid) : ''}</span>
        <div class="info">
          <div class="t" title="${esc(idx.title)}">${esc(idx.title)}${idx.page_title ? ' · ' + esc(idx.page_title) : ''}</div>
          <div class="m">${esc(idx.torrent_name)} → ${esc(idx.file_path)} · ${fmtSize(idx.file_size)}</div>
        </div>
        <a class="url" href="${esc(idx.url)}" target="_blank">B站原链接 ↗</a>
        <button class="btn small" data-act="copy-index" data-key="${esc(idx.key)}">复制</button>`;
      list.appendChild(item);
    }
  }

  // ---------- 种子制作 ----------
  async function createTorrents(folders) {
    const data = await api('/api/torrents', {
      method: 'POST',
      body: JSON.stringify({ folders }),
    });
    const tasks = data.tasks || [];
    const ok = tasks.filter((t) => t.task_id);
    const err = tasks.filter((t) => t.error);
    if (err.length) toast(err.map((e) => e.error).join('；'), 'err');
    if (ok.length) {
      toast(`已开始制作 ${ok.length} 个种子`);
      pollTasks();
    }
  }

  async function pollTasks() {
    const bar = $('#task-bar');
    bar.hidden = false;
    let anyRunning = true;
    while (anyRunning) {
      anyRunning = false;
      let tasks = [];
      try {
        tasks = (await api('/api/tasks')).tasks || [];
      } catch (_) { break; }
      bar.innerHTML = '';
      for (const t of tasks) {
        const pct = Math.round((t.progress || 0) * 100);
        const item = document.createElement('div');
        item.className = 'task-item ' + t.status;
        const statusText = t.status === 'done' ? '完成' : t.status === 'error' ? '失败' : '进行中';
        item.innerHTML = `
          <div class="trow"><span>${esc(t.folder_name)}</span><span>${statusText} ${pct}%</span></div>
          <div class="tmsg">${esc(t.message || t.error || '')}</div>
          <div class="progress"><i style="width:${pct}%"></i></div>`;
        bar.appendChild(item);
        if (t.status === 'running') anyRunning = true;
      }
      if (anyRunning) await new Promise((r) => setTimeout(r, 1200));
    }
    setTimeout(() => { bar.hidden = true; }, 2500);
    await refreshAll();
  }

  // ---------- 后台扫描进度 ----------
  let scanPollActive = false;

  function renderScanBar(st) {
    const bar = $('#scan-bar');
    const pct = Math.round((st.progress || 0) * 100);
    let title, sub = '';
    if (st.running) {
      title = st.total > 0 ? `扫描中 ${st.processed}/${st.total}` : '扫描中（正在收集目录）';
      if (st.current_name) sub = st.current_name;
    } else if (st.error) {
      title = '扫描失败';
      sub = st.error;
    } else if (st.finished_at) {
      title = `扫描完成：${st.folders} 个视频文件夹 · ${st.videos} 个视频`;
    } else {
      title = '尚未扫描';
    }
    bar.innerHTML = `
      <div class="trow">
        <span class="truncate">${esc(title)}${sub ? ' · ' + esc(sub) : ''}</span>
        <span>${st.running ? pct + '%' : ''}</span>
      </div>
      ${st.running ? `<div class="progress"><i style="width:${pct}%"></i></div>` : ''}`;
  }

  // 轮询 /api/scan 显示扫描进度，扫描结束后刷新数据。使用标记防止重复轮询。
  async function pollScan() {
    if (scanPollActive) return;
    scanPollActive = true;
    try {
      const bar = $('#scan-bar');
      bar.hidden = false;
      let running = true;
      while (running) {
        let st;
        try { st = await api('/api/scan'); } catch (_) { break; }
        running = st.running;
        renderScanBar(st);
        if (running) await new Promise((r) => setTimeout(r, 800));
      }
      setTimeout(() => { bar.hidden = true; }, 2500);
      await refreshAll();
    } finally {
      scanPollActive = false;
    }
  }

  // ---------- 事件绑定 ----------
  function bindEvents() {
    // Tab 切换
    $('#tabs').addEventListener('click', (e) => {
      const btn = e.target.closest('.tab');
      if (!btn) return;
      state.activeView = btn.dataset.view;
      $$('.tab').forEach((t) => t.classList.toggle('active', t === btn));
      ['videos', 'torrents', 'indexes', 'settings'].forEach((v) => {
        $('#view-' + v).hidden = v !== state.activeView;
      });
      if (state.activeView === 'torrents') loadTorrents();
      if (state.activeView === 'indexes') loadIndexes();
      if (state.activeView === 'settings') loadConfig();
    });

    // 状态筛选（全部/未制作/已制作）
    $('#status-filter').addEventListener('click', (e) => {
      const chip = e.target.closest('.chip[data-status]');
      if (!chip) return;
      state.statusFilter = chip.dataset.status;
      $$('#status-filter .chip[data-status]').forEach((c) => c.classList.toggle('active', c === chip));
      loadVideos();
    });

    // 类型标签筛选（与状态筛选组合）
    $('#type-chips').addEventListener('click', (e) => {
      const chip = e.target.closest('.type-chip');
      if (!chip) return;
      state.typeFilter = chip.dataset.type === state.typeFilter ? '' : chip.dataset.type;
      renderTypeChips();
      loadVideos();
    });

    // 来源 / 搜索（防抖）
    $('#filter-source').addEventListener('change', (e) => { state.sourceFilter = e.target.value; loadVideos(); });
    let debounce;
    $('#search').addEventListener('input', (e) => {
      clearTimeout(debounce);
      debounce = setTimeout(() => { state.search = e.target.value.trim(); loadVideos(); }, 250);
    });

    // 重新扫描（后台执行，进度条显示）
    $('#btn-rescan').addEventListener('click', async () => {
      try {
        const r = await api('/api/rescan', { method: 'POST' });
        if (r.already_running) {
          toast('扫描已在运行中', 'err');
        } else {
          toast('已开始后台扫描');
        }
        pollScan();
      } catch (err) {
        toast('启动扫描失败: ' + err.message, 'err');
      }
    });

    // ---------- 设置：扫描文件夹 ----------
    $('#btn-add-root').addEventListener('click', addRoot);
    $('#root-input').addEventListener('keydown', (e) => { if (e.key === 'Enter') addRoot(); });
    $('#roots-list').addEventListener('click', (e) => {
      const btn = e.target.closest('[data-remove]');
      if (!btn) return;
      const roots = (state.config && state.config.roots || []).slice();
      roots.splice(Number(btn.dataset.remove), 1);
      saveRoots(roots);
    });

    // 卡片操作（事件委托）
    $('#card-grid').addEventListener('click', async (e) => {
      const actEl = e.target.closest('[data-act]');
      if (!actEl) return;
      const act = actEl.dataset.act;
      const card = actEl.closest('.card');
      if (act === 'select') {
        const key = card.dataset.key;
        if (actEl.checked) state.selected.add(key); else state.selected.delete(key);
        card.classList.toggle('selected', actEl.checked);
        updateBatchBar();
        return;
      }
      if (act === 'create') {
        await createTorrents([actEl.dataset.folder]);
        return;
      }
      if (act === 'download') {
        window.open('/api/torrents/' + encodeURIComponent(actEl.dataset.hash) + '/download', '_blank');
        return;
      }
      if (act === 'view-index') {
        if (actEl.dataset.hash) {
          const d = await api('/api/torrents/' + encodeURIComponent(actEl.dataset.hash) + '/indexes');
          modal.show('种子索引', JSON.stringify(d, null, 2));
        } else {
          const idx = await api('/api/indexes/' + encodeURIComponent(actEl.dataset.key));
          modal.show('索引 ' + idx.bvid, JSON.stringify(idx, null, 2));
        }
      }
    });

    // 全选 / 批量制作
    $('#select-all').addEventListener('change', (e) => {
      const keys = state.videos.map((v) => v.key);
      if (e.target.checked) keys.forEach((k) => state.selected.add(k));
      else keys.forEach((k) => state.selected.delete(k));
      renderVideos();
      updateBatchBar();
    });

    $('#btn-batch-create').addEventListener('click', async () => {
      const folders = [...new Set(state.videos.filter((v) => state.selected.has(v.key)).map((v) => v.folder))];
      await createTorrents(folders);
      state.selected.clear();
      renderVideos();
      updateBatchBar();
    });

    // 种子表格 / 索引列表操作
    $('#torrent-table').addEventListener('click', async (e) => {
      const el = e.target.closest('[data-act]');
      if (!el) return;
      if (el.dataset.act === 'download') {
        window.open('/api/torrents/' + encodeURIComponent(el.dataset.hash) + '/download', '_blank');
      } else if (el.dataset.act === 'copy-hash') {
        await copyText(el.dataset.hash);
        toast('已复制 ' + el.dataset.hash);
      } else if (el.dataset.act === 'view-index') {
        const d = await api('/api/torrents/' + encodeURIComponent(el.dataset.hash) + '/indexes');
        modal.show('种子索引', JSON.stringify(d, null, 2));
      }
    });

    $('#index-list').addEventListener('click', async (e) => {
      const el = e.target.closest('[data-act]');
      if (!el) return;
      if (el.dataset.act === 'copy-index') {
        const idx = await api('/api/indexes/' + encodeURIComponent(el.dataset.key));
        await copyText(JSON.stringify(idx, null, 2));
        toast('索引已复制');
      }
    });

    $('#btn-copy-index').addEventListener('click', async () => {
      const data = await api('/index.json');
      await copyText(JSON.stringify(data, null, 2));
      toast('索引清单已复制');
    });

    // 弹窗关闭
    $('#modal-close').addEventListener('click', modal.hide);
    $('#modal-mask').addEventListener('click', (e) => { if (e.target.id === 'modal-mask') modal.hide(); });

    // 快捷键：Esc 关闭弹窗
    document.addEventListener('keydown', (e) => { if (e.key === 'Escape') modal.hide(); });
  }

  function updateBatchBar() {
    const count = state.selected.size;
    $('#batch-count').textContent = `已选 ${count}`;
    $('#batch-bar').hidden = count === 0;
    $('#btn-batch-create').disabled = count === 0;
  }

  // ---------- 设置：扫描文件夹 ----------
  async function loadConfig() {
    try {
      state.config = await api('/api/config');
    } catch (e) {
      toast('加载配置失败: ' + e.message, 'err');
      return;
    }
    const cfg = state.config;
    const list = $('#roots-list');
    list.innerHTML = '';
    if (!cfg.roots || cfg.roots.length === 0) {
      list.innerHTML = '<div class="root-row empty-root">尚未配置扫描文件夹</div>';
    }
    (cfg.roots || []).forEach((root, i) => {
      const row = document.createElement('div');
      row.className = 'root-row';
      row.innerHTML = `
        <span class="mono" title="${esc(root)}">${esc(root)}</span>
        <button class="btn small danger" data-remove="${i}">移除</button>`;
      list.appendChild(row);
    });

    $('#other-config').innerHTML = `
      <div class="kv-row"><span>数据目录</span><code>${esc(cfg.data_dir)}</code></div>
      <div class="kv-row"><span>种子输出目录</span><code>${esc(cfg.torrent_dir)}</code></div>
      <div class="kv-row"><span>Tracker</span><code>${esc((cfg.trackers || []).join(', ') || '（未配置）')}</code></div>
      <div class="kv-row"><span>私有种子</span><code>${cfg.private ? '是' : '否'}</code></div>
      <div class="kv-row"><span>ffprobe 深度探测</span><code>${cfg.probe_videos ? '开启' : '关闭'}</code></div>
      <div class="kv-row"><span>自动扫描间隔</span><code>${cfg.scan_interval || 0} 秒</code></div>`;
  }

  async function addRoot() {
    const input = $('#root-input');
    const path = input.value.trim();
    if (!path) { toast('请输入文件夹路径', 'err'); return; }
    const roots = (state.config && state.config.roots || []).slice();
    roots.push(path);
    await saveRoots(roots);
    if (state.config && state.config.roots.includes(path)) input.value = '';
  }

  async function saveRoots(roots) {
    try {
      const res = await api('/api/config/roots', {
        method: 'PUT',
        body: JSON.stringify({ roots }),
      });
      if (res.already_running) {
        toast(`已保存 ${res.roots.length} 个扫描文件夹，扫描已在运行中`);
      } else {
        toast(`已保存 ${res.roots.length} 个扫描文件夹，后台扫描中…`);
      }
      state.config = { ...(state.config || {}), roots: res.roots };
      await loadConfig();
      pollScan();
    } catch (e) {
      toast(e.message, 'err');
    }
  }

  // ---------- 启动 ----------
  async function init() {
    bindEvents();
    try {
      await refreshAll();
    } catch (e) {
      toast('初始化失败: ' + e.message, 'err');
    }
    // 若已有扫描在运行（如启动时的初始扫描 / 其它客户端触发），显示进度
    try {
      const st = await api('/api/scan');
      if (st.running) pollScan();
    } catch (_) {}
    // 定时刷新任务状态与扫描状态
    setInterval(async () => {
      try {
        const st = await api('/api/scan');
        if (st.running) pollScan();
      } catch (_) {}
    }, 15000);
  }

  document.addEventListener('DOMContentLoaded', init);
})();
