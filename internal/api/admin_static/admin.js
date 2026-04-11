/* Aurora — CSP-compatible event delegation */

/* --- Progress bar (only for navigation, not polling) --- */
function isPollingRequest(evt) {
  var elt = evt.detail && evt.detail.elt;
  if (!elt) return false;
  var trigger = elt.getAttribute('hx-trigger') || '';
  return trigger.indexOf('every') !== -1;
}

document.addEventListener('htmx:beforeRequest', function (evt) {
  if (isPollingRequest(evt)) {
    var dot = document.getElementById('live-dot');
    if (dot) dot.classList.add('pulse');
  }
});

document.addEventListener('htmx:afterSettle', function (evt) {
  if (isPollingRequest(evt)) {
    var dot = document.getElementById('live-dot');
    if (dot) setTimeout(function () { dot.classList.remove('pulse'); }, 300);
  }
});

/* --- Sidebar active state --- */
function updateSidebarActive() {
  var path = location.pathname + location.search;
  document.querySelectorAll('.nav-item').forEach(function (item) {
    var href = item.getAttribute('href');
    if (path === '/' && href === '/') {
      item.classList.add('active');
    } else if (href !== '/' && path.startsWith(href)) {
      item.classList.add('active');
    } else {
      item.classList.remove('active');
    }
  });
}
document.addEventListener('htmx:pushedIntoHistory', updateSidebarActive);
updateSidebarActive();

/* --- Modal helpers --- */
function closeAllModals() {
  document.querySelectorAll('.modal.open').forEach(function (m) {
    m.classList.remove('open');
  });
}

function closeModal(el) {
  var modal = el.closest('.modal');
  if (modal) modal.classList.remove('open');
}

document.addEventListener('click', function (e) {
  var opener = e.target.closest('[data-modal-open]');
  if (opener) {
    var id = opener.getAttribute('data-modal-open');
    var modal = document.getElementById(id);
    if (modal) {
      modal.classList.add('open');
      var input = modal.querySelector('input[type="text"], input[type="number"], textarea');
      if (input) setTimeout(function () { input.focus(); }, 80);
    }
    return;
  }

  if (e.target.closest('[data-modal-close]')) {
    closeModal(e.target);
    return;
  }

  if (e.target.classList.contains('modal-backdrop')) {
    closeModal(e.target);
  }
});

document.addEventListener('keydown', function (e) {
  if (e.key === 'Escape') closeAllModals();
});

/* --- Dropdown toggle --- */
document.addEventListener('click', function (e) {
  var toggle = e.target.closest('.dropdown-toggle');
  if (toggle) {
    e.preventDefault();
    var dd = toggle.closest('.dropdown');
    dd.classList.toggle('open');
    return;
  }
  document.querySelectorAll('.dropdown.open').forEach(function (dd) {
    dd.classList.remove('open');
  });
});

/* --- Toast --- */
function showToast(type, message) {
  var c = document.getElementById('toast-container');
  var icons = { success: '✓', error: '✗', info: 'i' };
  var t = document.createElement('div');
  t.className = 'toast toast-' + type;
  t.innerHTML =
    '<span class="toast-icon">' + (icons[type] || 'i') + '</span>' +
    '<span>' + message + '</span>' +
    '<button class="toast-close" data-modal-close>×</button>';
  c.appendChild(t);
  autoDismissToast(t);
}

function autoDismissToast(t) {
  setTimeout(function () {
    t.classList.add('toast-exit');
    setTimeout(function () { t.remove(); }, 500);
  }, 4000);
}

// Auto-dismiss server-rendered toasts on page load.
var tc = document.getElementById('toast-container');
if (tc) {
  tc.querySelectorAll('.toast').forEach(autoDismissToast);
}

/* --- HTMX event-driven toasts and modal close --- */
document.addEventListener('showToast', function (e) {
  showToast(e.detail.type, e.detail.message);
});

document.addEventListener('closeModal', closeAllModals);

/* --- Toast close via delegation --- */
document.addEventListener('click', function (e) {
  if (e.target.closest('.toast-close')) {
    var toast = e.target.closest('.toast');
    if (toast) toast.remove();
  }
});

