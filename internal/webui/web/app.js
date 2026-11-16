// DESIGN.md §3: the server is the single source of truth. Every control
// below sends a command over the one WebSocket and waits for the server's
// broadcast to actually update what's on screen — nothing here flips its
// own label/state on click.

let ws;
let state = null;

function send(action, data) {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ action, data }));
  }
}

function connect() {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  ws = new WebSocket(`${proto}://${location.host}/ws`);

  ws.onopen = () => setStatusText(t('status.connected'));
  ws.onclose = () => {
    setStatusText(t('status.disconnected'));
    setTimeout(connect, 2000);
  };
  ws.onerror = () => {};

  ws.onmessage = (evt) => {
    const msg = JSON.parse(evt.data);
    switch (msg.type) {
      case 'snapshot':
        state = msg.data;
        renderAll();
        break;
      case 'mount_state_changed':
        state.running = msg.data.running;
        state.mounts = msg.data.mounts;
        renderRunState();
        break;
      case 'stats_tick':
        state.stats = msg.data;
        renderStats();
        break;
      case 'log_line':
        appendLog(msg.data);
        break;
      case 'regions_changed':
        state.regions = msg.data;
        renderRegions();
        break;
      case 'region_changed': {
        const idx = state.regions.findIndex(r => r.id === msg.data.id);
        if (idx >= 0) state.regions[idx] = msg.data;
        else state.regions.push(msg.data);
        renderRegions();
        break;
      }
      case 'config_saved':
        state.config = msg.data;
        renderConfig();
        flashSaveStatus(t('status.saved'));
        break;
      case 'cache_cleaned':
        flashSaveStatus(t('status.cache_cleaned', {
          n: msg.data.files_removed,
          mb: (msg.data.bytes_freed / 1048576).toFixed(1),
        }));
        break;
      case 'error':
        flashSaveStatus(t('status.error', { msg: msg.data }), true);
        break;
      case 'region_verified':
        flashSaveStatus(
          msg.data.ok
            ? t('scenery.verify_ok', { name: msg.data.name, message: msg.data.message })
            : t('scenery.verify_failed', { name: msg.data.name, message: msg.data.message }),
          !msg.data.ok
        );
        break;
    }
  };
}

function setStatusText(text) {
  document.getElementById('status-text').textContent = text;
}

function renderAll() {
  renderRunState();
  renderConfig();
  renderStats();
  renderRegions();
  renderLogs();
}

function renderRunState() {
  const dot = document.getElementById('status-dot');
  const text = document.getElementById('status-text');
  dot.classList.toggle('on', !!state.running);
  text.textContent = state.running
    ? t('status.running', { n: state.mounts.length })
    : t('status.stopped');
  document.getElementById('btn-run').disabled = !!state.running;
  document.getElementById('btn-stop').disabled = !state.running;
}

// ZOOM_LEVELS are the preset zoom-level choices offered in the Min/Max
// zoom dropdowns — a fixed, curated range instead of a free-form number
// field, so a user can't accidentally type a zoom level the imagery
// providers don't actually serve. 12-19 covers the realistic range for
// this project's providers (BI/EOX/etc. — see internal/provider); 15 is
// specifically included because it's the exact level an actual user
// mentioned being happy with for most flying.
const ZOOM_LEVELS = [12, 13, 14, 15, 16, 17, 18, 19];

// populateZoomSelect fills a <select> with ZOOM_LEVELS, optionally with a
// leading "No cap" (value 0) option — used for Max zoom, since 0 there
// means the cap is disabled (config.AutoOrtho.MaxZoom's documented
// meaning), not an actual zoom level.
function populateZoomSelect(selectId, value, includeNoCap) {
  const sel = document.getElementById(selectId);
  sel.innerHTML = '';
  if (includeNoCap) {
    const o = document.createElement('option');
    o.value = '0';
    o.textContent = t('zoom.nocap');
    if (value === 0) o.selected = true;
    sel.appendChild(o);
  }
  for (const zl of ZOOM_LEVELS) {
    const o = document.createElement('option');
    o.value = String(zl);
    o.textContent = String(zl);
    if (zl === value) o.selected = true;
    sel.appendChild(o);
  }
  // An existing config value outside the preset list (an older/hand-
  // edited ini, say) must still show up and round-trip correctly instead
  // of silently snapping to whatever option happens to render first.
  const knownValues = includeNoCap ? [0, ...ZOOM_LEVELS] : ZOOM_LEVELS;
  if (!knownValues.includes(value)) {
    const o = document.createElement('option');
    o.value = String(value);
    o.textContent = String(value);
    o.selected = true;
    sel.appendChild(o);
  }
}

// updateMaxZoomWarning shows zoom.maxzoom_warning's hint whenever Max zoom
// is capped below 18 — see that key's own text for why: a flat cap hits
// dense areas (cities, airports) much harder than open countryside, since
// X-Plane requests higher native zoom there to begin with (confirmed via a
// real Berlin flight tonight — capping 3 levels below what an area wants
// means 8x fewer pixels, not a mild blur). 0 means "No cap", never warn.
const MAXZOOM_WARNING_THRESHOLD = 18;
function updateMaxZoomWarning() {
  const val = parseInt(document.getElementById('cfg-max_zoom').value, 10);
  const warn = val > 0 && val < MAXZOOM_WARNING_THRESHOLD;
  document.getElementById('maxzoom-warning').classList.toggle('hidden', !warn);
}

