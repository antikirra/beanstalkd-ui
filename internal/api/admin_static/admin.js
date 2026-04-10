// Aurora — Admin JS (HTMX + Vanilla)

// --- Progress bar ---
document.addEventListener('htmx:beforeRequest', function() {
  document.getElementById('progress-bar').classList.add('active');
  document.getElementById('progress-bar').classList.remove('done');
});

document.addEventListener('htmx:afterSettle', function() {
  var bar = document.getElementById('progress-bar');
  bar.classList.remove('active');
  bar.classList.add('done');
  setTimeout(function() { bar.classList.remove('done'); bar.style.width = ''; }, 600);
});

// --- Toast auto-dismiss ---
var toastObserver = new MutationObserver(function(mutations) {
  mutations.forEach(function(m) {
    m.addedNodes.forEach(function(node) {
      if (node.classList && node.classList.contains('toast')) {
        setTimeout(function() {
          node.classList.add('toast-exit');
          setTimeout(function() { node.remove(); }, 500);
        }, 4000);
      }
    });
  });
});

var toastContainer = document.getElementById('toast-container');
if (toastContainer) {
  toastObserver.observe(toastContainer, { childList: true });
  // Auto-dismiss existing toasts on load
  toastContainer.querySelectorAll('.toast').forEach(function(t) {
    setTimeout(function() {
      t.classList.add('toast-exit');
      setTimeout(function() { t.remove(); }, 500);
    }, 4000);
  });
}

// --- Modal management ---
document.addEventListener('keydown', function(e) {
  if (e.key === 'Escape') {
    document.querySelectorAll('.modal.open').forEach(function(m) {
      m.classList.remove('open');
    });
  }
});

// --- Tabs ---
document.addEventListener('click', function(e) {
  var tab = e.target.closest('.tab');
  if (!tab) return;

  var tabs = tab.parentElement;
  var container = tabs.parentElement;

  tabs.querySelectorAll('.tab').forEach(function(t) { t.classList.remove('tab-active'); });
  tab.classList.add('tab-active');

  var panelName = tab.getAttribute('data-tab');
  container.querySelectorAll('.tab-panel').forEach(function(p) {
    p.classList.toggle('tab-panel-active', p.getAttribute('data-panel') === panelName);
  });
});

// --- Settings (cookie-based) ---
function saveSettings() {
  var fields = {
    'tubePauseSeconds': 'tubePauseSeconds',
    'autoRefreshTimeoutMs': 'autoRefreshTimeoutMs',
    'searchResultLimit': 'searchResultLimit'
  };
  for (var id in fields) {
    var el = document.getElementById(id);
    if (el) setCookie(fields[id], el.value, 365);
  }

  var jsonDecode = document.getElementById('isDisabledJsonDecode');
  if (jsonDecode) setCookie('isDisabledJsonDecode', jsonDecode.checked ? '0' : '1', 365);

  var base64 = document.getElementById('isEnabledBase64Decode');
  if (base64) setCookie('isEnabledBase64Decode', base64.checked ? '1' : '0', 365);

  var highlight = document.getElementById('isDisabledJobDataHighlight');
  if (highlight) setCookie('isDisabledJobDataHighlight', highlight.checked ? '0' : '1', 365);
}

function saveFilterCookie(cookieName) {
  var modal = document.querySelector('.modal.open');
  if (!modal) return;
  var checked = [];
  modal.querySelectorAll('input[type="checkbox"]:checked').forEach(function(cb) {
    checked.push(cb.name);
  });
  setCookie(cookieName, checked.join(','), 365);
  location.reload();
}

// --- Add server ---
document.addEventListener('click', function(e) {
  if (e.target.id !== 'addServerBtn') return;
  var host = document.getElementById('host').value;
  var port = document.getElementById('port').value;
  if (!host || !port) return;

  var server = host + ':' + port;
  var current = getCookie('beansServers') || '';
  current = decodeURIComponent(current);
  if (current.indexOf(server) === -1) {
    current += server + ';';
  }
  setCookie('beansServers', encodeURIComponent(current), 365);
  document.getElementById('modal-add-server').classList.remove('open');
  location.reload();
});

// --- Add job ---
document.addEventListener('click', function(e) {
  if (e.target.id !== 'addJobBtn') return;
  var tube = document.getElementById('addJobTube').value;
  var data = document.getElementById('addJobData').value;
  var priority = document.getElementById('addJobPriority').value;
  var delay = document.getElementById('addJobDelay').value;
  var ttr = document.getElementById('addJobTTR').value;

  if (!tube || !data) return;

  var params = new URLSearchParams();
  params.set('tubeName', tube);
  params.set('tubeData', data);
  params.set('tubePriority', priority);
  params.set('tubeDelay', delay);
  params.set('tubeTtr', ttr);

  var server = new URLSearchParams(location.search).get('server');
  fetch('/tube?server=' + encodeURIComponent(server) + '&tube=' + encodeURIComponent(tube) + '&action=addjob', {
    method: 'POST',
    body: params
  }).then(function(r) { return r.json(); }).then(function(resp) {
    document.getElementById('modal-addjob').classList.remove('open');
    if (resp.result) {
      showToast('success', 'Job added successfully');
      htmx.trigger(document.body, 'htmx:load');
      location.reload();
    }
  });
});

