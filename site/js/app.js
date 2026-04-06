'use strict';

// ─────────────────────────────────────────
// Constants
// ─────────────────────────────────────────
const API_BASE              = 'https://api.takina.io/sptt/v1';
const STEAM_CDN             = 'https://cdn.cloudflare.steamstatic.com/steam/apps';
const STEAM_STORE_API       = 'https://api.takina.io/proxy/steamstore/api/appdetails';
const STEAM_COMMUNITY_PROXY = 'https://api.takina.io/proxy/steamcommunity'
const STEAM_STORE_URL       = 'https://store.steampowered.com/app';

// ─────────────────────────────────────────
// State
// ─────────────────────────────────────────
const state = {
  steamId:        null,
  profile:        null,
  stats:          null,
  sessions:       [],
  activeSessions: [],

  page:       0,
  pageSize:   20,
  totalCount: 0,
  totalPages: 0,

  sortBy:  'utcstart',
  sortDir: 'desc',

  filters: {
    appId:       '',
    startFrom:   null,
    startTo:     null,
    endFrom:     null,
    endTo:       null,
    playtimeMin: '',
    playtimeMax: '',
  },

  // appId (number) → { name: string|null, headerImage: string }
  gameCache: new Map(),
};

// ─────────────────────────────────────────
// Utilities
// ─────────────────────────────────────────

/** Format a UTC ISO string as the user's local date+time. */
function fmtLocalTime(utcStr) {
  return new Date(utcStr).toLocaleString(undefined, {
    year:   'numeric',
    month:  'short',
    day:    'numeric',
    hour:   '2-digit',
    minute: '2-digit',
  });
}

/** Format the difference between two UTC strings as Xh Ym. */
function fmtDuration(startStr, endStr) {
  const ms       = new Date(endStr) - new Date(startStr);
  const totalMin = Math.max(0, Math.round(ms / 60_000));
  const h        = Math.floor(totalMin / 60);
  const m        = totalMin % 60;
  return h > 0 ? `${h}h ${m}m` : `${m}m`;
}

/** Format minutes (playtime_forever API field) as Xh Ym. */
function fmtPlaytime(minutes) {
  if (!minutes) return '—';
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  return h > 0 ? `${h}h ${m}m` : `${m}m`;
}

/**
 * Convert a date string (YYYY-MM-DD) to an RFC3339 UTC string.
 * boundary='start' clamps to 00:00:00 local, 'end' clamps to 23:59:59 local.
 */
function localToRFC3339(dateStr, boundary = 'start') {
  if (!dateStr) return null;
  const time = boundary === 'end' ? 'T23:59:59' : 'T00:00:00';
  return new Date(dateStr + time).toISOString();
}

/** Copy an App ID to clipboard and briefly flash the button green. */
function copyAppId(btn, appId) {
  navigator.clipboard.writeText(String(appId)).then(() => {
    btn.classList.add('copy-btn--ok');
    setTimeout(() => btn.classList.remove('copy-btn--ok'), 1200);
  });
}

