// Minimal DOM helpers. The UI has no build step, so it renders with functions
// rather than a template language.

// el builds an element: attributes (and `class`, `text`, `html`, on* handlers)
// come from props, children may be nodes, strings, or nested arrays.
export function el(tag, props = {}, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(props || {})) {
    if (value === null || value === undefined || value === false) continue;
    if (key === 'class') node.className = value;
    else if (key === 'text') node.textContent = value;
    else if (key === 'dataset') Object.assign(node.dataset, value);
    else if (key.startsWith('on')) node.addEventListener(key.slice(2).toLowerCase(), value);
    else if (value === true) node.setAttribute(key, '');
    else node.setAttribute(key, value);
  }
  append(node, children);
  return node;
}

function append(node, children) {
  for (const child of children.flat(Infinity)) {
    if (child === null || child === undefined || child === false) continue;
    node.append(child instanceof Node ? child : document.createTextNode(String(child)));
  }
}

export function clear(node) {
  node.replaceChildren();
  return node;
}

export function badge(value) {
  return el('span', { class: `badge ${value}`, text: value });
}

export function tags(values) {
  if (!values || values.length === 0) return null;
  return el('div', { class: 'tags' }, values.map((value) => el('span', { class: 'tag', text: value })));
}

export function field(labelText, control) {
  return el('label', {}, labelText, control);
}

export function select(name, values, selected) {
  return el(
    'select',
    { name },
    values.map(([value, label]) => el('option', { value, selected: value === selected }, label)),
  );
}

// formValues reads a form into a plain object, dropping empty strings so an
// untouched optional field is absent from the request rather than sent as "".
export function formValues(form) {
  const values = {};
  for (const [key, raw] of new FormData(form).entries()) {
    const value = typeof raw === 'string' ? raw.trim() : raw;
    if (value !== '') values[key] = value;
  }
  return values;
}

export function commaList(raw) {
  if (!raw) return undefined;
  const values = raw.split(',').map((value) => value.trim()).filter(Boolean);
  return values.length > 0 ? values : undefined;
}

export function timestamp(value) {
  if (!value) return '—';
  return new Date(value).toISOString().replace('T', ' ').slice(0, 19) + 'Z';
}