function renderConfig() {
  const c = state.config;
  detectedUsername = detectUsername(c);
  setPathInputValue('cfg-xplane_path', c.xplane_path);
  setPathInputValue('cfg-scenery_path', c.scenery_path);
  setPathInputValue('cfg-cache_dir', c.cache_dir);
  setPathInputValue('cfg-download_dir', c.download_dir);
  setVal('cfg-file_cache_size_gb', c.file_cache_size_gb);
  populateZoomSelect('cfg-min_zoom', c.min_zoom, false);
  populateZoomSelect('cfg-max_zoom', c.max_zoom, true);
  updateMaxZoomWarning();
  setVal('cfg-maxwait', c.maxwait);
  setVal('cfg-max_concurrent_builds', c.max_concurrent_builds);
  setVal('cfg-fetch_threads', c.fetch_threads);
  setVal('cfg-max_open_tiles', c.max_open_tiles);
  setVal('cfg-performance_mode', c.performance_mode);
  document.getElementById('cfg-disable_coarse_fallback').checked = !!c.disable_coarse_fallback;
  document.getElementById('cfg-auto_open_ui').checked = !!c.auto_open_ui;

  const sel = document.getElementById('cfg-maptype_override');
  sel.innerHTML = '';
  const opts = ['', ...(c.maptypes || [])];
  for (const opt of opts) {
    const o = document.createElement('option');
    o.value = opt;
    o.textContent = opt || t('zoom.maptype_default');
    if (opt === c.maptype_override) o.selected = true;
    sel.appendChild(o);
  }

  // Values above were just set programmatically from the server's own
  // snapshot/config_saved broadcast, not by the user — so whatever "has
  // the user changed anything since the last save" tracking was doing,
  // this is the moment it should reset to "nothing to save yet".
  markSaveDirty(false);
}

// --- Save button dirty/clean indicator ------------------------------------
// Green (#btn-save.save-clean) means the form matches what's on disk;
// yellow (#btn-save.save-dirty) means the user has touched a control since
// the last successful save and needs to click Save themselves — nothing
// auto-saves. Tracked by listening on every #cfg-* control rather than
// diffing values, since that's simpler and exactly as correct: the only
// way these controls change is a real user edit (renderConfig only ever
// sets .value/.checked directly, which never fires input/change).
function markSaveDirty(dirty) {
  const btn = document.getElementById('btn-save');
  btn.classList.toggle('save-dirty', dirty);
  btn.classList.toggle('save-clean', !dirty);
}

function setupSaveDirtyTracking() {
  document.querySelectorAll('[id^="cfg-"]').forEach((el) => {
    el.addEventListener('input', () => markSaveDirty(true));
    el.addEventListener('change', () => markSaveDirty(true));
  });
}

function setVal(id, v) {
  const el = document.getElementById(id);
  if (document.activeElement !== el) el.value = v;
}

function renderStats() {
  const s = state.stats || {};
  document.getElementById('stat-cached_tiles').textContent = s.cached_tiles ?? '-';
  document.getElementById('stat-open_tiles').textContent = s.open_tiles ?? '-';
  document.getElementById('stat-rss').textContent = s.process_rss_bytes
    ? `${(s.process_rss_bytes / (1 << 20)).toFixed(0)} MB / ${(s.total_system_mem_bytes / (1 << 20)).toFixed(0)} MB`
    : '-';
  document.getElementById('stat-mounts').textContent = (state.mounts || []).length;
}

// PHASES_WITH_BYTE_PROGRESS are the phases where download_percent is actual
// byte-level progress (so a determinate bar makes sense). Every other
// phase — verifying/assembling/installing, and "extracting" once a
// package's file count is known — either reports 0 or a file-count-based
// percent; see renderRegions below for how extracting is handled
// specially since it does carry a real (file-count) percent.
const PHASE_INDETERMINATE = new Set(['verifying', 'assembling', 'installing']);

function renderRegions() {
  const container = document.getElementById('regions');
  container.innerHTML = '';
  for (const r of (state.regions || [])) {
    const row = document.createElement('div');
    row.className = 'region-row';
    if (r.error) row.classList.add('region-row-error');

    const name = document.createElement('span');
    name.className = 'region-name';
    name.textContent = r.name || r.id;
    if (r.installed) {
      const dot = document.createElement('span');
      dot.className = 'dot region-mount-dot' + (r.mounted ? ' on' : '');
      dot.title = r.mounted ? t('scenery.mounted') : t('scenery.unmounted');
      name.prepend(dot);
    }

    const meta = document.createElement('span');
    meta.className = 'region-meta';
    if (r.downloading) {
      // download_status comes straight from the Go downloader
      // (scenery.Progress.Status) — real operational text, not UI copy,
      // so it's shown as-is rather than translated.
      const pkgLabel = r.package_total
        ? ` — ${t('scenery.package_n_of', { index: r.package_index, total: r.package_total })}`
        : '';
      const phase = r.phase || 'downloading';
      const pctLabel = PHASE_INDETERMINATE.has(phase) ? '' : ` (${(r.download_percent || 0).toFixed(0)}%)`;
      meta.textContent = `${r.download_status || '...'}${pctLabel}${pkgLabel}`;
    } else if (r.error) {
      meta.classList.add('error');
      meta.textContent = t('scenery.error', { msg: r.error });
    } else if (r.installed) {
      meta.textContent = r.up_to_date
        ? t('scenery.installed', { ver: r.local_version })
        : t('scenery.update_available', { local: r.local_version, latest: r.latest_version });
    } else {
      meta.textContent = `${r.latest_version} · ` + t('scenery.size', { size: r.size_mb.toFixed(0) });
    }

    row.appendChild(name);
    row.appendChild(meta);

    if (r.downloading) {
      const phase = r.phase || 'downloading';
      const bar = document.createElement('div');
      bar.className = 'region-progress';
      const fill = document.createElement('div');
      if (PHASE_INDETERMINATE.has(phase)) {
        fill.className = 'region-progress-fill indeterminate';
      } else {
        fill.className = 'region-progress-fill';
        fill.style.width = `${Math.min(100, Math.max(0, r.download_percent || 0)).toFixed(0)}%`;
      }
      bar.appendChild(fill);
      row.appendChild(bar);
    }

    if (!r.installed || !r.up_to_date) {
      const btn = document.createElement('button');
      btn.textContent = r.downloading ? t('scenery.working') : (r.installed ? t('scenery.update') : t('scenery.install'));
      btn.disabled = !!r.downloading;
      btn.onclick = () => send('install_region', { id: r.id });
      row.appendChild(btn);
    }

    if (r.installed) {
      const verifyBtn = document.createElement('button');
      verifyBtn.textContent = r.verifying ? t('scenery.verifying') : t('scenery.verify');
      verifyBtn.disabled = !!r.verifying || !!r.downloading;
      verifyBtn.onclick = () => send('verify_region', { id: r.id });
      row.appendChild(verifyBtn);

      const mountBtn = document.createElement('button');
      mountBtn.textContent = r.mounted ? t('scenery.unmount') : t('scenery.mount');
      mountBtn.disabled = !!r.downloading;
      mountBtn.onclick = () => send(r.mounted ? 'unmount_region' : 'mount_region', { id: r.id });
      row.appendChild(mountBtn);
    }

    container.appendChild(row);
  }
}

