// Thin wrapper over the JSON API. Every response body is either the requested
// document (requirements are exactly docs/schemas/requirement.schema.json) or
// {"error": "..."}, so error handling lives here once.

async function request(path, options = {}) {
  const response = await fetch(path, {
    ...options,
    headers: options.body ? { 'Content-Type': 'application/json' } : undefined,
  });
  const text = await response.text();
  const payload = text ? JSON.parse(text) : null;
  if (!response.ok) {
    throw new Error(payload && payload.error ? payload.error : `${response.status} ${response.statusText}`);
  }
  return payload;
}

const project = (ref) => `/api/projects/${encodeURIComponent(ref)}`;
const post = (path, body) => request(path, { method: 'POST', body: JSON.stringify(body) });

export const api = {
  workspace: () => request('/api/workspace'),

  listProjects: () => request('/api/projects'),
  createProject: (body) => post('/api/projects', body),
  getProject: (ref) => request(project(ref)),

  addSource: (ref, source) => post(`${project(ref)}/sources`, source),

  listRequirements: (ref, status) =>
    request(`${project(ref)}/requirements${status ? `?status=${encodeURIComponent(status)}` : ''}`),
  createRequirement: (ref, body) => post(`${project(ref)}/requirements`, body),
  updateRequirement: (ref, id, body) =>
    request(`${project(ref)}/requirements/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(body),
    }),
  requirementHistory: (ref, id) =>
    request(`${project(ref)}/requirements/${encodeURIComponent(id)}/history`),
  setRequirementStatus: (ref, id, body) =>
    post(`${project(ref)}/requirements/${encodeURIComponent(id)}/status`, body),
};
