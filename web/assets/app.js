// Application shell: hash routing between the directory-of-projects landing view
// and the single active project view, plus the shared error toast.

import { api } from './api.js';
import { el } from './dom.js';
import { renderProjects } from './views/projects.js';
import { renderProject } from './views/project.js';

const view = document.getElementById('view');
const toast = document.getElementById('toast');
let toastTimer = 0;

function notify(message, kind = 'error') {
  toast.textContent = message;
  toast.className = `toast${kind === 'ok' ? ' ok' : ''}`;
  toast.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => {
    toast.hidden = true;
  }, kind === 'ok' ? 2500 : 8000);
}

function navigate(hash) {
  if (window.location.hash === hash) route();
  else window.location.hash = hash;
}

async function route() {
  const context = { navigate, notify };
  const match = /^#\/projects\/([^/]+)$/.exec(window.location.hash);
  try {
    if (match) await renderProject(view, decodeURIComponent(match[1]), context);
    else await renderProjects(view, context);
  } catch (error) {
    notify(error.message);
    view.replaceChildren(
      el('div', { class: 'card' }, el('p', { text: error.message }), el('p', {}, el('a', { href: '#/', text: 'Back to projects' }))),
    );
  }
}

api.workspace()
  .then(({ home }) => {
    document.getElementById('workspace').textContent = home;
  })
  .catch((error) => notify(error.message));

window.addEventListener('hashchange', route);
route();