// rawLogLines tracks every line ever shown (initial snapshot + each live
// log_line message) in its original, unredacted form — so toggling demo
// mode can re-render the whole visible history through redactUsername
// without losing anything appended since the last full renderLogs() call,
// which only state.logs (the initial snapshot) would do.
let rawLogLines = [];

function renderLogs() {
  rawLogLines = state.logs || [];
  const pre = document.getElementById('log');
  pre.textContent = rawLogLines.map(redactUsername).join('\n');
  pre.scrollTop = pre.scrollHeight;
}

function appendLog(line) {
  rawLogLines.push(line);
  const pre = document.getElementById('log');
  pre.textContent += (pre.textContent ? '\n' : '') + redactUsername(line);
  pre.scrollTop = pre.scrollHeight;
}

// rerenderLogsFromBuffer redraws the Log block from rawLogLines (not
// state.logs) — used when demo mode is toggled, so already-streamed lines
// since the last full renderLogs() aren't lost.
function rerenderLogsFromBuffer() {
  const pre = document.getElementById('log');
  pre.textContent = rawLogLines.map(redactUsername).join('\n');
  pre.scrollTop = pre.scrollHeight;
}

function flashSaveStatus(text, isError) {
  const el = document.getElementById('save-status');
  el.textContent = text;
  el.style.color = isError ? 'var(--error-text)' : '';
  setTimeout(() => { if (el.textContent === text) el.textContent = ''; }, 4000);
}

function confirmAction(text, onYes) {
  const overlay = document.getElementById('confirm-overlay');
  document.getElementById('confirm-text').textContent = text;
  overlay.classList.remove('hidden');
  const yes = document.getElementById('confirm-yes');
  const no = document.getElementById('confirm-no');
  const cleanup = () => {
    overlay.classList.add('hidden');
    yes.onclick = null;
    no.onclick = null;
  };
  yes.onclick = () => { cleanup(); onYes(); };
  no.onclick = cleanup;
}

// --- Directory browser ------------------------------------------------
// A plain REST round trip against the server's own filesystem, not a
// WebSocket command: picking a folder is a local, read-only UI action,
// not server state DESIGN.md §3 needs to broadcast to other tabs. This
// only works because the web UI is loopback-bound (§10) — the server and
// the browser hitting it are the same machine.

let dirBrowserTargetId = null;

function openDirBrowser(targetInputId) {
  dirBrowserTargetId = targetInputId;
  const startPath = pathFieldValue(targetInputId).trim();
  document.getElementById('dirbrowser-up').disabled = false;
  document.getElementById('dirbrowser-overlay').classList.remove('hidden');
  loadDirBrowser(startPath);
}

function closeDirBrowser() {
  document.getElementById('dirbrowser-overlay').classList.add('hidden');
  dirBrowserTargetId = null;
}

function loadDirBrowser(path) {
  const url = '/api/browse' + (path ? '?path=' + encodeURIComponent(path) : '');
  fetch(url)
    .then(async (resp) => {
      const body = await resp.json();
      if (!resp.ok) throw new Error(body.error || `HTTP ${resp.status}`);
      return body;
    })
    .then(renderDirBrowser)
    .catch((err) => showDirBrowserError(err.message));
}

function showDirBrowserError(message) {
  const el = document.getElementById('dirbrowser-error');
  el.textContent = message;
  el.classList.remove('hidden');
}

function renderDirBrowser(data) {
  document.getElementById('dirbrowser-error').classList.add('hidden');
  document.getElementById('dirbrowser-path').value = data.path;

  const upBtn = document.getElementById('dirbrowser-up');
  upBtn.disabled = !data.parent;
  upBtn.onclick = data.parent ? () => loadDirBrowser(data.parent) : null;

  renderDirBrowserList(data.entries, (entry) => loadDirBrowser(entry.path));
}

function renderDirBrowserList(entries, onPick) {
  const list = document.getElementById('dirbrowser-list');
  list.innerHTML = '';
  for (const entry of entries) {
    const li = document.createElement('li');
    li.textContent = entry.name;
    li.onclick = () => onPick(entry);
    list.appendChild(li);
  }
}

