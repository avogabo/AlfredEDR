async function apiGet(path) {
  const r = await fetch(path, { headers: { 'Accept': 'application/json' } });
  if (!r.ok) throw new Error(await r.text());
  return await r.json();
}

async function apiPostJson(path, body) {
  const r = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'Accept': 'application/json' },
    body: JSON.stringify(body || {})
  });
  if (!r.ok) throw new Error(await r.text());
  return await r.json();
}

async function apiPutJson(path, body) {
  const r = await fetch(path, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', 'Accept': 'application/json' },
    body: JSON.stringify(body || {})
  });
  if (!r.ok) throw new Error(await r.text());
  return await r.json();
}

function el(tag, attrs = {}, children = []) {
  const n = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === 'class') n.className = v;
    else if (k === 'text') n.textContent = v;
    else n.setAttribute(k, v);
  }
  for (const c of children) n.appendChild(c);
  return n;
}

function fmtTime(ts) {
  if (!ts) return '';
  return String(ts).replace('T',' ').replace('Z','');
}

function _num(v) {
  const s = String(v == null ? '' : v).trim();
  const n = Number(s);
  if (Number.isFinite(n)) return n;
  const m = s.match(/\d+/);
  return m ? Number(m[0]) : 0;
}

function updateDLStreamsHint() {
  const connEl = document.getElementById('setDL_CONN');
  const preEl = document.getElementById('setDL_PREFETCH');
  const hint = document.getElementById('dlStreamsHint');
  if (!connEl || !preEl || !hint) return;
  const conn = _num(connEl.value || connEl.placeholder || 0);
  const pre = _num(preEl.value || preEl.placeholder || 0);
  const streams = (conn > 0 && pre > 0) ? Math.max(1, Math.floor(conn / pre)) : 0;
  hint.textContent = `Streams simultáneos estimados: ~${streams}`;
}

async function refreshBackupsList() {
  const sel = document.getElementById('setBackupsRestoreName');
  const st = document.getElementById('setBackupsStatus');
  if (!sel) return;
  try {
    const r = await apiGet('/api/v1/backups');
    const items = (r && r.items) ? r.items : [];
    sel.innerHTML = '';
    for (const it of items) {
      const o = document.createElement('option');
      o.value = it.name;
      const cfgTag = it.config_present ? ' +config' : '';
      o.textContent = `${it.name}${cfgTag} (${it.time || ''})`;
      sel.appendChild(o);
    }
    if (st) st.textContent = `Backups: ${items.length}`;
  } catch (e) {
    if (st) st.textContent = `Error backups: ${e}`;
  }
}

