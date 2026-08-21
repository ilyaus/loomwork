// Single active project view: document source links plus requirement management
// with version history. Loomwork edits one project at a time, so this view owns
// the whole project context.

import { api } from '../api.js';
import { badge, clear, commaList, el, field, formValues, select, tags, timestamp } from '../dom.js';

const SOURCE_TYPES = [
  ['ado', 'ado'],
  ['confluence', 'confluence'],
  ['github', 'github'],
  ['other', 'other'],
];

export async function renderProject(view, ref, { navigate, notify }) {
  // One selection and one filter for the lifetime of this view; every mutation
  // re-reads the store so the UI never renders from a stale local copy.
  const state = { selected: null, statusFilter: '' };

  async function paint() {
    const [project, requirements] = await Promise.all([
      api.getProject(ref),
      api.listRequirements(ref, state.statusFilter),
    ]);
    if (state.selected && !requirements.some((requirement) => requirement.id === state.selected)) {
      state.selected = null;
    }
    const history = state.selected ? await api.requirementHistory(ref, state.selected) : null;

    clear(view).append(
      el(
        'div',
        { class: 'crumbs' },
        el('a', { href: '#/', text: 'Projects' }),
        el('span', { class: 'dim', text: '/' }),
        el('h1', { text: project.name }),
        tags(project.tags),
      ),
      el('div', { class: 'mono dim', text: project.id }),
      project.description ? el('p', { class: 'muted', text: project.description }) : null,
      sourcesSection(project, run),
      requirementsSection(project, requirements, history, state, run),
    );
  }

  // run wraps a mutation: report the failure, then repaint from the store.
  async function run(action, message) {
    try {
      await action();
      if (message) notify(message, 'ok');
    } catch (error) {
      notify(error.message);
    }
    await paint().catch((error) => notify(error.message));
  }

  await paint();
  return { navigate };
}

function sourcesSection(project, run) {
  const sources = project.sources || [];
  return el(
    'section',
    { class: 'section' },
    el('h2', { text: 'Document sources' }),
    el(
      'div',
      { class: 'card' },
      sources.length === 0
        ? el('p', { class: 'empty', text: 'No document sources linked yet.' })
        : el(
            'table',
            {},
            el('thead', {}, el('tr', {}, ['name', 'type', 'location'].map((head) => el('th', { text: head })))),
            el(
              'tbody',
              {},
              sources.map((source) =>
                el(
                  'tr',
                  {},
                  el('td', { text: source.name }),
                  el('td', {}, el('span', { class: 'tag', text: source.type })),
                  el('td', {}, sourceLocation(source)),
                ),
              ),
            ),
          ),
    ),
    el(
      'details',
      { class: 'section' },
      el('summary', { text: '+ Link a document source' }),
      sourceForm(project, run),
    ),
  );
}

function sourceLocation(source) {
  const parts = [];
  if (source.url) parts.push(el('a', { href: source.url, target: '_blank', rel: 'noreferrer', text: source.url }));
  if (source.localPath) parts.push(el('span', { class: 'mono dim', text: source.localPath }));
  if (source.s3Uri) parts.push(el('span', { class: 'mono dim', text: source.s3Uri }));
  return el('div', { class: 'row' }, parts);
}

function sourceForm(project, run) {
  const form = el(
    'form',
    {
      class: 'card',
      onsubmit: (event) => {
        event.preventDefault();
        const values = formValues(form);
        run(async () => {
          await api.addSource(project.id, values);
          form.reset();
        }, `linked source ${values.name}`);
      },
    },
    el(
      'div',
      { class: 'grid2' },
      field('name', el('input', { name: 'name', required: true, placeholder: 'spec' })),
      field('type', select('type', SOURCE_TYPES, 'confluence')),
    ),
    field('url', el('input', { name: 'url', placeholder: 'https://wiki/checkout' })),
    el(
      'div',
      { class: 'grid2' },
      field('local copy', el('input', { name: 'localPath', placeholder: './checkout.pdf' })),
      field('s3 copy', el('input', { name: 's3Uri', placeholder: 's3://bucket/checkout.pdf' })),
    ),
    el(
      'div',
      { class: 'row' },
      el('button', { class: 'primary', type: 'submit', text: 'Link source' }),
      el('span', { class: 'dim', text: 'A url, a local copy, or an s3 copy is required. Re-using a name updates that link.' }),
    ),
  );
  return form;
}

function requirementsSection(project, requirements, history, state, run) {
  const statusFilter = select(
    'status',
    [['', 'all'], ['active', 'active'], ['obsolete', 'obsolete'], ['superseded', 'superseded']],
    state.statusFilter,
  );
  statusFilter.addEventListener('change', () =>
    run(() => {
      state.statusFilter = statusFilter.value;
    }),
  );
  return el(
    'section',
    { class: 'section' },
    el(
      'div',
      { class: 'row' },
      el('h2', { text: 'Requirements' }),
      el('span', { class: 'spacer' }),
      el('label', { class: 'row' }, 'status', statusFilter),
    ),
    el(
      'div',
      { class: 'split' },
      requirementTable(requirements, state, run),
      state.selected ? requirementDetail(project, history, state, run) : el('div', { class: 'card empty', text: 'Select a requirement to see its versions.' }),
    ),
    el(
      'details',
      { class: 'section' },
      el('summary', { text: '+ Add a requirement' }),
      createRequirementForm(project, run),
    ),
  );
}