// commitBrowsedPath is the one place a folder chosen through either picker
// (the native OS dialog or the in-page fallback browser) actually lands in
// a path field — shared so both flows get the same X-Plane-dir autofill.
function commitBrowsedPath(targetInputId, chosen) {
  setPathInputValue(targetInputId, chosen);
  if (targetInputId === 'cfg-xplane_path') autofillFromXplanePath(chosen);
}

// tryNativeBrowse asks the backend to pop a real OS folder-picker dialog
// (see native_picker.go) — the in-page browser below only runs as a
// fallback, for a machine with none of zenity/kdialog/powershell/osascript
// available (GET /api/browse/native then answers 501).
function tryNativeBrowse(targetInputId) {
  const startPath = pathFieldValue(targetInputId).trim();
  const url = '/api/browse/native' + (startPath ? '?path=' + encodeURIComponent(startPath) : '');
  fetch(url)
    .then(async (resp) => {
      if (resp.status === 501) {
        openDirBrowser(targetInputId);
        return;
      }
      const body = await resp.json();
      if (!resp.ok) throw new Error(body.error || `HTTP ${resp.status}`);
      if (body.path) commitBrowsedPath(targetInputId, body.path);
    })
    .catch(() => openDirBrowser(targetInputId));
}

function setupDirBrowser() {
  document.querySelectorAll('.browse-btn').forEach((btn) => {
    btn.onclick = () => tryNativeBrowse(btn.dataset.target);
  });
  document.getElementById('dirbrowser-cancel').onclick = closeDirBrowser;
  document.getElementById('dirbrowser-select').onclick = () => {
    const chosen = document.getElementById('dirbrowser-path').value;
    if (dirBrowserTargetId) commitBrowsedPath(dirBrowserTargetId, chosen);
    closeDirBrowser();
  };
}

// Once the X-Plane install dir is known (however it got set — typed,
// Browse…, or Find X-Plane…), the Scenery install dir has one obvious
// correct default (DESIGN.md's own documented layout, see the field's own
// hint text) — fill it in automatically, but only if the user hasn't
// already put something else there, so this never clobbers a deliberate
// choice.
function autofillFromXplanePath(xplanePath) {
  xplanePath = (xplanePath || '').trim();
  if (!xplanePath) return;
  if (!pathFieldValue('cfg-scenery_path').trim()) {
    setPathInputValue('cfg-scenery_path', xplanePath.replace(/[\\/]+$/, '') + '/Custom Scenery/z_autoortho');
  }
}

function showDetectXplaneStatus(key, params) {
  const el = document.getElementById('detect-xplane-status');
  el.textContent = t(key, params);
  el.classList.remove('hidden');
}

function setupDetectXplane() {
  const btn = document.getElementById('btn-detect-xplane');
  const xplaneInput = document.getElementById('cfg-xplane_path');

  xplaneInput.addEventListener('change', () => autofillFromXplanePath(pathFieldValue('cfg-xplane_path')));

  btn.onclick = () => {
    document.getElementById('detect-xplane-status').classList.add('hidden');
    btn.disabled = true;
    fetch('/api/detect-xplane')
      .then(async (resp) => {
        const body = await resp.json();
        if (!resp.ok) throw new Error(body.error || `HTTP ${resp.status}`);
        return body.candidates || [];
      })
      .then((candidates) => {
        if (candidates.length === 0) {
          showDetectXplaneStatus('setup.detect_xplane_none');
        } else if (candidates.length === 1) {
          setPathInputValue('cfg-xplane_path', candidates[0].path);
          autofillFromXplanePath(candidates[0].path);
          showDetectXplaneStatus('setup.detect_xplane_found', { path: candidates[0].path });
        } else {
          showDetectXplaneStatus('setup.detect_xplane_multiple', { n: candidates.length });
          // More than one X-Plane install found — rather than making the
          // user pick from yet another custom list widget, drop them
          // straight into the folder picker (native dialog, or the
          // fallback browser) already positioned at the first match so
          // they can confirm it or navigate to whichever one they meant.
          setPathInputValue('cfg-xplane_path', candidates[0].path);
          tryNativeBrowse('cfg-xplane_path');
        }
      })
      .catch((err) => showDetectXplaneStatus('setup.detect_xplane_error', { error: err.message }))
      .finally(() => { btn.disabled = false; });
  };
}

function setupLanguageSwitcher() {
  document.querySelectorAll('.lang-btn').forEach((btn) => {
    btn.onclick = () => setLanguage(btn.dataset.lang, () => {
      // Re-render everything built dynamically with t() — the static
      // data-i18n/data-i18n-html chrome is already handled by setLanguage
      // itself, but region rows, the maptype dropdown, and the status
      // line are all generated JS, not markup, so they need a real
      // re-render to pick up the new language.
      if (state) renderAll();
      renderMascotOutfits();
    });
  });
  applyStaticTranslations();
}

function setupThemeSwitcher() {
  document.querySelectorAll('.theme-btn').forEach((btn) => {
    btn.onclick = () => setTheme(btn.dataset.theme);
  });
  applyTheme();
}

// --- Demonstration mode --------------------------------------------------
// A purely client-side, per-viewer toggle — same category as theme/
// background/mascot (not saved config, not sent to the backend): redacts
// the OS username — not the whole value — in the 4 X-Plane/scenery/cache/
// download path fields, and out of the Log block (the backend streams raw
// log lines verbatim — internal/logging's own convention — so a real path
// like /home/<user>/... appears there unmodified). Exists so a screenshot
// or screen recording of this page doesn't leak either, without hiding
// the rest of the path (still useful context in a screenshot) and without
// ever changing what's actually configured — see setPathInputValue's doc
// comment for how the real value and the possibly-redacted display stay
// in sync.
const DEMO_MODE_STORAGE_KEY = 'ao_demo_mode';