// --- Manual UI (DB-backed) ---
// NOTE: appended here for now; can be refactored into separate file later.
async function refreshManual() {
  const statusId = 'manStatus';
  const listId = 'manList';
  const crumbsId = 'manCrumbs';

  setStatus(statusId, 'Cargando...');

  // crumbs
  try {
    const path = await apiGet(`/api/v1/manual/path?dir_id=${encodeURIComponent(manualDirId)}`);
    const box = document.getElementById(crumbsId);
    if (box) {
      box.innerHTML = '';
      for (let i = 0; i < path.length; i++) {
        const d = path[i];
        const b = el('button', { class: 'crumb', type: 'button', text: d.name });
        b.onclick = () => { manualDirId = d.id; refreshManual().catch(err => setStatus(statusId, String(err))); };
        box.appendChild(b);
        if (i !== path.length - 1) box.appendChild(el('span', { class: 'crumbSep', text: '›' }));
      }
    }
  } catch (e) {
    // ignore
  }

  const dirs = await apiGet(`/api/v1/manual/dirs?parent_id=${encodeURIComponent(manualDirId)}`);
  const items = await apiGet(`/api/v1/manual/items?dir_id=${encodeURIComponent(manualDirId)}`);

  const list = document.getElementById(listId);
  list.innerHTML = '';

  // folders
  for (const d of (dirs || [])) {
    const row = el('div', { class: 'listRow' });
    row.appendChild(el('div', { class: 'name' }, [
      el('div', { class: 'icon', text: 'DIR' }),
      el('div', { text: d.name })
    ]));
    row.appendChild(el('div', { class: 'mono muted', text: '' }));
    row.appendChild(el('div', { class: 'mono muted', text: '' }));

    const cell = el('div');
    const btn = el('button', { class: 'btn', type: 'button', text: '⋮' });
    btn.style.padding = '6px 10px';
    btn.onclick = async (ev) => {
      ev.stopPropagation();
      const choice = prompt('Acción carpeta:\n1 = Renombrar\n2 = Mover (cambia parent_id)\n3 = Borrar (si vacía)\n\nEscribe 1,2,3');
      if (!choice) return;
      if (String(choice).trim() === '1') {
        const name = prompt('Nuevo nombre', d.name);
        if (!name) return;
        await apiPutJson(`/api/v1/manual/dirs/${encodeURIComponent(d.id)}`, { name });
        await refreshManual();
      } else if (String(choice).trim() === '2') {
        const parent_id = prompt('Nuevo parent_id (dir id). root=raíz', d.parent_id || 'root');
        if (!parent_id) return;
        await apiPutJson(`/api/v1/manual/dirs/${encodeURIComponent(d.id)}`, { parent_id });
        await refreshManual();
      } else if (String(choice).trim() === '3') {
        const ok = confirm('¿Borrar carpeta? Solo si está vacía.');
        if (!ok) return;
        const rr = await fetch(`/api/v1/manual/dirs/${encodeURIComponent(d.id)}`, { method: 'DELETE' });
        if (!rr.ok) throw new Error(await rr.text());
        await refreshManual();
      }
    };
    cell.appendChild(btn);
    row.appendChild(cell);

    row.onclick = () => { manualDirId = d.id; refreshManual().catch(err => setStatus(statusId, String(err))); };
    list.appendChild(row);
  }

  // items
  for (const it of (items || [])) {
    const row = el('div', { class: 'listRow' });
    row.appendChild(el('div', { class: 'name' }, [
      el('div', { class: 'icon', text: 'FILE' }),
      el('div', { class: 'mono', text: it.label || it.filename || '(item)' })
    ]));
    row.appendChild(el('div', { class: 'mono muted', text: fmtSize(it.bytes || 0) }));
    row.appendChild(el('div', { class: 'mono muted', text: '' }));

    const cell = el('div');
    const btn = el('button', { class: 'btn', type: 'button', text: '⋮' });
    btn.style.padding = '6px 10px';
    btn.onclick = async (ev) => {
      ev.stopPropagation();
      const choice = prompt('Acción:\n1 = Renombrar (label)\n2 = Mover (dir_id)\n3 = Quitar de este montaje\n4 = Borrar global (BD)\n5 = Borrado completo (.trash)\n\nEscribe 1..5');
      if (!choice) return;
      const c = String(choice).trim();
      if (c === '1') {
        const label = prompt('Nuevo nombre', it.label || it.filename || '');
        if (!label) return;
        await apiPutJson(`/api/v1/manual/items/${encodeURIComponent(it.id)}`, { label });
        await refreshManual();
      } else if (c === '2') {
        const dir_id = prompt('Nuevo dir_id (carpeta). root=raíz', it.dir_id || 'root');
        if (!dir_id) return;
        await apiPutJson(`/api/v1/manual/items/${encodeURIComponent(it.id)}`, { dir_id });
        await refreshManual();
      } else if (c === '3') {
        const ok = confirm('¿Quitar de este montaje (Manual)?');
        if (!ok) return;
        const rr = await fetch(`/api/v1/manual/items/${encodeURIComponent(it.id)}`, { method: 'DELETE' });
        if (!rr.ok) throw new Error(await rr.text());
        await refreshManual();
      } else if (c === '4') {
        const ok = confirm('¿Borrar global? Desaparece de auto+manual. No borra NZB/PAR2.');
        if (!ok) return;
        await apiPostJson('/api/v1/catalog/imports/delete', { id: it.import_id });
        await refreshManual();
        await refreshList('auto');
      } else if (c === '5') {
        const ok = confirm('⚠ Borrado completo\n\nBD + mover NZB+PAR2 a .trash\n\n¿Continuar?');
        if (!ok) return;
        const typed = prompt('Escribe BORRAR para confirmar');
        if ((typed || '').trim().toUpperCase() !== 'BORRAR') return;
        await apiPostJson('/api/v1/catalog/imports/delete_full', { id: it.import_id });
        await refreshManual();
        await refreshList('auto');
      }
    };
    cell.appendChild(btn);
    row.appendChild(cell);

    list.appendChild(row);
  }

  setStatus(statusId, `OK (${(dirs||[]).length} dirs, ${(items||[]).length} items)`);
}

function goUpManual() {
  if (manualDirId === 'root') return;
  apiGet(`/api/v1/manual/path?dir_id=${encodeURIComponent(manualDirId)}`).then(path => {
    if (!path || path.length < 2) {
      manualDirId = 'root';
    } else {
      manualDirId = path[path.length-2].id || 'root';
    }
    refreshManual().catch(err => setStatus('manStatus', String(err)));
  }).catch(() => {
    manualDirId = 'root';
    refreshManual().catch(err => setStatus('manStatus', String(err)));
  });
}