/** Escape HTML to prevent XSS in rendered strings from API / Steam. */
function esc(str) {
  return String(str ?? '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

/** Thumbnail URL for a Steam app. */
function thumbUrl(appId) {
  return `${STEAM_CDN}/${appId}/header.jpg`;
}

// ─────────────────────────────────────────
// Backend API
// ─────────────────────────────────────────
async function apiFetch(path, params = {}) {
  const url = new URL(`${API_BASE}${path}`);
  for (const [k, v] of Object.entries(params)) {
    if (v !== null && v !== undefined && v !== '') {
      url.searchParams.set(k, String(v));
    }
  }
  const res = await fetch(url.toString());
  if (!res.ok) throw new Error(`API ${res.status}: ${path}`);
  return res.json();
}

async function apiStats(steamId) {
  return apiFetch(`/users/${steamId}/stats`);
}

async function apiActiveSessions(steamId) {
  return apiFetch(`/users/${steamId}/active_sessions`);
}

async function apiSessions(steamId) {
  const f = state.filters;
  return apiFetch(`/users/${steamId}/sessions`, {
    page:           state.page,
    page_size:      state.pageSize,
    sort_by:        state.sortBy,
    sort_dir:       state.sortDir,
    app_id:         f.appId || '',
    utcstart_from:  f.startFrom  ?? '',
    utcstart_to:    f.startTo    ?? '',
    utcend_from:    f.endFrom    ?? '',
    utcend_to:      f.endTo      ?? '',
    // UI shows hours; API expects minutes
    playtime_min: f.playtimeMin !== '' ? Math.round(parseFloat(f.playtimeMin) * 60) : '',
    playtime_max: f.playtimeMax !== '' ? Math.round(parseFloat(f.playtimeMax) * 60) : '',
  });
}

// ─────────────────────────────────────────
// Steam: Profile XML
// ─────────────────────────────────────────

/**
 * Fetch a Steam profile via the Community XML endpoint through proxy.
 */
async function fetchSteamProfile(steamId) {
  const url  = `${STEAM_COMMUNITY_PROXY}/profiles/${steamId}/?xml=1`;

  const res  = await fetch(url, { signal: AbortSignal.timeout(8_000) });

  const parser = new DOMParser();
  const text   = await res.text();
  const doc    = parser.parseFromString(text ?? '', 'text/xml');

  const txt = (tag) => doc.querySelector(tag)?.textContent?.trim() ?? '';

  return {
    steamId64:    txt('steamID64'),
    displayName:  txt('steamID'),
    onlineState:  txt('onlineState'),   // 'online' | 'offline' | 'in-game'
    stateMessage: txt('stateMessage'),
    privacyState: txt('privacyState'),  // 'public' | 'private' | 'friendsonly'
    visibilityState: parseInt(txt('visibilityState'), 10) || 1, // 3 = public
    avatarIcon:   txt('avatarIcon'),
    avatarMedium: txt('avatarMedium'),
    avatarFull:   txt('avatarFull'),
    vacBanned:    txt('vacBanned') === '1',
    isLimited:    txt('isLimitedAccount') === '1',
    memberSince:  txt('memberSince'),
    location:     txt('location'),
    realname:     txt('realname'),
    customURL:    txt('customURL'),
  };
}

// ─────────────────────────────────────────
// Steam: App Details (cached)
// ─────────────────────────────────────────

/**
 * Resolve a Steam appId to { name, headerImage }.
 * Results are cached in state.gameCache for the lifetime of the page.
 * Overlapping requests for the same id are deduplicated via a pending Map.
 */
const _pendingDetails = new Map(); // appId → Promise

async function getGameDetails(appId) {
  if (state.gameCache.has(appId)) return state.gameCache.get(appId);
  if (_pendingDetails.has(appId)) return _pendingDetails.get(appId);

  const promise = (async () => {
    const fallback = { name: null, headerImage: thumbUrl(appId) };
    try {
      const res  = await fetch(
        `${STEAM_STORE_API}?appids=${appId}&filters=basic`,
        { signal: AbortSignal.timeout(5_000) }
      );
      const data = await res.json();
      if (data[appId]?.success) {
        const d = data[appId].data;
        fallback.name        = d.name          || null;
        fallback.headerImage = d.header_image  || fallback.headerImage;
      }
    } catch {
      // Network or parse error — use fallback with CDN thumb
    }
    state.gameCache.set(appId, fallback);
    _pendingDetails.delete(appId);
    return fallback;
  })();

  _pendingDetails.set(appId, promise);
  return promise;
}

// ─────────────────────────────────────────
// Render: Profile Card
// ─────────────────────────────────────────
function renderProfile() {
  const el = document.getElementById('profile-card');
  const p  = state.profile;

  if (!p) {
    el.innerHTML = `
      <div class="profile-head">
        <div class="profile-avatar-placeholder">?</div>
        <div class="profile-name-block">
          <div class="profile-name">${esc(state.steamId)}</div>
          <div class="profile-realname" style="color:var(--yellow)">Profile unavailable</div>
        </div>
      </div>`;
    return;
  }

  const isPublic = p.visibilityState === 3;
  const avatar   = p.avatarFull || p.avatarMedium || p.avatarIcon;

  const avatarEl = avatar
    ? `<img class="profile-avatar" src="${esc(avatar)}" alt="avatar"
            onerror="this.classList.add('hidden');this.nextElementSibling.classList.remove('hidden')">
       <div class="profile-avatar-placeholder hidden">?</div>`
    : `<div class="profile-avatar-placeholder">?</div>`;

  const statusClass = {
    'online':  'status-online',
    'in-game': 'status-ingame',
  }[p.onlineState] ?? 'status-offline';

  const statusLabel = p.onlineState === 'in-game'
    ? 'In-Game'
    // ? (p.stateMessage || 'In-Game')
    : (p.onlineState === 'online' ? 'Online' : 'Offline');

  const metaRows = [
    p.realname    && `<div class="meta-row"><span class="meta-key">Name</span><span class="meta-val">${esc(p.realname)}</span></div>`,
    p.location    && `<div class="meta-row"><span class="meta-key">Location</span><span class="meta-val">${esc(p.location)}</span></div>`,
    p.memberSince && `<div class="meta-row"><span class="meta-key">Member</span><span class="meta-val">${esc(p.memberSince)}</span></div>`,
    p.customURL   && `<div class="meta-row"><span class="meta-key">Profile</span>
                        <span class="meta-val"><a href="https://steamcommunity.com/id/${esc(p.customURL)}" target="_blank" rel="noreferrer">
                          /id/${esc(p.customURL)}
                        </a></span></div>`,
    p.steamId64   && `<div class="meta-row"><span class="meta-key">SteamID</span><span class="meta-val" style="font-size:11px;font-family:monospace">${esc(p.steamId64)}</span></div>`,
  ].filter(Boolean).join('');

  el.innerHTML = `
    <div class="profile-head">
      ${avatarEl}
      <div class="profile-name-block">
        <div class="profile-name"><a href="https://steamcommunity.com/profiles/${esc(state.steamId)}" target="_blank" rel="noreferrer">${esc(p.displayName || state.steamId)}</a></div>
        <span class="status-badge ${statusClass}">
          <span class="dot"></span>${esc(statusLabel)}
        </span>
      </div>
    </div>
    <div class="profile-meta">
      ${metaRows}
      ${!isPublic ? `<div class="private-note">⚠ Private / friends-only profile</div>` : ''}
      ${p.vacBanned ? `<div class="vac-badge">⚑ VAC Banned</div>` : ''}
    </div>`;
}

// ─────────────────────────────────────────
// Render: Stats Grid
// ─────────────────────────────────────────
function renderStats() {
  const el = document.getElementById('stats-grid');

  // Compute page-level session duration sum
  let pageDurMins = 0;
  for (const s of state.sessions) {
    const ms = new Date(s.utc_end) - new Date(s.utc_start);
    pageDurMins += Math.max(0, Math.round(ms / 60_000));
  }

  const totalSessions = state.stats?.total_sessions ?? '—';

  el.innerHTML = `
    <div class="stat-card">
      <div class="stat-label">Total Sessions</div>
      <div class="stat-value">${esc(String(totalSessions))}</div>
    </div>
    <div class="stat-card">
      <div class="stat-label">Filtered</div>
      <div class="stat-value">${state.totalCount}</div>
      <div class="stat-sub">matching sessions</div>
    </div>
    <div class="stat-card">
      <div class="stat-label">Page</div>
      <div class="stat-value">${state.page + 1}<span style="font-size:14px;color:var(--txt-muted);font-weight:400"> / ${state.totalPages || '—'}</span></div>
    </div>
    <div class="stat-card">
      <div class="stat-label">Page Playtime</div>
      <div class="stat-value" style="font-size:20px">${fmtPlaytime(pageDurMins)}</div>
      <div class="stat-sub">sum of this page</div>
    </div>
    <div class="stat-card">
      <div class="stat-label">Active Now</div>
      <div class="stat-value" style="color:${state.activeSessions.length ? 'var(--green)' : 'var(--txt-muted)'}">
        ${state.activeSessions.length}
      </div>
    </div>`;
}

// ─────────────────────────────────────────
// Render: Active Sessions
// ─────────────────────────────────────────
function renderActiveSessions() {
  const section = document.getElementById('active-section');
  const list    = document.getElementById('active-list');

  if (!state.activeSessions.length) {
    section.classList.add('hidden');
    return;
  }

  section.classList.remove('hidden');

  list.innerHTML = state.activeSessions.map(s => `
    <div class="active-card">
      <img class="active-thumb" id="active-thumb-${s.app_id}"
           src="${thumbUrl(s.app_id)}" alt=""
           onerror="this.classList.add('hidden');document.getElementById('active-thumb-ph-${s.app_id}').classList.remove('hidden')">
      <div class="active-thumb-ph hidden" id="active-thumb-ph-${s.app_id}">?</div>
      <div class="active-info">
        <div class="active-name" id="active-name-${s.app_id}">App ${s.app_id}</div>
        <div class="active-since">Since ${fmtLocalTime(s.utc_start)}</div>
      </div>
    </div>`).join('');

  // Resolve names asynchronously
  for (const s of state.activeSessions) {
    getGameDetails(s.app_id).then(g => {
      const el = document.getElementById(`active-name-${s.app_id}`);
      if (el && g?.name) el.textContent = g.name;
    });
  }
}

// ─────────────────────────────────────────
// Render: Sessions Table
// ─────────────────────────────────────────
function renderSessions() {
  const tbody = document.getElementById('sessions-body');

  if (!state.sessions.length) {
    tbody.innerHTML = `<tr><td colspan="6" class="table-empty">No sessions found.</td></tr>`;
    renderStats();
    return;
  }

  // Build rows with placeholder game names first (fast paint)
  tbody.innerHTML = state.sessions.map((s, i) => `
    <tr>
      <td>
        <div class="game-cell">
          <img class="game-thumb" src="${thumbUrl(s.app_id)}" alt=""
               onerror="this.style.visibility='hidden'">
          <span class="game-name">
            <a href="${STEAM_STORE_URL}/${s.app_id}" target="_blank" rel="noreferrer"
               id="gname-${i}" class="game-name-ph">App ${s.app_id}</a>
          </span>
        </div>
      </td>
      <td class="cell-appid">
        <button class="copy-btn" title="Copy App ID" onclick="copyAppId(this, ${s.app_id})">
          <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
            <rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/>
          </svg>
        </button>
        <span class="appid-val">${s.app_id}</span>
      </td>
      <td class="cell-time">${fmtLocalTime(s.utc_start)}</td>
      <td class="cell-time">${fmtLocalTime(s.utc_end)}</td>
      <td class="cell-dur">${fmtDuration(s.utc_start, s.utc_end)}</td>
      <td class="cell-playtime">${fmtPlaytime(s.playtime_forever)}</td>
    </tr>`).join('');

  renderStats();
  renderSortIndicators();

  // Resolve game names and update cells in place
  state.sessions.forEach((s, i) => {
    getGameDetails(s.app_id).then(g => {
      const link = document.getElementById(`gname-${i}`);
      if (!link) return;
      if (g?.name) {
        link.textContent = g.name;
        link.classList.remove('game-name-ph');
      }
    });
  });
}

// ─────────────────────────────────────────
// Render: Sort Indicators
// ─────────────────────────────────────────
const SORT_COLS = ['appid', 'utcstart', 'utcend', 'playtime_forever'];

function renderSortIndicators() {
  for (const col of SORT_COLS) {
    const ind = document.getElementById(`si-${col}`);
    const th  = ind?.closest('th');
    if (!ind || !th) continue;

    if (state.sortBy === col) {
      ind.textContent = state.sortDir === 'asc' ? '▲' : '▼';
      th.classList.add('sort-active');
    } else {
      ind.textContent = '↕';
      th.classList.remove('sort-active');
    }
  }
}

// ─────────────────────────────────────────
// Render: Pagination
// ─────────────────────────────────────────
function renderPagination() {
  const el = document.getElementById('pagination');
  const { page, totalPages, totalCount } = state;

  if (totalPages <= 1) { el.innerHTML = ''; return; }

  // Build a windowed page list with ellipsis
  const pages = buildPageList(page, totalPages);

  const btn = (p, label, extra = '') =>
    `<button class="page-btn ${p === page ? 'active' : ''}" data-page="${p}" ${extra}>${label}</button>`;

  el.innerHTML = [
    btn(page - 1, '‹', page === 0 ? 'disabled' : ''),
    ...pages.map(p =>
      p === '...'
        ? `<span class="page-ellipsis">…</span>`
        : btn(p, p + 1)
    ),
    btn(page + 1, '›', page >= totalPages - 1 ? 'disabled' : ''),
    `<span class="page-info">${totalCount} sessions</span>`,
  ].join('');

  el.querySelectorAll('.page-btn:not([disabled])').forEach(b => {
    b.addEventListener('click', () => {
      state.page = parseInt(b.dataset.page, 10);
      loadSessions();
    });
  });
}

function buildPageList(current, total) {
  if (total <= 7) return Array.from({ length: total }, (_, i) => i);

  const pages = [0];
  const lo = Math.max(1, current - 2);
  const hi = Math.min(total - 2, current + 2);

  if (lo > 1)         pages.push('...');
  for (let i = lo; i <= hi; i++) pages.push(i);
  if (hi < total - 2) pages.push('...');
  pages.push(total - 1);
  return pages;
}

// ─────────────────────────────────────────
// Data Loading
// ─────────────────────────────────────────

/** Load all data for a given Steam ID (first time or on ID change). */
async function loadUser(steamId) {
  state.steamId       = steamId;
  state.page          = 0;
  state.profile       = null;
  state.stats         = null;
  state.sessions      = [];
  state.activeSessions = [];

  // Reflect in URL without reloading
  const url = new URL(window.location.href);
  url.searchParams.set('steamid', steamId);
  window.history.replaceState(null, '', url);

  // Show user view skeleton
  document.getElementById('empty-state').classList.add('hidden');
  const userView = document.getElementById('user-view');
  userView.classList.remove('hidden');

  // Profile card placeholder
  document.getElementById('profile-card').innerHTML = `
    <div class="profile-head">
      <div class="profile-avatar-placeholder skeleton" style="flex-shrink:0"></div>
      <div style="flex:1">
        <div class="skeleton" style="height:14px;width:60%;margin-bottom:8px;border-radius:4px"></div>
        <div class="skeleton" style="height:11px;width:40%;border-radius:4px"></div>
      </div>
    </div>`;
  document.getElementById('stats-grid').innerHTML = '';

  // Fetch profile, stats, and active sessions in parallel; don't let any
  // single failure block the rest of the UI.
  const [profileResult, statsResult, activeResult] = await Promise.allSettled([
    fetchSteamProfile(steamId),
    apiStats(steamId),
    apiActiveSessions(steamId),
  ]);

  state.profile       = profileResult.status === 'fulfilled' ? profileResult.value  : null;
  state.stats         = statsResult.status   === 'fulfilled' ? statsResult.value    : null;
  state.activeSessions = activeResult.status === 'fulfilled'
    ? (activeResult.value?.data ?? [])
    : [];

  if (profileResult.status === 'rejected') {
    console.warn('Steam profile fetch failed:', profileResult.reason);
  }

  renderProfile();
  renderActiveSessions();

  await loadSessions();
}

/** Reload the sessions table with current sort/filter/page. */
async function loadSessions() {
  if (!state.steamId) return;

  setTableLoading(true);

  try {
    const data       = await apiSessions(state.steamId);
    state.sessions   = data.data        ?? [];
    state.page       = data.page        ?? 0;
    state.pageSize   = data.page_size   ?? 20;
    state.totalCount = data.total_count ?? 0;
    state.totalPages = data.total_pages ?? 0;
  } catch (err) {
    console.error('Sessions fetch failed:', err);
    state.sessions   = [];
    state.totalCount = 0;
    state.totalPages = 0;
  }

  setTableLoading(false);
  setCustomSelectValue(document.getElementById('page-size-select'), String(state.pageSize));
  renderSessions();
  renderPagination();
}

function setTableLoading(on) {
  document.getElementById('table-overlay').classList.toggle('hidden', !on);
}

// ─────────────────────────────────────────
// Filters
// ─────────────────────────────────────────
function readFilters() {
  return {
    appId:       document.getElementById('f-appid').value.trim(),
    startFrom:   localToRFC3339(document.getElementById('f-start-from').value, 'start'),
    startTo:     localToRFC3339(document.getElementById('f-start-to').value,   'end'),
    endFrom:     localToRFC3339(document.getElementById('f-end-from').value,   'start'),
    endTo:       localToRFC3339(document.getElementById('f-end-to').value,     'end'),
    playtimeMin: document.getElementById('f-pt-min').value,
    playtimeMax: document.getElementById('f-pt-max').value,
  };
}

function applyFilters() {
  state.filters = readFilters();
  state.page    = 0;
  loadSessions();
}

function resetFilters() {
  ['f-appid','f-start-from','f-start-to','f-end-from','f-end-to','f-pt-min','f-pt-max']
    .forEach(id => { document.getElementById(id).value = ''; });
  state.filters = { appId:'', startFrom:null, startTo:null, endFrom:null, endTo:null, playtimeMin:'', playtimeMax:'' };
  state.page    = 0;
  loadSessions();
}

// ─────────────────────────────────────────
// Custom Select
// ─────────────────────────────────────────

/**
 * Initialise a .custom-select element.
 * onChange(value) is called whenever the user picks an option.
 */
function initCustomSelect(el, onChange) {
  const trigger  = el.querySelector('.custom-select__trigger');
  const label    = el.querySelector('.custom-select__label');
  const options  = el.querySelectorAll('.custom-select__option');

  // Mark initial selected option
  const currentVal = el.dataset.value;
  options.forEach(o => o.classList.toggle('selected', o.dataset.value === currentVal));

  // Toggle open/closed
  trigger.addEventListener('click', e => {
    e.stopPropagation();
    el.classList.toggle('open');
  });

  // Pick an option
  options.forEach(o => {
    o.addEventListener('click', e => {
      e.stopPropagation();
      const value = o.dataset.value;
      label.textContent = o.textContent;
      el.dataset.value  = value;
      options.forEach(opt => opt.classList.toggle('selected', opt === o));
      el.classList.remove('open');
      onChange(value);
    });
  });

  // Keyboard: Enter/Space toggles, Escape closes, Up/Down navigates
  el.addEventListener('keydown', e => {
    const isOpen = el.classList.contains('open');
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      el.classList.toggle('open');
    } else if (e.key === 'Escape') {
      el.classList.remove('open');
    } else if ((e.key === 'ArrowDown' || e.key === 'ArrowUp') && isOpen) {
      e.preventDefault();
      const opts  = [...options];
      const idx   = opts.findIndex(o => o.classList.contains('selected'));
      const next  = e.key === 'ArrowDown'
        ? opts[Math.min(idx + 1, opts.length - 1)]
        : opts[Math.max(idx - 1, 0)];
      next.click();
    }
  });

  // Close when clicking outside
  document.addEventListener('click', () => el.classList.remove('open'));
}

/** Programmatically update a custom-select's displayed value. */
function setCustomSelectValue(el, value) {
  const label   = el.querySelector('.custom-select__label');
  const options = el.querySelectorAll('.custom-select__option');
  options.forEach(o => {
    const match = o.dataset.value === value;
    o.classList.toggle('selected', match);
    if (match) label.textContent = o.textContent;
  });
  el.dataset.value = value;
}

// ─────────────────────────────────────────
// Event Handlers + Init
// ─────────────────────────────────────────
function init() {
  // ── Search form ──
  document.getElementById('search-form').addEventListener('submit', e => {
    e.preventDefault();
    const val = document.getElementById('steamid-input').value.trim();
    if (val) loadUser(val);
  });

  // ── Sortable column headers ──
  document.querySelectorAll('th.sortable').forEach(th => {
    th.addEventListener('click', () => {
      const col = th.dataset.col;
      if (state.sortBy === col) {
        state.sortDir = state.sortDir === 'asc' ? 'desc' : 'asc';
      } else {
        state.sortBy  = col;
        state.sortDir = 'asc';
      }
      state.page = 0;
      loadSessions();
    });
  });

  // ── Custom page-size dropdown ──
  initCustomSelect(document.getElementById('page-size-select'), value => {
    state.pageSize = parseInt(value, 10);
    state.page     = 0;
    loadSessions();
  });

  // ── Filter toggle ──
  const filterToggle = document.getElementById('filter-toggle');
  const filterPanel  = document.getElementById('filter-panel');
  filterToggle.addEventListener('click', () => {
    const open = filterPanel.classList.toggle('hidden') === false;
    filterToggle.classList.toggle('active', open);
    filterToggle.setAttribute('aria-expanded', String(open));
  });

  // ── Filter apply / reset ──
  document.getElementById('btn-apply').addEventListener('click', applyFilters);
  document.getElementById('btn-reset').addEventListener('click', resetFilters);

  // ── Bootstrap from URL param ──
  const params  = new URLSearchParams(window.location.search);
  const steamId = params.get('steamid');
  if (steamId) {
    document.getElementById('steamid-input').value = steamId;
    loadUser(steamId);
  }
}

document.addEventListener('DOMContentLoaded', init);
