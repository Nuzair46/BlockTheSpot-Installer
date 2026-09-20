/*
 * BlockTheSpot panel for the Spotify desktop client.
 *
 * Added to Apps/xpui.spa by the installer, which prepends a window.__BTS_INFO__ line with the
 * details it knows (app version, kit, Spotify version, updater state). Everything here is DOM and
 * CSS only: no React internals, no imports, and every entry point is wrapped, so a selector that
 * stops matching costs a hidden element, never a broken client.
 *
 * Anchors and selectors are data-testid values read out of the shipped bundle:
 *   settings-page          the settings container, on the /preferences route
 *   inAppMessageContainer  where the client appends promo popups (xpui-root-dialogs)
 * Switching an option re-writes one <style> element and takes effect immediately; only loading
 * this file needs Spotify to have been started once after installing.
 */
(function () {
  'use strict';

  var INFO = (window.__BTS_INFO__ || {});
  var STORE = 'blockthespot:settings';
  var STYLE_ID = 'blockthespot-style';
  var PANEL_ID = 'blockthespot-panel';
  var POPUP = 'inAppMessageContainer';

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
    popups: {
      label: 'Close promo popups',
      hint: 'The "months of Premium" style dialogs, removed as soon as the client opens one.',
      value: true,
      selectors: [POPUP, 'sponsored-recommendation-modal-trigger']
    },
    upsell: {
      label: 'Hide Premium upsell',
      hint: 'The upgrade button in the top bar and the Premium hero block.',
      value: true,
      selectors: ['upgrade-button', 'premium-hero']
    },
    updateNag: {
      label: 'Hide the "update available" badge',
      hint: 'Updates are blocked, so the badge only nags.',
      value: true,
      selectors: ['user-widget-update-available']
    }
  };

  var state = {};

  function read() {
    var saved = {};
    try { saved = JSON.parse(localStorage.getItem(STORE) || '{}') || {}; } catch (e) { saved = {}; }
    var result = {};
    for (var key in GROUPS) result[key] = typeof saved[key] === 'boolean' ? saved[key] : GROUPS[key].value;
    return result;
  }

  function write() {
    try { localStorage.setItem(STORE, JSON.stringify(state)); } catch (e) { /* private mode */ }
  }

  function applyCss() {
    var selectors = [];
    for (var key in GROUPS) {
      if (!state[key]) continue;
      GROUPS[key].selectors.forEach(function (id) { selectors.push('[data-testid="' + id + '"]'); });
    }
    var style = document.getElementById(STYLE_ID);
    if (!style) {
      style = document.createElement('style');
      style.id = STYLE_ID;
      (document.head || document.documentElement).appendChild(style);
    }
    style.textContent = selectors.length ? selectors.join(',') + '{display:none !important;}' : '';
  }

  // The client fills this container with a promo and empties it itself on cleanup, so emptying it
  // early is an operation it already performs. Hiding alone can leave the dialog holding focus.
  function clearPopups() {
    if (!state.popups) return;
    try {
      var nodes = document.querySelectorAll('[data-testid="' + POPUP + '"]');
      for (var i = 0; i < nodes.length; i++) {
        if (nodes[i].childNodes.length) nodes[i].innerHTML = '';
      }
    } catch (e) { /* leave it to the CSS rule */ }
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

  // Spotify's own custom properties, so the panel follows the client's theme.
  var CARD = 'margin:0 0 24px;padding:16px 20px;border-radius:8px;background:var(--background-elevated-base,rgba(255,255,255,.06));';
  var MUTED = 'color:var(--text-subdued,#a7a7a7);font-size:12px;line-height:1.5;margin:4px 0 0;';
  var ROW = 'display:flex;align-items:flex-start;justify-content:space-between;gap:16px;padding:10px 0;';

  function toggleRow(key) {
    var group = GROUPS[key];
    var input = el('input', { type: 'checkbox', id: 'bts-' + key, style: 'margin-top:3px;flex:0 0 auto;' });
    input.checked = !!state[key];
    input.addEventListener('change', function () {
      state[key] = input.checked;
      write();
      applyCss();
      clearPopups();
    });
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
        style: 'width:100%;margin:8px 0;padding:6px 8px;border-radius:4px;border:1px solid var(--background-elevated-highlight,rgba(255,255,255,.2));background:transparent;color:inherit;'
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
              control = el('span', { style: 'color:var(--text-subdued,#a7a7a7);font-size:12px;max-width:220px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;', text: String(entry.value) });
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
    return el('details', {}, [
      el('summary', { style: 'cursor:pointer;font-weight:600;', text: 'A/B and feature flags' }),
      el('p', { style: MUTED, text: 'Experimental. These are read from this client’s local storage; Spotify decides most of them on the server, so an override here may be ignored or reset.' }),
      body
    ]);
  }

  function statusLine() {
    var bits = ['BlockTheSpot is active'];
    if (INFO.kit) bits.push(INFO.kit + ' kit');
    if (INFO.spotifyVersion) bits.push('Spotify ' + INFO.spotifyVersion);
    if (INFO.appVersion) bits.push('installer ' + INFO.appVersion);
    bits.push(INFO.updatesBlocked ? 'auto-update blocked' : 'auto-update allowed');
    return bits.join(' · ');
  }

  function buildPanel() {
    var toggles = el('div');
    for (var key in GROUPS) toggles.appendChild(toggleRow(key));
    return el('section', { id: PANEL_ID, style: CARD }, [
      el('h2', { style: 'margin:0;font-size:16px;', text: 'BlockTheSpot' }),
      el('p', { style: MUTED, text: statusLine() }),
      toggles,
      flagsSection(),
      el('p', { style: MUTED, text: 'Changes apply straight away. Auto-update is blocked by the installer, not from here. Run the installer again and choose Restore to undo everything.' })
    ]);
  }

  // The settings container, taken from the bundle rather than guessed at.
  function insertPanel() {
    try {
      var host = document.querySelector('[data-testid="settings-page"]');
      if (!host) return;
      var existing = document.getElementById(PANEL_ID);
      if (existing && existing.parentNode === host) return;
      if (existing) existing.parentNode.removeChild(existing);
      host.insertBefore(buildPanel(), host.firstChild);
    } catch (e) { /* leave the page alone */ }
  }

  // "BlockTheSpot is active" next to whatever shows the Spotify version (the About area).
  function markAbout() {
    try {
      var marker = 'bts-about-marker';
      var nodes = document.querySelectorAll('[data-testid="settings-page"] p, [data-testid="settings-page"] span, [role="dialog"] p, [role="dialog"] span');
      for (var i = 0; i < nodes.length; i++) {
        var node = nodes[i];
        var text = (node.textContent || '').trim();
        if (!/^(Spotify\s+)?(for\s+\w+\s+)?\d+\.\d+\.\d+\.\d+/.test(text)) continue;
        if (!node.parentNode || node.parentNode.querySelector('.' + marker)) continue;
        node.parentNode.insertBefore(
          el('div', { class: marker, style: MUTED, text: statusLine() }), node.nextSibling);
        return;
      }
    } catch (e) { /* no About visible */ }
  }

  function tick() { clearPopups(); insertPanel(); markAbout(); }

  function start() {
    try {
      state = read();
      applyCss();
      tick();
      var timer;
      new MutationObserver(function () {
        clearPopups();
        clearTimeout(timer);
        timer = setTimeout(function () { insertPanel(); markAbout(); }, 120);
      }).observe(document.body, { childList: true, subtree: true });
      // One line so it is obvious in DevTools whether this file loaded at all.
      if (window.console && console.info) console.info('[BlockTheSpot] panel loaded', INFO);
    } catch (e) { /* nothing else runs */ }
  }

  try {
    if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', start);
    else start();
  } catch (e) { /* never surface */ }
})();