function requirementTable(requirements, state, run) {
  const table = el('div', { class: 'card' });
  if (requirements.length === 0) {
    table.append(el('p', { class: 'empty', text: 'No requirements match.' }));
    return table;
  }
  table.append(
    el(
      'table',
      {},
      el('thead', {}, el('tr', {}, ['id', 'v', 'status', 'requirement'].map((head) => el('th', { text: head })))),
      el(
        'tbody',
        {},
        requirements.map((requirement) =>
          el(
            'tr',
            {
              class: `selectable${state.selected === requirement.id ? ' selected' : ''}`,
              onclick: () => run(() => {
                state.selected = requirement.id;
              }),
            },
            el('td', {}, el('span', { class: 'mono', text: requirement.id })),
            el('td', {}, el('span', { class: 'mono', text: `v${requirement.version}` })),
            el('td', {}, badge(requirement.status)),
            el('td', { class: 'requirement-text', text: requirement.text }),
          ),
        ),
      ),
    ),
  );
  return table;
}

function requirementDetail(project, history, state, run) {
  const current = history[history.length - 1];
  return el(
    'div',
    { class: 'card' },
    el(
      'div',
      { class: 'row' },
      el('h3', { text: current.id }),
      badge(current.status),
      badge(current.origin),
      el('span', { class: 'spacer' }),
      el('button', {
        class: 'small',
        text: current.status === 'obsolete' ? 'Mark active' : 'Mark obsolete',
        onclick: () =>
          run(
            () => api.setRequirementStatus(project.id, current.id, { status: current.status === 'obsolete' ? 'active' : 'obsolete' }),
            `${current.id} status changed`,
          ),
      }),
    ),
    current.source_type
      ? el('div', { class: 'mono dim', text: `${current.source_type} ${current.source_ref || ''}`.trim() })
      : null,
    tags(current.tags),
    el('h2', { class: 'section', text: `Versions (${history.length})` }),
    el(
      'div',
      { class: 'history' },
      [...history].reverse().map((version) =>
        el(
          'div',
          { class: 'version' },
          el(
            'div',
            { class: 'head' },
            el('span', { class: 'mono', text: `v${version.version}` }),
            badge(version.status),
            el('span', { class: 'spacer' }),
            el('span', { class: 'mono dim', text: timestamp(version.created_at) }),
          ),
          el('div', { class: 'requirement-text', text: version.text }),
        ),
      ),
    ),
    el(
      'details',
      { class: 'section' },
      el('summary', { text: '+ New version' }),
      updateRequirementForm(project, current, run),
    ),
  );
}

function createRequirementForm(project, run) {
  const form = el(
    'form',
    {
      class: 'card',
      onsubmit: (event) => {
        event.preventDefault();
        const values = formValues(form);
        run(async () => {
          await api.createRequirement(project.id, { ...values, tags: commaList(values.tags) });
          form.reset();
        }, 'requirement created');
      },
    },
    field('text', el('textarea', { name: 'text', required: true, placeholder: 'Cart totals include tax' })),
    el(
      'div',
      { class: 'grid2' },
      field('source type', select('source_type', [['', 'none'], ...SOURCE_TYPES], '')),
      field('source reference', el('input', { name: 'source_ref', placeholder: 'AB#1234' })),
      field('origin', select('origin', [['authored', 'authored'], ['extracted', 'extracted']], 'authored')),
      field('status', select('status', [['active', 'active'], ['obsolete', 'obsolete']], 'active')),
      field('tags (comma separated)', el('input', { name: 'tags', placeholder: 'cart,tax' })),
    ),
    el('div', { class: 'row' }, el('button', { class: 'primary', type: 'submit', text: 'Create requirement' })),
  );
  return form;
}

function updateRequirementForm(project, current, run) {
  const form = el(
    'form',
    {
      onsubmit: (event) => {
        event.preventDefault();
        const values = formValues(form);
        run(
          () => api.updateRequirement(project.id, current.id, { ...values, tags: commaList(values.tags) }),
          `${current.id} superseded by a new version`,
        );
      },
    },
    field('text', el('textarea', { name: 'text', text: current.text })),
    el(
      'div',
      { class: 'grid2' },
      field('source type', select('source_type', [['', 'none'], ...SOURCE_TYPES], current.source_type || '')),
      field('source reference', el('input', { name: 'source_ref', value: current.source_ref || '' })),
      field('tags (comma separated)', el('input', { name: 'tags', value: (current.tags || []).join(',') })),
    ),
    el(
      'div',
      { class: 'row' },
      el('button', { class: 'primary', type: 'submit', text: 'Save new version' }),
      el('span', { class: 'dim', text: `v${current.version} is retained and marked superseded.` }),
    ),
  );
  return form;
}
