/*
 * BlockTheSpot panel for the Spotify desktop client.
 *
 * Added to Apps/xpui.spa by the installer, which prepends a window.__BTS_INFO__ line with the
 * details it knows (app version, kit, Spotify version, updater state). Everything here is DOM and
 * CSS only: no React internals, no imports, and every entry point is wrapped, so a selector that
 * stops matching costs a hidden element, never a broken client.
 *
 * Selectors come from data-testid values read out of the shipped bundle.
 */
(function () {
  'use strict';

  var INFO = (window.__BTS_INFO__ || {});
  var STORE = 'blockthespot:settings';
  var STYLE_ID = 'blockthespot-style';
  var PANEL_ID = 'blockthespot-panel';

  var GROUPS = {
    ads: {
      label: 'Hide ad slots',
      hint: 'Home, player, canvas and podcast ad containers, on top of the patched ad state.',
      value: true,
      selectors: [
        'home-ads-container', 'home-ad-card', 'home-ad-video-asset', 'home-ad-display-asset-fallback',
        'home-dsa-message-banner', 'embedded-ad', 'embedded-ad-carousel', 'embedded-ad-html-display',
        'embedded-ad-html-creative', 'ad-companion-card', 'canvas-ad-container', 'canvas-ad-player',
        'video-player-ad-wrapper', 'standalone-video-ad-player', 'ads-video-player-npv',
        'scroll-card-ad', 'survey-ad', 'survey-option-ad', 'leavebehind-advertiser'
      ]
    },
    upsell: {
      label: 'Hide Premium upsell',
      hint: 'The upgrade button in the top bar and the Premium hero block.',
      value: true,
      selectors: ['upgrade-button', 'premium-hero']
    },
    downloadApp: {
      label: 'Hide "download the app" prompts',
      hint: 'Off by default: some people use this button.',
      value: false,
      selectors: ['upsell-download-app-button']
    },
    updateNag: {
      label: 'Hide the "update available" badge',
      hint: 'Updates are blocked, so the badge only nags.',
      value: true,
      selectors: ['user-widget-update-available']
    }
  };

  function read() {
    var saved = {};
    try { saved = JSON.parse(localStorage.getItem(STORE) || '{}') || {}; } catch (e) { saved = {}; }
    var state = {};
    for (var key in GROUPS) state[key] = typeof saved[key] === 'boolean' ? saved[key] : GROUPS[key].value;
    return state;
  }

  function write(state) {
    try { localStorage.setItem(STORE, JSON.stringify(state)); } catch (e) { /* private mode */ }
  }

  function applyCss(state) {
    var selectors = [];
    for (var key in GROUPS) {
      if (!state[key]) continue;
      GROUPS[key].selectors.forEach(function (id) { selectors.push('[data-testid="' + id + '"]'); });
    }
    var style = document.getElementById(STYLE_ID);
    if (!style) {
      style = document.createElement('style');
      style.id = STYLE_ID;
      document.head.appendChild(style);
    }
    style.textContent = selectors.length ? selectors.join(',') + '{display:none !important;}' : '';
  }

  /* ---- small DOM helpers, so the panel needs no framework ---- */
  function el(tag, props, children) {
    var node = document.createElement(tag);
    if (props) for (var k in props) {
      if (k === 'style') node.setAttribute('style', props[k]);
      else if (k === 'text') node.textContent = props[k];
      else node.setAttribute(k, props[k]);
    }
    (children || []).forEach(function (child) { if (child) node.appendChild(child); });
    return node;
  }

  var CARD = 'margin:0 0 24px;padding:16px 20px;border:1px solid rgba(255,255,255,.12);border-radius:8px;';
  var MUTED = 'opacity:.62;font-size:12px;line-height:1.5;margin:4px 0 0;';
  var ROW = 'display:flex;align-items:flex-start;justify-content:space-between;gap:16px;padding:10px 0;';

  function toggleRow(key, state, onChange) {
    var group = GROUPS[key];
    var input = el('input', { type: 'checkbox', id: 'bts-' + key, style: 'margin-top:3px;flex:0 0 auto;' });
    input.checked = !!state[key];
    input.addEventListener('change', function () { onChange(key, input.checked); });
    return el('div', { style: ROW }, [
      el('label', { for: 'bts-' + key, style: 'flex:1 1 auto;cursor:pointer;' }, [
        el('div', { text: group.label }),
        el('p', { style: MUTED, text: group.hint })
      ]),
      input
    ]);
  }

  /* ---- A/B flags: read whatever this build keeps in localStorage, never invent it ---- */
  function readFlags() {
    var found = [];
    try {
      for (var i = 0; i < localStorage.length; i++) {
        var key = localStorage.key(i);
        if (!key || !/remote|config|feature|flag|experiment|ab[-_:]/i.test(key)) continue;
        var raw = localStorage.getItem(key);
        if (!raw || raw.length > 400000) continue;
        var parsed;
        try { parsed = JSON.parse(raw); } catch (e) { continue; }
        if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) continue;
        for (var name in parsed) {
          var value = parsed[name];
          if (typeof value === 'boolean' || typeof value === 'string' || typeof value === 'number') {
            found.push({ store: key, name: name, value: value });
          }
        }
      }
    } catch (e) { /* storage unavailable */ }
    return found.sort(function (a, b) { return a.name < b.name ? -1 : 1; });
  }

  function setFlag(entry, value) {
    try {
      var parsed = JSON.parse(localStorage.getItem(entry.store) || '{}');
      parsed[entry.name] = value;
      localStorage.setItem(entry.store, JSON.stringify(parsed));
      return true;
    } catch (e) { return false; }
  }

  function flagsSection() {
    var flags = readFlags();
    var body = el('div', { style: 'max-height:320px;overflow:auto;margin-top:8px;' });
    if (!flags.length) {
      body.appendChild(el('p', { style: MUTED, text: 'No feature flags are stored locally on this build, so there is nothing to override here.' }));
    } else {
      var filter = el('input', {
        type: 'search', placeholder: 'Filter ' + flags.length + ' flags…',
        style: 'width:100%;margin:8px 0;padding:6px 8px;border-radius:4px;border:1px solid rgba(255,255,255,.2);background:transparent;color:inherit;'
      });
      var list = el('div');
      function render() {
        list.textContent = '';
        var needle = (filter.value || '').toLowerCase();
        flags.filter(function (f) { return !needle || f.name.toLowerCase().indexOf(needle) >= 0; })
          .slice(0, 200)
          .forEach(function (entry) {
            var control;
            if (typeof entry.value === 'boolean') {
              control = el('input', { type: 'checkbox' });
              control.checked = entry.value;
              control.addEventListener('change', function () {
                if (!setFlag(entry, control.checked)) control.checked = entry.value;
                else entry.value = control.checked;
              });
            } else {
              control = el('span', { style: 'opacity:.7;font-size:12px;max-width:220px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;', text: String(entry.value) });
            }
            list.appendChild(el('div', { style: ROW.replace('10px', '6px') }, [
              el('code', { style: 'font-size:12px;word-break:break-all;flex:1 1 auto;', text: entry.name }),
              control
            ]));
          });
      }
      filter.addEventListener('input', render);
      render();
      body.appendChild(filter);
      body.appendChild(list);
    }

    var summary = el('summary', { style: 'cursor:pointer;font-weight:600;' , text: 'A/B and feature flags' });
    var details = el('details', {}, [
      summary,
      el('p', { style: MUTED, text: 'Experimental. These are read from this client’s local storage; Spotify decides most of them on the server, so an override here may be ignored or reset.' }),
      body
    ]);
    return details;
  }

  function statusLine() {
    var bits = ['BlockTheSpot is active'];
    if (INFO.kit) bits.push(INFO.kit + ' kit');
    if (INFO.spotifyVersion) bits.push('Spotify ' + INFO.spotifyVersion);
    if (INFO.appVersion) bits.push('installer v' + INFO.appVersion);
    bits.push(INFO.updatesBlocked ? 'auto-update blocked' : 'auto-update allowed');
    return bits.join(' · ');
  }

  function buildPanel() {
    var state = read();
    function onChange(key, value) { state[key] = value; write(state); applyCss(state); }

    var toggles = el('div');
    for (var key in GROUPS) toggles.appendChild(toggleRow(key, state, onChange));

    return el('section', { id: PANEL_ID, style: CARD }, [
      el('h2', { style: 'margin:0;font-size:16px;', text: 'BlockTheSpot' }),
      el('p', { style: MUTED.replace('.62', '.8'), text: statusLine() }),
      toggles,
      flagsSection(),
      el('p', { style: MUTED, text: 'Auto-update is blocked by the installer, not from here: it locks the folder Spotify downloads updates into. Run the installer again and choose Restore to undo everything.' })
    ]);
  }

  /* ---- placement ---- */
  function onSettings() {
    return /preferences|settings/i.test(location.pathname || '');
  }

  function insertPanel() {
    try {
      if (!onSettings()) return;
      if (document.getElementById(PANEL_ID)) return;
      // The settings body is the scroll container holding the option rows; fall back to main.
      var host = document.querySelector('main [data-overlayscrollbars-contents], main .main-view-container__scroll-node-child, main');
      if (!host) return;
      host.insertBefore(buildPanel(), host.firstChild);
    } catch (e) { /* leave the page alone */ }
  }

  // "BlockTheSpot is active" next to whatever shows the Spotify version (the About area/dialog).
  function markAbout() {
    try {
      var marker = 'bts-about-marker';
      var nodes = document.querySelectorAll('main p, main span, [role="dialog"] p, [role="dialog"] span');
      for (var i = 0; i < nodes.length; i++) {
        var node = nodes[i];
        if (node.querySelector && node.querySelector('.' + marker)) continue;
        var text = (node.textContent || '').trim();
        if (!/^(Spotify\s+)?(for\s+\w+\s+)?\d+\.\d+\.\d+\.\d+/.test(text)) continue;
        if (node.parentNode && node.parentNode.querySelector('.' + marker)) continue;
        var line = el('div', { class: marker, style: 'opacity:.8;font-size:12px;margin-top:4px;', text: statusLine() });
        node.parentNode.insertBefore(line, node.nextSibling);
        return;
      }
    } catch (e) { /* no About visible */ }
  }

  function tick() { insertPanel(); markAbout(); }

  function start() {
    try {
      applyCss(read());
      tick();
      var observer = new MutationObserver(function () {
        clearTimeout(start.timer);
        start.timer = setTimeout(tick, 150);
      });
      observer.observe(document.body, { childList: true, subtree: true });
    } catch (e) { /* nothing else runs */ }
  }

  try {
    if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', start);
    else start();
  } catch (e) { /* never surface */ }
})();