// --- Clear tubes ---
document.addEventListener('click', function(e) {
  if (e.target.id !== 'clearTubesBtn') return;
  if (!confirm('Clear all selected tubes? This cannot be undone.')) return;

  var server = new URLSearchParams(location.search).get('server');
  var params = new URLSearchParams();
  document.querySelectorAll('#modal-clear-tubes input[type="checkbox"]:checked').forEach(function(cb) {
    params.set(cb.name, '1');
  });

  fetch('/server?server=' + encodeURIComponent(server) + '&action=clearTubes', {
    method: 'POST',
    body: params
  }).then(function() {
    document.getElementById('modal-clear-tubes').classList.remove('open');
    showToast('success', 'Tubes cleared');
    location.reload();
  });
});

// --- Clear tubes regex selector ---
document.addEventListener('click', function(e) {
  if (e.target.id !== 'clearTubesSelect') return;
  var pattern = document.getElementById('tubeSelector').value;
  if (!pattern) return;
  try {
    var regex = new RegExp(pattern.replace(/\*/g, '.*'));
    document.querySelectorAll('#modal-clear-tubes input[type="checkbox"]').forEach(function(cb) {
      cb.checked = regex.test(cb.name);
    });
  } catch(err) { /* ignore bad regex */ }
});

// --- Toast helper ---
function showToast(type, message) {
  var container = document.getElementById('toast-container');
  var icons = { success: '✓', error: '✗', info: 'ⓘ' };
  var toast = document.createElement('div');
  toast.className = 'toast toast-' + type;
  toast.innerHTML = (icons[type] || '') + ' <span>' + message + '</span>' +
    '<button class="toast-close" onclick="this.parentElement.remove()">×</button>';
  container.appendChild(toast);
}

// --- Cookie helpers ---
function setCookie(name, value, days) {
  var d = new Date();
  d.setTime(d.getTime() + (days * 24 * 60 * 60 * 1000));
  document.cookie = name + '=' + value + ';expires=' + d.toUTCString() + ';path=/;SameSite=Lax';
}

function getCookie(name) {
  var v = document.cookie.match('(^|;) ?' + name + '=([^;]*)(;|$)');
  return v ? v[2] : null;
}

// --- Add sample from job ---
document.addEventListener('click', function(e) {
  var btn = e.target.closest('.addSample');
  if (!btn) return;
  var modal = document.getElementById('modal-addsample');
  if (!modal) return;
  document.getElementById('addSampleJobId').value = btn.getAttribute('data-jobid');
  document.getElementById('addSampleName').value = '';
  modal.classList.add('open');
});

document.addEventListener('click', function(e) {
  if (e.target.id !== 'addSampleBtn') return;
  var modal = document.getElementById('modal-addsample');
  var name = document.getElementById('addSampleName').value;
  var jobId = document.getElementById('addSampleJobId').value;
  if (!name) { showToast('error', 'Sample name required'); return; }

  var params = new URLSearchParams();
  params.set('addsamplename', name);
  params.set('addsamplejobid', jobId);
  modal.querySelectorAll('input[type="checkbox"]:checked').forEach(function(cb) {
    params.set(cb.name, '1');
  });

  var server = new URLSearchParams(location.search).get('server');
  var tube = new URLSearchParams(location.search).get('tube');
  fetch('/tube?server=' + encodeURIComponent(server) + '&tube=' + encodeURIComponent(tube) + '&action=addSample', {
    method: 'POST',
    body: params
  }).then(function(r) { return r.json(); }).then(function(resp) {
    modal.classList.remove('open');
    if (resp.result) {
      showToast('success', 'Sample saved');
    } else {
      showToast('error', resp.error || 'Failed to save sample');
    }
  });
});

// --- Auto-refresh toggle ---
document.addEventListener('click', function(e) {
  if (e.target.id !== 'autoRefreshToggle' && !e.target.closest('#autoRefreshToggle')) return;
  var elements = document.querySelectorAll('[hx-trigger*="every"]');
  elements.forEach(function(el) {
    var current = el.getAttribute('hx-trigger');
    if (el.hasAttribute('data-original-trigger')) {
      el.setAttribute('hx-trigger', el.getAttribute('data-original-trigger'));
      el.removeAttribute('data-original-trigger');
      htmx.process(el);
      showToast('info', 'Auto-refresh enabled');
    } else {
      el.setAttribute('data-original-trigger', current);
      el.removeAttribute('hx-trigger');
      htmx.process(el);
      showToast('info', 'Auto-refresh paused');
    }
  });
});

// --- Keyboard shortcuts ---
document.addEventListener('keydown', function(e) {
  if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA') return;
  if (e.key === '/' && !e.ctrlKey && !e.metaKey) {
    e.preventDefault();
    var search = document.querySelector('.search-input');
    if (search) search.focus();
  }
});