/* --- Tabs --- */
document.addEventListener('click', function (e) {
  var tab = e.target.closest('.tab');
  if (!tab) return;
  var tabs = tab.parentElement;
  var container = tabs.parentElement;
  tabs.querySelectorAll('.tab').forEach(function (t) { t.classList.remove('tab-active'); });
  tab.classList.add('tab-active');
  var panel = tab.getAttribute('data-tab');
  container.querySelectorAll('.tab-panel').forEach(function (p) {
    p.classList.toggle('tab-panel-active', p.getAttribute('data-panel') === panel);
  });
});

/* --- Settings (cookie-based) --- */
document.addEventListener('click', function (e) {
  if (!e.target.closest('[data-action="saveSettings"]')) return;
  var fields = ['tubePauseSeconds', 'autoRefreshTimeoutMs', 'searchResultLimit'];
  fields.forEach(function (id) {
    var el = document.getElementById(id);
    if (el) setCookie(id, el.value, 365);
  });
  var jd = document.getElementById('isDisabledJsonDecode');
  if (jd) setCookie('isDisabledJsonDecode', jd.checked ? '0' : '1', 365);
  var b64 = document.getElementById('isEnabledBase64Decode');
  if (b64) setCookie('isEnabledBase64Decode', b64.checked ? '1' : '0', 365);
  var hl = document.getElementById('isDisabledJobDataHighlight');
  if (hl) setCookie('isDisabledJobDataHighlight', hl.checked ? '0' : '1', 365);
  closeModal(e.target);
  showToast('success', 'Settings saved');
});

/* --- Filter save --- */
document.addEventListener('click', function (e) {
  var btn = e.target.closest('[data-action="saveFilter"]');
  if (!btn) return;
  var cookie = btn.getAttribute('data-cookie');
  var modal = e.target.closest('.modal');
  if (!modal) return;
  var checked = [];
  modal.querySelectorAll('input[type="checkbox"]:checked').forEach(function (cb) {
    checked.push(cb.name);
  });
  setCookie(cookie, checked.join(','), 365);
  closeModal(btn);
  location.reload();
});

/* --- Add server --- */
document.addEventListener('click', function (e) {
  if (!e.target.closest('[data-action="addServer"]')) return;
  var host = document.getElementById('host').value;
  var port = document.getElementById('port').value;
  if (!host || !port) return;
  var server = host + ':' + port;
  var cur = decodeURIComponent(getCookie('beansServers') || '');
  if (cur.indexOf(server) === -1) cur += server + ';';
  setCookie('beansServers', encodeURIComponent(cur), 365);
  closeModal(e.target);
  location.reload();
});

/* --- Add sample from job (opens modal with job ID) --- */
document.addEventListener('click', function (e) {
  var btn = e.target.closest('.addSample');
  if (!btn) return;
  var modal = document.getElementById('modal-addsample');
  if (!modal) return;
  document.getElementById('addSampleJobId').value = btn.getAttribute('data-jobid');
  var nameInput = modal.querySelector('input[name="addsamplename"]');
  nameInput.value = '';
  modal.classList.add('open');
  setTimeout(function () { nameInput.focus(); }, 80);
});

/* --- Clear tubes --- */
document.addEventListener('click', function (e) {
  if (!e.target.closest('[data-action="clearTubes"]')) return;
  var clearModal = document.getElementById('modal-clear-tubes');
  var checked = clearModal.querySelectorAll('input[type="checkbox"]:checked');
  if (checked.length === 0) { showToast('error', 'No tubes selected'); return; }

  // Switch to confirm modal.
  closeModal(e.target);
  pendingConfirmForm = null;
  var confirmModal = document.getElementById('modal-confirm');
  confirmModal.querySelector('.confirm-message').innerHTML = 'Clear <b>' + checked.length + ' selected tube(s)</b>? All jobs will be deleted.';
  confirmModal.classList.add('open');

  var handler = function (ev) {
    if (!ev.target.closest('[data-action="confirmYes"]')) return;
    document.removeEventListener('click', handler, true);
    closeModal(ev.target);

    var server = new URLSearchParams(location.search).get('server');
    var values = {};
    checked.forEach(function (cb) { values[cb.name] = '1'; });
    htmx.ajax('POST', '/server?server=' + encodeURIComponent(server) + '&action=clearTubes', {
      values: values, swap: 'none'
    });
  };
  document.addEventListener('click', handler, true);
});