function fmtSize(n) {
  if (n == null || n === '') return '';
  const x = Number(n);
  if (!isFinite(x)) return String(n);
  const units = ['B','KB','MB','GB','TB'];
  let v = x;
  let i = 0;
  while (v >= 1024 && i < units.length-1) { v /= 1024; i++; }
  const s = (i === 0) ? String(Math.round(v)) : v.toFixed(1);
  return s + ' ' + units[i];
}

// Pages
let __uploadTimer = null;
let __logsLoadedOnce = false;
function showPage(name) {
  for (const id of ['library','upload','import','settings','logs']) {
    document.getElementById('page_' + id).classList.toggle('hide', id !== name);
  }
  for (const item of document.querySelectorAll('.navItem')) {
    item.classList.toggle('active', item.dataset.page === name);
  }

  // Only poll upload status while on Upload page.
  if (__uploadTimer) {
    clearInterval(__uploadTimer);
    __uploadTimer = null;
  }
  if (name === 'upload') {
    refreshUploadPanels().catch(() => {});
    __uploadTimer = setInterval(() => refreshUploadPanels().catch(() => {}), 2500);
  }

  if (name === 'settings') {
    // Always reload settings to reflect persisted values after save/restart.
    loadUploadSettings().catch(() => {});
  }

  }

  if (name === 'logs') {
    if (!__logsLoadedOnce) {
      __logsLoadedOnce = true;
      refreshLogsJobs().catch(() => {});
    }
  }
}

// Library explorer (FUSE)
// NOTE: the actual mount root inside the container is typically /host/mount/*.
// We discover it from the backend to avoid hardcoding /mount/* which breaks on Unraid.
let AUTO_ROOT = '/mount/library-auto';
let MAN_ROOT = '/mount/library-manual'; // legacy label; UI now uses DB-backed manual tree
let autoPath = AUTO_ROOT;
let manPath = MAN_ROOT;
let manualDirId = 'root';

async function initLibraryRoots() {
  // Prefer server-provided roots (matches cfg.paths.mount_point).
  try {
    const r = await apiGet('/api/v1/library/auto/root');
    if (r && r.root) {
      AUTO_ROOT = r.root;
      // Manual root is the sibling mount under the same mount_point.
      MAN_ROOT = String(r.root).replace(/library-auto\s*$/, 'library-manual');
    }
  } catch (e) {
    // Fallback that works for default container config.
    AUTO_ROOT = '/host/mount/library-auto';
    MAN_ROOT = '/host/mount/library-manual';
  }

  // If we were still on the old hardcoded root, reset to discovered root.
  if (!autoPath || autoPath === '/mount/library-auto' || autoPath.startsWith('/mount/library-auto/')) {
    autoPath = AUTO_ROOT;
  }
  if (!manPath || manPath === '/mount/library-manual' || manPath.startsWith('/mount/library-manual/')) {
    manPath = MAN_ROOT;
  }
}

function renderCrumbs(boxId, path, root, onPick) {
  const box = document.getElementById(boxId);
  box.innerHTML = '';

  // Never allow navigating above root (otherwise backend returns: path outside library-auto).
  if (root && typeof root === 'string') {
    if (!path || !String(path).startsWith(root)) path = root;
  }

  const rootParts = (root || '').split('/').filter(Boolean);
  const parts = String(path).split('/').filter(Boolean);

  // Always render a root crumb first (label = last segment of root).
  if (rootParts.length) {
    const rootLabel = rootParts[rootParts.length - 1];
    const bRoot = el('button', { class: 'crumb', type: 'button', text: rootLabel });
    bRoot.onclick = () => onPick('/' + rootParts.join('/'));
    box.appendChild(bRoot);
  }

  // Render only crumbs under root.
  const rest = parts.slice(rootParts.length);
  let acc = '/' + rootParts.join('/');
  for (let i = 0; i < rest.length; i++) {
    if (acc) box.appendChild(el('span', { class: 'crumbSep', text: '›' }));
    acc += '/' + rest[i];
    const target = acc;
    const b = el('button', { class: 'crumb', type: 'button', text: rest[i] });
    b.onclick = () => onPick(target);
    box.appendChild(b);
  }
}

function setStatus(id, t) {
  document.getElementById(id).textContent = t || '';
}

