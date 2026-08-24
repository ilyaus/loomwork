import type {
  ChatMessage,
  DocumentSource,
  Project,
  ProjectSummary,
  ProviderModels,
  Requirement,
  TreeResponse,
  ViewerDocument,
  Workspace,
} from "./types";

async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const response = await fetch(path, {
    ...options,
    headers: options.body ? {"Content-Type": "application/json", ...options.headers} : options.headers,
  });
  const text = await response.text();
  const payload = text ? JSON.parse(text) as T | {error?: string} : null;
  if (!response.ok) {
    const message = payload && typeof payload === "object" && "error" in payload
      ? payload.error
      : `${response.status} ${response.statusText}`;
    throw new Error(message || "Request failed");
  }
  return payload as T;
}

const projectPath = (ref: string) => `/api/projects/${encodeURIComponent(ref)}`;

export const api = {
  workspace: () => request<Workspace>("/api/workspace"),
  listProjects: () => request<ProjectSummary[]>("/api/projects"),
  createProject: (body: {name: string; description: string; tags: string[]}) =>
    request<Project>("/api/projects", {method: "POST", body: JSON.stringify(body)}),
  getProject: (ref: string) => request<Project>(projectPath(ref)),
  addSource: (ref: string, source: DocumentSource) =>
    request<DocumentSource[]>(`${projectPath(ref)}/sources`, {method: "POST", body: JSON.stringify(source)}),
  createRequirement: (ref: string, body: {text: string; tags: string[]}) =>
    request<Requirement>(`${projectPath(ref)}/requirements`, {method: "POST", body: JSON.stringify(body)}),
  listRequirements: (ref: string) =>
    request<Requirement[]>(`${projectPath(ref)}/requirements`),
  updateRequirement: (ref: string, id: string, body: {text: string; tags: string[]}) =>
    request<Requirement>(`${projectPath(ref)}/requirements/${encodeURIComponent(id)}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    }),
  amendRequirement: (ref: string, id: string, body: {text: string; tags: string[]}) =>
    request<Requirement>(`${projectPath(ref)}/requirements/${encodeURIComponent(id)}`, {
      method: "PUT",
      body: JSON.stringify(body),
    }),
  requirementHistory: (ref: string, id: string) =>
    request<Requirement[]>(`${projectPath(ref)}/requirements/${encodeURIComponent(id)}/history`),
  setRequirementStatus: (ref: string, id: string, status: "active" | "obsolete") =>
    request<Requirement>(`${projectPath(ref)}/requirements/${encodeURIComponent(id)}/status`, {
      method: "POST",
      body: JSON.stringify({status}),
    }),
  projectItems: (ref: string) => request<TreeResponse>(`${projectPath(ref)}/items`),
  projectItem: (projectRef: string, family: string, ref: string, version?: number) =>
    request<ViewerDocument>(
      `${projectPath(projectRef)}/items/${encodeURIComponent(family)}/${encodeURIComponent(ref)}${
        version ? `?version=${version}` : ""
      }`,
    ),
  models: () => request<ProviderModels[]>("/api/models"),
};

export type ChatEvent =
  | {event: "ready"; data: {status: string}}
  | {event: "message"; data: {delta: string}}
  | {event: "done"; data: {model?: string; finishReason?: string}}
  | {event: "error"; data: {error: string}};

export async function streamChat(
  projectRef: string,
  selector: string,
  messages: ChatMessage[],
  artifactRefs: string[],
  onEvent: (event: ChatEvent) => void,
  signal: AbortSignal,
): Promise<void> {
  const response = await fetch(`${projectPath(projectRef)}/chat`, {
    method: "POST",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify({selector, messages, artifactRefs}),
    signal,
  });
  if (!response.ok || !response.body) {
    const payload = await response.json().catch(() => ({error: response.statusText})) as {error?: string};
    throw new Error(payload.error || `${response.status} ${response.statusText}`);
  }
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  while (true) {
    const {done, value} = await reader.read();
    buffer += decoder.decode(value, {stream: !done});
    let boundary = buffer.indexOf("\n\n");
    while (boundary >= 0) {
      const block = buffer.slice(0, boundary);
      buffer = buffer.slice(boundary + 2);
      const eventLine = block.split("\n").find((line) => line.startsWith("event:"));
      const dataLine = block.split("\n").find((line) => line.startsWith("data:"));
      if (eventLine && dataLine) {
        onEvent({
          event: eventLine.slice(6).trim(),
          data: JSON.parse(dataLine.slice(5).trim()),
        } as ChatEvent);
      }
      boundary = buffer.indexOf("\n\n");
    }
    if (done) break;
  }
}