/* --- Clear tubes regex selector --- */
document.addEventListener('click', function (e) {
  if (!e.target.closest('[data-action="selectTubes"]')) return;
  var pattern = document.getElementById('tubeSelector').value;
  if (!pattern) return;
  try {
    var re = new RegExp(pattern.replace(/\*/g, '.*'));
    var modal = document.getElementById('modal-clear-tubes');
    modal.querySelectorAll('input[type="checkbox"]').forEach(function (cb) {
      cb.checked = re.test(cb.name);
    });
  } catch (err) { /* ignore */ }
});

/* --- Auto-refresh toggle --- */
document.addEventListener('click', function (e) {
  if (!e.target.closest('[data-action="toggleRefresh"]')) return;
  var btn = e.target.closest('[data-action="toggleRefresh"]');
  var paused = document.querySelectorAll('[data-paused-trigger]');

  if (paused.length > 0) {
    // Resume: restore triggers.
    paused.forEach(function (el) {
      el.setAttribute('hx-trigger', el.getAttribute('data-paused-trigger'));
      el.removeAttribute('data-paused-trigger');
      htmx.process(el);
    });
    btn.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2"><rect x="6" y="4" width="4" height="16"/><rect x="14" y="4" width="4" height="16"/></svg> Pause';
    showToast('info', 'Auto-refresh resumed');
  } else {
    // Pause: save and remove triggers.
    var els = document.querySelectorAll('[hx-trigger*="every"]');
    els.forEach(function (el) {
      el.setAttribute('data-paused-trigger', el.getAttribute('hx-trigger'));
      el.removeAttribute('hx-trigger');
      htmx.process(el);
    });
    btn.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2"><polygon points="5 3 19 12 5 21 5 3"/></svg> Resume';
    showToast('info', 'Auto-refresh paused');
  }
});

/* --- Confirm dialog (custom modal) --- */
var pendingConfirmForm = null;

document.addEventListener('submit', function (e) {
  var form = e.target.closest('form[data-confirm]');
  if (!form || form.hasAttribute('data-confirmed')) return;
  e.preventDefault();
  pendingConfirmForm = form;
  var modal = document.getElementById('modal-confirm');
  modal.querySelector('.confirm-message').innerHTML = form.getAttribute('data-confirm');
  modal.classList.add('open');
});

document.addEventListener('click', function (e) {
  if (e.target.closest('[data-action="confirmYes"]')) {
    closeModal(e.target);
    if (pendingConfirmForm) {
      pendingConfirmForm.setAttribute('data-confirmed', '');
      pendingConfirmForm.submit();
      pendingConfirmForm = null;
    }
  }
  if (e.target.closest('[data-action="confirmNo"]')) {
    closeModal(e.target);
    pendingConfirmForm = null;
  }
});

/* --- Keyboard shortcuts --- */
document.addEventListener('keydown', function (e) {
  if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA') return;
  if (e.key === '/' && !e.ctrlKey && !e.metaKey) {
    e.preventDefault();
    var s = document.querySelector('.search-input');
    if (s) s.focus();
  }
});

/* --- Statistics update interval (modifies HTMX polling trigger) --- */
document.addEventListener('change', function (e) {
  if (e.target.id !== 'statsUpdateInterval') return;
  var v = parseInt(e.target.value);
  if (!v || v < 1) return;
  var chart = document.getElementById('stats-chart');
  if (chart) {
    chart.setAttribute('hx-trigger', 'every ' + v + 's');
    htmx.process(chart);
  }
});

/* --- Cookie helpers --- */
function setCookie(name, value, days) {
  var d = new Date();
  d.setTime(d.getTime() + days * 86400000);
  document.cookie = name + '=' + value + ';expires=' + d.toUTCString() + ';path=/;SameSite=Lax';
}
function getCookie(name) {
  var m = document.cookie.match('(^|;) ?' + name + '=([^;]*)(;|$)');
  return m ? m[2] : null;
}