// pathFieldIds lists every <input> whose value is a real local filesystem
// path containing the OS username — the set demonstration mode redacts.
const pathFieldIds = ['cfg-xplane_path', 'cfg-scenery_path', 'cfg-cache_dir', 'cfg-download_dir'];

function loadStoredDemoMode() {
  try {
    return localStorage.getItem(DEMO_MODE_STORAGE_KEY) === '1';
  } catch (e) {
    return false;
  }
}

let demoMode = loadStoredDemoMode();

// detectedUsername is extracted once per renderConfig call from whichever
// configured path first matches a known home-directory pattern — Linux
// (/home/<user>/), macOS (/Users/<user>/), or Windows (\Users\<user>\).
// null if none of the 4 path fields are set yet or none match.
let detectedUsername = null;

function detectUsername(cfg) {
  const candidates = [cfg.xplane_path, cfg.scenery_path, cfg.cache_dir, cfg.download_dir];
  const patterns = [/\/home\/([^/]+)\//, /\/Users\/([^/]+)\//, /[Uu]sers\\([^\\]+)\\/];
  for (const path of candidates) {
    if (!path) continue;
    for (const re of patterns) {
      const m = re.exec(path);
      if (m) return m[1];
    }
  }
  return null;
}

// redactUsername is applied to every Log line when demoMode is on — a plain
// substring replace (not RegExp), so an unusual username containing regex
// metacharacters can't break it or silently fail to match.
function redactUsername(line) {
  if (!demoMode || !detectedUsername) return line;
  return line.split(detectedUsername).join('****');
}

// setPathInputValue is the one place any of the 4 path <input>s ever gets
// its value set — renderConfig (fresh state from the server), the
// directory browser's "Select this folder", "Find X-Plane…", and
// autofillFromXplanePath all go through this instead of touching .value
// directly. It stashes the real value in a data attribute (the actual
// source of truth, read back by the Save button's payload regardless of
// demo mode) and sets the visible .value to either that real value or its
// redacted form, depending on the current demoMode — so toggling demo mode
// on/off at any time, even after the value has changed underneath, always
// shows the right thing without ever touching what's really configured.
// Mirrors setVal's "don't clobber an in-progress edit" guard.
function setPathInputValue(id, value) {
  const el = document.getElementById(id);
  if (document.activeElement === el) return;
  el.dataset.realValue = value;
  el.value = redactUsername(value);
}

// pathFieldValue reads the real (never redacted) value of one of the 4
// path fields — what the Save button's payload must use regardless of
// whether demo mode currently has that field showing a masked display.
function pathFieldValue(id) {
  const el = document.getElementById(id);
  return el.dataset.realValue !== undefined ? el.dataset.realValue : el.value;
}

function applyDemoMode() {
  for (const id of pathFieldIds) {
    const el = document.getElementById(id);
    el.readOnly = demoMode; // typing over a masked display would save garbage; Browse/Find X-Plane still work (they go through setPathInputValue, not keystrokes)
    setPathInputValue(id, pathFieldValue(id)); // re-derive the display (masked or real) for the current demoMode
  }
  const toggle = document.getElementById('demo-mode-toggle');
  if (toggle) toggle.checked = demoMode;
  rerenderLogsFromBuffer(); // re-run redaction (or remove it) without losing lines appended since the last full renderLogs()
}

function setDemoMode(on) {
  demoMode = !!on;
  try {
    localStorage.setItem(DEMO_MODE_STORAGE_KEY, demoMode ? '1' : '0');
  } catch (e) {
    // Non-fatal — the choice just won't survive a reload this session.
  }
  applyDemoMode();
}

function setupDemoModeToggle() {
  const toggle = document.getElementById('demo-mode-toggle');
  toggle.onchange = () => setDemoMode(toggle.checked);
  applyDemoMode();
}

// --- Settings panel -----------------------------------------------------
// A single gear icon in the header opens an overlay holding the language
// and theme switchers (moved here from the header itself) plus the
// background-image picker below. Same hidden-class overlay pattern as the
// confirm/directory-browser dialogs elsewhere on this page.

function openSettings() {
  document.getElementById('settings-overlay').classList.remove('hidden');
}

function closeSettings() {
  document.getElementById('settings-overlay').classList.add('hidden');
}

function setupSettingsPanel() {
  document.getElementById('btn-settings').onclick = openSettings;
  document.getElementById('settings-close').onclick = closeSettings;
  // Clicking the dimmed backdrop (not the panel itself) also closes it —
  // this is a low-stakes preferences panel, not a destructive confirm
  // dialog, so that extra convenience is fine here specifically.
  document.getElementById('settings-overlay').addEventListener('click', (evt) => {
    if (evt.target.id === 'settings-overlay') closeSettings();
  });
}

// --- Usage guide panel ----------------------------------------------------
// Same hidden-class overlay pattern as settings/confirm/directory-browser.
// This covers using the app once it's already installed and running —
// install/build steps live in the README instead (the guide content itself
// is English-only, deliberately not run through i18n's per-language dicts;
// t() falls back to the 'en' entry for any language missing a key, so
// leaving it only in I18N.en is enough — it just won't be translated yet).

function openUserGuide() {
  document.getElementById('user-guide-overlay').classList.remove('hidden');
}

function closeUserGuide() {
  document.getElementById('user-guide-overlay').classList.add('hidden');
}

function setupUserGuide() {
  document.getElementById('btn-user-guide').onclick = openUserGuide;
  document.getElementById('guide-close').onclick = closeUserGuide;
  document.getElementById('user-guide-overlay').addEventListener('click', (evt) => {
    if (evt.target.id === 'user-guide-overlay') closeUserGuide();
  });
}

// --- Custom background image --------------------------------------------
// Stored as a data: URL in localStorage — a per-viewer preference, not
// server state, same category as the theme/language choice. Downscaled
// through a canvas before storing so a large phone photo doesn't blow
// past typical per-origin localStorage quotas (~5-10MB).

const BG_STORAGE_KEY = 'autoortho_bg_image';
const BG_OPACITY_STORAGE_KEY = 'autoortho_bg_opacity';
const BG_MAX_DIMENSION = 1920;
const BG_DEFAULT_OPACITY_PERCENT = 3; // matches style.css's #bg-overlay-layer default

function setBackgroundImage(dataUrl) {
  const layer = document.getElementById('bg-overlay-layer');
  layer.style.backgroundImage = dataUrl ? `url("${dataUrl}")` : '';
  // The opacity slider only makes sense once there's actually an image to
  // adjust — hidden the rest of the time rather than sitting there inert.
  document.getElementById('bg-opacity-row').classList.toggle('hidden', !dataUrl);
}

function loadStoredBgOpacityPercent() {
  try {
    const stored = localStorage.getItem(BG_OPACITY_STORAGE_KEY);
    const n = stored ? parseInt(stored, 10) : NaN;
    if (!isNaN(n) && n >= 1 && n <= 5) return n;
  } catch (e) {
    // localStorage unavailable — fall through to the default.
  }
  return BG_DEFAULT_OPACITY_PERCENT;
}

function setBackgroundOpacityPercent(percent, persist) {
  const layer = document.getElementById('bg-overlay-layer');
  layer.style.opacity = String(percent / 100);
  document.getElementById('bg-opacity-slider').value = String(percent);
  document.getElementById('bg-opacity-value').textContent = `${percent}%`;
  if (persist) {
    try {
      localStorage.setItem(BG_OPACITY_STORAGE_KEY, String(percent));
    } catch (e) {
      // Non-fatal — the slider still works for this session either way.
    }
  }
}

function applyBackgroundFromStorage() {
  let dataUrl = null;
  try {
    dataUrl = localStorage.getItem(BG_STORAGE_KEY);
  } catch (e) {
    // localStorage unavailable — no persisted background, start plain.
  }
  setBackgroundImage(dataUrl);
  setBackgroundOpacityPercent(loadStoredBgOpacityPercent(), false);
}

function handleBackgroundFileChosen(file) {
  if (!file) return;
  const reader = new FileReader();
  reader.onload = () => {
    const img = new Image();
    img.onload = () => {
      let { width, height } = img;
      if (width > BG_MAX_DIMENSION || height > BG_MAX_DIMENSION) {
        const scale = BG_MAX_DIMENSION / Math.max(width, height);
        width = Math.round(width * scale);
        height = Math.round(height * scale);
      }
      const canvas = document.createElement('canvas');
      canvas.width = width;
      canvas.height = height;
      canvas.getContext('2d').drawImage(img, 0, 0, width, height);
      const dataUrl = canvas.toDataURL('image/jpeg', 0.82);
      try {
        localStorage.setItem(BG_STORAGE_KEY, dataUrl);
      } catch (e) {
        flashSaveStatus(t('settings.background_too_large'), true);
        return;
      }
      setBackgroundImage(dataUrl);
    };
    img.src = reader.result;
  };
  reader.readAsDataURL(file);
}

function removeBackgroundImage() {
  try {
    localStorage.removeItem(BG_STORAGE_KEY);
  } catch (e) {
    // Non-fatal — worst case the old image just reappears next load.
  }
  setBackgroundImage(null);
}

function setupBackgroundPicker() {
  const fileInput = document.getElementById('bg-file-input');
  document.getElementById('btn-choose-bg').onclick = () => fileInput.click();
  fileInput.onchange = () => handleBackgroundFileChosen(fileInput.files[0]);
  document.getElementById('btn-remove-bg').onclick = removeBackgroundImage;
  document.getElementById('bg-opacity-slider').oninput = (evt) => {
    setBackgroundOpacityPercent(parseInt(evt.target.value, 10), true);
  };
  applyBackgroundFromStorage();
}

// --- Desktop mascot ------------------------------------------------------
// A purely client-side, per-viewer toggle — same category as the theme and
// background-image preferences above, persisted the same way. The original
// plan was a native always-on-top desktop overlay, which runs into
// Wayland's deliberate lack of a global-cursor-position API.
// None of that applies here — this lives inside the page's own DOM, so
// "where's the cursor" is just a mousemove listener, no OS-level API needed.

// Each pupil layer is a full-canvas PNG (930x1408, see mascot/*.png) with the
// actual pupil art sitting at a fixed spot within it — these are that spot's
// center, as a fraction of the canvas, measured directly from the exported
// art (Pillow's alpha bbox) rather than guessed. Expressed as fractions (not
// pixels) so they stay correct regardless of the mascot's on-screen size.
const MASCOT_EYES = [
  { el: null, fx: 286 / 930, fy: 642 / 1408 },  // left_eye.png
  { el: null, fx: 559 / 930, fy: 606.5 / 1408 }, // right_eye.png
];
// How far a pupil may be nudged from its rest position, in canvas px, before
// it starts peeking past the body art's eye hole (a sliver of sclera shows
// past the pupil's edge). Tuned by eye against the actual exported art —
// 30-35 reads clearly as "looking that direction" in all 4 directions with
// no visible clipping; much above ~65 starts showing that sliver.
const MASCOT_PUPIL_MAX_SHIFT_CANVAS_PX = 32;
const MASCOT_CANVAS_WIDTH = 930;

// Outfits: swappable body-art layers, same canvas/eye-hole alignment as the
// default (mascot_body.png) so the sclera/pupil layers underneath still
// show through correctly regardless of which one is active — each file's
// transparent eye-hole mask was copied directly from mascot_body.png's own
// (not hand-detected per outfit), which is what actually keeps them
// pixel-aligned. Add a new {file, emoji} entry here (and the matching PNG
// in mascot/) to add another outfit later. No dedicated "fighter jet" emoji
// exists in Unicode, so these are the closest distinguishable stand-ins,
// not literal matches.
const MASCOT_OUTFITS = [
  { file: 'mascot_body.png', emoji: '🛩️', nameKey: 'mascot.outfit_jetfighter' },   // original flight suit
  { file: 'mascot_body_acrobatic.png', emoji: '✈️', nameKey: 'mascot.outfit_acrobatic' }, // acrobatic pilot
  { file: 'mascot_body_airliner.png', emoji: '🛫', nameKey: 'mascot.outfit_airliner' },  // airline captain
  { file: 'mascot_body_agricultural.png', emoji: '🌾', nameKey: 'mascot.outfit_agricultural' }, // ag/crop-duster pilot
];
const MASCOT_OUTFIT_STORAGE_KEY = 'autoortho_mascot_outfit';

function loadStoredMascotOutfit() {
  try {
    const stored = localStorage.getItem(MASCOT_OUTFIT_STORAGE_KEY);
    if (MASCOT_OUTFITS.some((o) => o.file === stored)) return stored;
  } catch (e) {
    // fall through to default
  }
  return MASCOT_OUTFITS[0].file;
}

function setMascotOutfit(file, persist) {
  document.getElementById('mascot-clickzone').src = 'mascot/' + file;
  document.getElementById('sel-mascot-outfit').value = file;
  if (persist) {
    try {
      localStorage.setItem(MASCOT_OUTFIT_STORAGE_KEY, file);
    } catch (e) {
      // Non-fatal — the outfit still applies for this session either way.
    }
  }
}

// renderMascotOutfits (re)builds the outfit <select>'s options with
// translated names — pulled out of setupMascot so a language switch can
// re-run it too (setupMascot itself only ever runs once, at startup).
function renderMascotOutfits() {
  const outfitSel = document.getElementById('sel-mascot-outfit');
  const current = outfitSel.value;
  outfitSel.innerHTML = '';
  for (const o of MASCOT_OUTFITS) {
    const opt = document.createElement('option');
    opt.value = o.file;
    opt.textContent = `${o.emoji} ${t(o.nameKey)}`;
    outfitSel.appendChild(opt);
  }
  if (current) outfitSel.value = current;
}

function setupMascot() {
  const mascot = document.getElementById('mascot');
  MASCOT_EYES[0].el = document.getElementById('mascot-pupil-left');
  MASCOT_EYES[1].el = document.getElementById('mascot-pupil-right');

  let lastX = window.innerWidth / 2;
  let lastY = window.innerHeight / 2;
  let rafPending = false;

  function applyPointer(x, y) {
    const rect = mascot.getBoundingClientRect();
    const maxShift = MASCOT_PUPIL_MAX_SHIFT_CANVAS_PX * (rect.width / MASCOT_CANVAS_WIDTH);
    for (const eye of MASCOT_EYES) {
      const cx = rect.left + eye.fx * rect.width;
      const cy = rect.top + eye.fy * rect.height;
      const dx = x - cx;
      const dy = y - cy;
      const dist = Math.hypot(dx, dy) || 1;
      const scale = Math.min(dist, maxShift) / dist;
      eye.el.style.transform = `translate(${dx * scale}px, ${dy * scale}px)`;
    }
  }

  window.addEventListener('mousemove', (evt) => {
    lastX = evt.clientX;
    lastY = evt.clientY;
    if (rafPending) return;
    rafPending = true;
    requestAnimationFrame(() => {
      rafPending = false;
      applyPointer(lastX, lastY);
    });
  });

  renderMascotOutfits();
  const outfitSel = document.getElementById('sel-mascot-outfit');
  outfitSel.onchange = () => setMascotOutfit(outfitSel.value, true);

  setupMascotSpeech();
  setMascotOutfit(loadStoredMascotOutfit(), false);
}

// --- Mascot speech bubble -------------------------------------------------
// Placeholder lines — English only for now, swap/extend freely; nothing
// else in the code assumes this exact list.
const MASCOT_PHRASES = [
  "Wheels up in 3... 2... 1!",
  "Scenery's looking crisp today.",
  "Clear skies ahead, captain.",
  "Don't mind me, just watching the radar.",
  "Beep boop. Still just a mascot.",
  "Cache looking nice and full!",
  "Is it just me, or is this horizon gorgeous today?",
  "Click me again, I dare you.",
  "Systems nominal. Vibes immaculate.",
  "I've got my eyes on you.",
  "Who's flying this thing, anyway?",
  "Just doing a little patrol of your screen.",
  "Tip: You can change the app background image in the settings menu",
  "I believe in you. You can accomplish anything you want in this life.",
  "I love when people comment 'FAA' on airplane videos that are not even in the USA, so quirky",
  "MIGgers rise up!",
  "Houston, we have a very good day.",
  "10/10, would fly again.",
  "Stealth is overrated",
  "Fun fact: F-35 is opensource",
  "Do you know what is the hardest question in programming?",
  "Helis are hard to fly",
  // Real setup/behavior tips, learned the hard way from actual flights.
  // Keep these accurate if the defaults they cite change.
  "Tip: 'Speed' mode trades sharpness for smoothness over dense cities, try it if big airports feel stuttery.",
  "Tip: a tile that goes blurry under heavy load stays blurry until it's evicted, fly far enough away and back to force a fresh build.",
  "Tip: stutter over a big city is usually your CPU decoding tiles, not your internet connection.",
  "Tip: chunks slower than 0.5s get served blurry so the view doesn't freeze, that's 'maxwait', tune it if you're on a slow connection.",
  "Tip: about 300 full-quality tiles can be open at once before new ones go blurry to protect memory. That's MaxOpenTiles.",
  "Tip: the memory watchdog steps in at 50% RAM used, warns loudly at 70%, and shuts down safely at 85%,before the OS ever has to kill me :(",
  "Tip: give it a minute over a new dense area before judging sharpness, cold tiles take longer to build than ones you've already flown over.",
  "It's never too late to get your PPL. You only get one life, go fly it.",
  "Short on cash for rudder pedals? A /diy/ pedals build is a great weekend project.",
  "I miss lighting up in the cockpit.",
  "The 'Black Box' is actually bright orange.",
  "Prior to 1926, a person could fly passengers or goods without obtaining a pilot's license.",
  "Give gliding a try, it's a lot of fun.",
  "WE GAAN!",
  "Everything I want to do is illegal!",
  "I'm 500 miles from my car rn.",
  "Krenemo kasnije, stignemo ranije.",
  "Don't succumb to hypoxia even if it feels good.",
  "Feeling a little sad? The FAA asks that you report it immediately, if that applies to you.",
  "Want to practice proper ATC communications? Give VATSIM or IVAO a try.",
  "When meeting your FBO: 'I need to request a service, please.'",
  "When meeting your FBO: open your jacket slightly to show her your pilot's gun.",
  "When meeting your FBO: 'They're my passengers, not yours.'",
  "I paid about $100K USD for my CPL and you tell me I can't have a single beer?",
  "I love motorcycles too.",
  "Маневрируй, маневрируй, давай!",
  "God bless you! and have a safe flight!"
];

const MASCOT_BUBBLE_DURATION_MS = 7000;
let mascotBubbleTimer = null;

function showMascotBubble() {
  const bubble = document.getElementById('mascot-bubble');
  const text = document.getElementById('mascot-bubble-text');
  text.textContent = MASCOT_PHRASES[Math.floor(Math.random() * MASCOT_PHRASES.length)];
  bubble.classList.remove('hidden');
  if (mascotBubbleTimer) clearTimeout(mascotBubbleTimer);
  mascotBubbleTimer = setTimeout(hideMascotBubble, MASCOT_BUBBLE_DURATION_MS);
}

function hideMascotBubble() {
  document.getElementById('mascot-bubble').classList.add('hidden');
  if (mascotBubbleTimer) {
    clearTimeout(mascotBubbleTimer);
    mascotBubbleTimer = null;
  }
}

function setupMascotSpeech() {
  document.getElementById('mascot-clickzone').onclick = showMascotBubble;
  document.getElementById('mascot-bubble').onclick = hideMascotBubble;
}

document.addEventListener('DOMContentLoaded', () => {
  setupDirBrowser();
  setupDetectXplane();
  setupLanguageSwitcher();
  setupThemeSwitcher();
  setupDemoModeToggle();
  setupSettingsPanel();
  setupUserGuide();
  setupBackgroundPicker();
  setupMascot();
  setupSaveDirtyTracking();

  document.getElementById('cfg-max_zoom').addEventListener('change', updateMaxZoomWarning);

  document.getElementById('btn-run').onclick = () => send('mount');
  document.getElementById('btn-stop').onclick = () =>
    confirmAction(t('confirm.stop_text'), () => send('stop'));
  document.getElementById('btn-restart').onclick = () =>
    confirmAction(t('confirm.restart_text'), () => send('restart'));
  document.getElementById('btn-shutdown').onclick = () =>
    confirmAction(t('confirm.shutdown_text'), () => send('shutdown'));
  document.getElementById('btn-clean-cache').onclick = () => send('clean_cache');
  document.getElementById('btn-refresh-regions').onclick = () => send('refresh_regions');

  document.getElementById('btn-save').onclick = () => {
    send('save_config', {
      xplane_path: pathFieldValue('cfg-xplane_path'),
      scenery_path: pathFieldValue('cfg-scenery_path'),
      cache_dir: pathFieldValue('cfg-cache_dir'),
      download_dir: pathFieldValue('cfg-download_dir'),
      file_cache_size_gb: parseInt(document.getElementById('cfg-file_cache_size_gb').value, 10),
      maptype_override: document.getElementById('cfg-maptype_override').value,
      min_zoom: parseInt(document.getElementById('cfg-min_zoom').value, 10),
      max_zoom: parseInt(document.getElementById('cfg-max_zoom').value, 10),
      maxwait: parseFloat(document.getElementById('cfg-maxwait').value),
      max_concurrent_builds: parseInt(document.getElementById('cfg-max_concurrent_builds').value, 10),
      fetch_threads: parseInt(document.getElementById('cfg-fetch_threads').value, 10),
      max_open_tiles: parseInt(document.getElementById('cfg-max_open_tiles').value, 10),
      performance_mode: document.getElementById('cfg-performance_mode').value,
      disable_coarse_fallback: document.getElementById('cfg-disable_coarse_fallback').checked,
      auto_open_ui: document.getElementById('cfg-auto_open_ui').checked,
    });
  };

  connect();
});