async function refreshList(kind) {
  const isAuto = kind === 'auto';
  const path = isAuto ? autoPath : manPath;
  const root = isAuto ? AUTO_ROOT : MAN_ROOT;
  const crumbsId = isAuto ? 'autoCrumbs' : 'manCrumbs';
  const listId = isAuto ? 'autoList' : 'manList';
  const statusId = isAuto ? 'autoStatus' : 'manStatus';

  setStatus(statusId, 'Cargando...');
  renderCrumbs(crumbsId, path, root, (picked) => {
    // Guard: never allow crumbs to pick above root.
    if (!picked || !String(picked).startsWith(root)) picked = root;
    if (isAuto) autoPath = picked; else manPath = picked;
    refreshList(kind).catch(err => setStatus(statusId, String(err)));
  });

  const data = isAuto
    ? await apiGet(`/api/v1/library/auto/list?path=${encodeURIComponent(path)}`)
    : await apiGet(`/api/v1/hostfs/list?path=${encodeURIComponent(path)}`);
  const list = document.getElementById(listId);
  list.innerHTML = '';

  for (const e of (data.entries || [])) {
    const row = el('div', { class: 'listRow' });
    const icon = e.is_dir ? 'DIR' : 'FILE';
    row.appendChild(el('div', { class: 'name' }, [
      el('div', { class: 'icon', text: icon }),
      el('div', { class: e.is_dir ? '' : 'mono', text: e.name })
    ]));
    row.appendChild(el('div', { class: 'mono muted', text: e.is_dir ? '' : fmtSize(e.size) }));
    row.appendChild(el('div', { class: 'mono muted', text: fmtTime(e.mod_time) }));

    // Action cell (auto list)
    if (isAuto) {
      const cell = el('div');
      if (!e.is_dir && e.import_id) {
        const actions = el('button', { class: 'btn', type: 'button', text: '⋮' });
        actions.style.padding = '6px 10px';
        actions.onclick = async (ev) => {
          ev.stopPropagation();
          const choice = prompt('Acción:\n1 = Borrar global (BD)\n2 = Borrado completo (BD+NZB+PAR2)\n\nEscribe 1 o 2');
          if (!choice) return;
          if (String(choice).trim() === '1') {
            const ok = confirm('¿Borrar global?\n\nDesaparece de auto+manual. No borra NZB/PAR2.');
            if (!ok) return;
            await apiPostJson('/api/v1/catalog/imports/delete', { id: e.import_id });
            await refreshList('auto');
            return;
          }
          if (String(choice).trim() === '2') {
            const ok = confirm('⚠ Borrado completo\n\nBD + mover NZB+PAR2 a .trash\n\n¿Continuar?');
            if (!ok) return;
            const typed = prompt('Escribe BORRAR para confirmar');
            if ((typed || '').trim().toUpperCase() !== 'BORRAR') return;
            await apiPostJson('/api/v1/catalog/imports/delete_full', { id: e.import_id });
            await refreshList('auto');
            return;
          }
        };
        cell.appendChild(actions);
      }
      row.appendChild(cell);
    }

    if (e.is_dir) {
      row.onclick = () => {
        if (isAuto) autoPath = e.path; else manPath = e.path;
        refreshList(kind).catch(err => setStatus(statusId, String(err)));
      };
    }

    list.appendChild(row);
  }

  // Guard: if user navigated out of expected root, snap back.
  if (!path.startsWith(root)) {
    if (isAuto) autoPath = root; else manPath = root;
  }

  setStatus(statusId, `OK (${(data.entries || []).length})`);
}

function goUp(kind) {
  if (kind === 'auto') {
    const root = AUTO_ROOT;
    if (autoPath === root) return;
    const p = autoPath.split('/').filter(Boolean);
    p.pop();
    autoPath = '/' + p.join('/');
    if (!autoPath.startsWith(root)) autoPath = root;
    refreshList('auto').catch(err => setStatus('autoStatus', String(err)));
    return;
  }

  // manual (DB-backed)
  goUpManual();
}

function setLibraryTab(which) {
  const autoPane = document.getElementById('autoPane');
  const manualPane = document.getElementById('manualPane');
  document.getElementById('tabAuto').classList.toggle('active', which === 'auto');
  document.getElementById('tabManual').classList.toggle('active', which === 'manual');
  autoPane.classList.toggle('hide', which !== 'auto');
  manualPane.classList.toggle('hide', which !== 'manual');
  if (which === 'auto') refreshList('auto').catch(err => setStatus('autoStatus', String(err)));
  if (which === 'manual') refreshManual().catch(err => setStatus('manStatus', String(err)));
}

