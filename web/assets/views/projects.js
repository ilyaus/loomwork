// Directory-of-projects landing view: one summary card per project, built from
// the cached counts in project.json so no project's subfolders are scanned.

import { api } from '../api.js';
import { commaList, el, field, formValues, render, tags, timestamp } from '../dom.js';

export async function renderProjects(view, { navigate, notify }) {
  const projects = await api.listProjects();
  render(
    view,
    el('div', { class: 'crumbs' }, el('h1', { text: 'Projects' }), el('span', { class: 'dim', text: `${projects.length} in this workspace` })),
    el('div', { class: 'cards' }, projects.map((project) => projectCard(project, navigate))),
    projects.length === 0 ? el('p', { class: 'empty', text: 'No projects yet. Create one below.' }) : null,
    el('section', { class: 'section' }, el('h2', { text: 'New project' }), newProjectForm(navigate, notify)),
  );
}

function projectCard(project, navigate) {
  const open = () => navigate(`#/projects/${encodeURIComponent(project.id)}`);
  return el(
    'article',
    { class: 'card project-card', onclick: open },
    el('div', { class: 'name', text: project.name }),
    el('div', { class: 'desc', text: project.description || '' }),
    tags(project.tags),
    el(
      'div',
      { class: 'stats' },
      stat('requirements', `${project.requirements} (${project.activeRequirements} active)`),
      stat('document sources', String(project.sources)),
      // Testability numbers are derived from execution reports and test-case
      // coverage, which phases 4 and 5 add; the API reports them as unavailable
      // rather than as zero so the placeholder is explicit.
      stat('last tested', project.testability.lastTestedAt ? timestamp(project.testability.lastTestedAt) : '—', !project.testability.available),
      stat('coverage', percent(project.testability.coveragePercent), !project.testability.available),
      stat('open gaps', project.testability.openGaps ?? '—', !project.testability.available),
      stat('updated', timestamp(project.updatedAt)),
    ),
    el('div', { class: 'mono dim', text: project.id }),
  );
}

function stat(key, value, pending = false) {
  return el(
    'div',
    { class: 'stat' },
    el('span', { class: 'k', text: key }),
    el('span', { class: `v${pending ? ' pending' : ''}`, text: String(value), title: pending ? 'available once execution reports land (phases 4–5)' : null }),
  );
}

function percent(value) {
  return value === null || value === undefined ? '—' : `${value}%`;
}

function newProjectForm(navigate, notify) {
  const form = el(
    'form',
    {
      class: 'card',
      onsubmit: async (event) => {
        event.preventDefault();
        const values = formValues(form);
        try {
          const created = await api.createProject({
            name: values.name,
            description: values.description,
            tags: commaList(values.tags),
          });
          notify(`created project ${created.name}`, 'ok');
          navigate(`#/projects/${encodeURIComponent(created.id)}`);
        } catch (error) {
          notify(error.message);
        }
      },
    },
    el(
      'div',
      { class: 'grid2' },
      field('name', el('input', { name: 'name', required: true, placeholder: 'checkout' })),
      field('tags (comma separated)', el('input', { name: 'tags', placeholder: 'cart,payments' })),
    ),
    field('description', el('input', { name: 'description', placeholder: 'Checkout API regression suite' })),
    el('div', { class: 'row' }, el('button', { class: 'primary', type: 'submit', text: 'Create project' })),
  );
  return form;
}
