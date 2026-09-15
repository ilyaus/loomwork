import type {
  AgentDefinition,
  AgentDefinitionWrite,
  Artifact,
  ArtifactType,
  ChatMessage,
  DocumentSource,
  ImportResult,
  ItemVersion,
  OverrideRule,
  OverrideRuleWrite,
  Project,
  ProjectImportRequest,
  ProjectImportPreview,
  ProjectSummary,
  ProviderModels,
  Requirement,
  RequirementTestLink,
  RequirementWrite,
  TestabilityReport,
  TestDocumentSettings,
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
  let payload: unknown = null;
  if (text) {
    try {
      payload = JSON.parse(text);
    } catch {
      payload = {error: text.slice(0, 200)};
    }
  }
  if (!response.ok) {
    const message = payload && typeof payload === "object" && "error" in payload
      ? String((payload as {error: unknown}).error)
      : `${response.status} ${response.statusText}`;
    throw new Error(message || "Request failed");
  }
  return payload as T;
}

const json = (method: string, body: unknown): RequestInit => ({method, body: JSON.stringify(body)});
const projectPath = (ref: string) => `/api/projects/${encodeURIComponent(ref)}`;
const itemPath = (projectRef: string, family: string, ref: string) =>
  `${projectPath(projectRef)}/items/${encodeURIComponent(family)}/${encodeURIComponent(ref)}`;

export const api = {
  workspace: () => request<Workspace>("/api/workspace"),
  models: () => request<ProviderModels[]>("/api/models"),

  listProjects: () => request<ProjectSummary[]>("/api/projects"),
  createProject: (body: {name: string; description: string; tags: string[]}) =>
    request<Project>("/api/projects", json("POST", body)),
  previewProjectImport: (body: ProjectImportRequest) => request<ProjectImportPreview>("/api/project-import/preview", json("POST", body)),
  importProject: (body: ProjectImportRequest) => request<{project: Project; preview: ProjectImportPreview}>("/api/project-import", json("POST", body)),
  getProject: (ref: string) => request<Project>(projectPath(ref)),
  testability: (ref: string) => request<TestabilityReport>(`${projectPath(ref)}/testability`),
  addSource: (ref: string, source: DocumentSource) =>
    request<DocumentSource[]>(`${projectPath(ref)}/sources`, json("POST", source)),

  listRequirements: (ref: string) => request<Requirement[]>(`${projectPath(ref)}/requirements`),
  requirementTests: (ref: string) => request<Record<string, RequirementTestLink[]>>(`${projectPath(ref)}/requirement-tests`),
  createRequirement: (ref: string, body: RequirementWrite) =>
    request<Requirement>(`${projectPath(ref)}/requirements`, json("POST", body)),
  // PATCH writes the next version; PUT amends the current one in place.
  updateRequirement: (ref: string, id: string, body: RequirementWrite) =>
    request<Requirement>(`${projectPath(ref)}/requirements/${encodeURIComponent(id)}`, json("PATCH", body)),
  amendRequirement: (ref: string, id: string, body: RequirementWrite) =>
    request<Requirement>(`${projectPath(ref)}/requirements/${encodeURIComponent(id)}`, json("PUT", body)),
  requirementHistory: (ref: string, id: string) =>
    request<Requirement[]>(`${projectPath(ref)}/requirements/${encodeURIComponent(id)}/history`),
  setRequirementStatus: (ref: string, id: string, status: "active" | "obsolete") =>
    request<Requirement>(`${projectPath(ref)}/requirements/${encodeURIComponent(id)}/status`, json("POST", {status})),

  addArtifact: (ref: string, body: {name: string; type: ArtifactType; content: string; mediaType?: string; tags: string[]; pinned: boolean}) =>
    request<Artifact>(`${projectPath(ref)}/artifacts`, json("POST", body)),
  addReport: (ref: string, body: {name: string; content: string}) =>
    request<{name: string}>(`${projectPath(ref)}/reports`, json("POST", body)),
  testDocumentSettings: (ref: string) => request<TestDocumentSettings>(`${projectPath(ref)}/test-document-settings`),
  setTestDocumentSettings: (ref: string, roots: string[]) => request<TestDocumentSettings>(`${projectPath(ref)}/test-document-settings`, json("PUT", {roots})),
  importTestSuite: (ref: string, document: unknown) =>
    request<ImportResult>(`${projectPath(ref)}/test-suites`, json("POST", document)),

  createAgentDefinition: (ref: string, body: AgentDefinitionWrite) =>
    request<AgentDefinition>(`${projectPath(ref)}/agent-definitions`, json("POST", body)),
  updateAgentDefinition: (ref: string, name: string, body: AgentDefinitionWrite) =>
    request<AgentDefinition>(`${projectPath(ref)}/agent-definitions/${encodeURIComponent(name)}`, json("PATCH", body)),
  createOverrideRule: (ref: string, body: OverrideRuleWrite) =>
    request<OverrideRule>(`${projectPath(ref)}/override-rules`, json("POST", body)),
  updateOverrideRule: (ref: string, id: string, body: OverrideRuleWrite) =>
    request<OverrideRule>(`${projectPath(ref)}/override-rules/${encodeURIComponent(id)}`, json("PATCH", body)),
  setOverrideRuleStatus: (ref: string, id: string, status: "active" | "obsolete") =>
    request<OverrideRule>(`${projectPath(ref)}/override-rules/${encodeURIComponent(id)}/status`, json("POST", {status})),

  projectItems: (ref: string) => request<TreeResponse>(`${projectPath(ref)}/items`),
  projectItem: (projectRef: string, family: string, ref: string, version?: number) =>
    request<ViewerDocument>(`${itemPath(projectRef, family, ref)}${version ? `?version=${version}` : ""}`),
  itemHistory: (projectRef: string, family: string, ref: string) =>
    request<ItemVersion[]>(`${itemPath(projectRef, family, ref)}/history`),
};

export type ChatEvent =
  | {event: "ready"; data: {status: string}}
  | {event: "message"; data: {delta: string}}
  | {event: "done"; data: {model?: string; finishReason?: string; usage?: {promptTokens?: number; completionTokens?: number}}}
  | {event: "error"; data: {error: string}};

// streamChat reads the SSE response and reports each event; the returned promise
// settles when the stream closes or the signal aborts.
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
      const lines = block.split("\n");
      const eventLine = lines.find((line) => line.startsWith("event:"));
      const dataLine = lines.find((line) => line.startsWith("data:"));
      if (eventLine && dataLine) {
        onEvent({event: eventLine.slice(6).trim(), data: JSON.parse(dataLine.slice(5).trim())} as ChatEvent);
      }
      boundary = buffer.indexOf("\n\n");
    }
    if (done) break;
  }
}