async function restartNow() {
  const btn = document.getElementById('btnRestartTop');
  const old = btn.textContent;
  btn.textContent = 'Reiniciando...';
  btn.disabled = true;
  try {
    await apiPostJson('/api/v1/restart', {});
  } catch (e) {
    alert(String(e));
  } finally {
    setTimeout(() => { btn.textContent = old; btn.disabled = false; }, 6000);
  }
}

// HEALTH (NZB Repair)
const HEALTH_ROOT = '';
// Settings
  if (document.getElementById('btnSetSave')) {
    document.getElementById('btnSetSave').onclick = () => saveUploadSettings().catch(() => {});
    document.getElementById('btnSetReload').onclick = () => loadUploadSettings().catch(() => {});
  }
  if (document.getElementById('setBackupsReload')) {
    document.getElementById('setBackupsReload').onclick = () => refreshBackupsList().catch(() => {});
  }
  if (document.getElementById('setBackupsRun')) {
    document.getElementById('setBackupsRun').onclick = async () => {
      const st = document.getElementById('setBackupsStatus');
      try {
        if (st) st.textContent = 'Ejecutando backup…';
        await apiPostJson('/api/v1/backups/run', { include_config: true });
        await refreshBackupsList();
        if (st) st.textContent = 'Backup manual completado';
      } catch (e) {
        if (st) st.textContent = 'Error backup: ' + String(e);
      }
    };
  }
  const bindRestore = (btnId, includeDB, includeConfig, label) => {
    const btn = document.getElementById(btnId);
    if (!btn) return;
    btn.onclick = async () => {
      const sel = document.getElementById('setBackupsRestoreName');
      const st = document.getElementById('setBackupsStatus');
      const name = sel ? String(sel.value || '').trim() : '';
      if (!name) return;
      const ok = confirm(`¿Restaurar backup ${name}?\n\nModo: ${label}. AlfredEDR se reiniciará.`);
      if (!ok) return;
      try {
        if (st) st.textContent = `Restaurando (${label})…`;
        await apiPostJson('/api/v1/backups/restore', {
          name,
          include_db: includeDB,
          include_config: includeConfig,
        });
        if (st) st.textContent = 'Restaurado. Reiniciando…';
      } catch (e) {
        if (st) st.textContent = 'Error restore: ' + String(e);
      }
    };
  };
  bindRestore('setBackupsRestoreAll', true, true, 'DB+config');
  bindRestore('setBackupsRestoreDB', true, false, 'solo DB');
  bindRestore('setBackupsRestoreConfig', false, true, 'solo config');
  if (document.getElementById('btnDBReset')) {
    document.getElementById('btnDBReset').onclick = async () => {
      const ok = confirm('¿Borrar SOLO la base de datos?\n\n- Se perderán imports/overrides/jobs\n- La configuración NO se borra\n- Reinicia el contenedor\n\n¿Continuar?');
      if (!ok) return;
      const st = document.getElementById('setStatus');
      if (st) st.textContent = 'Reseteando BD… (Resetting DB)';
      await apiPostJson('/api/v1/db/reset', {});
      if (st) st.textContent = 'Reiniciando… (Restarting)';
      await apiPostJson('/api/v1/restart', {});
    };
  }
  if (document.getElementById('btnFileBotTestLicense')) {
    document.getElementById('btnFileBotTestLicense').onclick = async () => {
      const st = document.getElementById('filebotTestStatus');
      try {
        if (st) st.textContent = 'Probando licencia...';
        const r = await apiPostJson('/api/v1/filebot/license/test', {});
        if (st) st.textContent = r && r.ok ? 'OK licencia FileBot' : 'Licencia no válida';
      } catch (e) {
        if (st) st.textContent = 'Error test licencia: ' + String(e);
      }
    };
  }

  // Health
  if (document.getElementById('btnHealthScan')) {
    document.getElementById('btnHealthScan').onclick = async () => {
      try {
      } catch (e) {
      }
    };
  }
  if (document.getElementById('btnHealthRefresh')) {
  }
  if (document.getElementById('btnHealthUp')) {
    document.getElementById('btnHealthUp').onclick = () => goUpHealth();
  }

  // Logs
  if (document.getElementById('btnLogsRefresh')) {
    document.getElementById('btnLogsRefresh').onclick = () => refreshLogsJobs().catch(() => {});
  }

  // Load imports + review initially
  refreshImports().catch(() => {});
  })().catch(err => {
    console.error(err);
    alert(String(err));
  });
});
