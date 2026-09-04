export type Workspace = {
  home: string;
  projectsDir: string;
};

export type DocumentSource = {
  name: string;
  type: "ado" | "confluence" | "github" | "other";
  url?: string;
  localPath?: string;
  s3Uri?: string;
};

export type ArtifactBody = {
  content?: string;
  ref?: string;
  mediaType?: string;
};

export type Artifact = {
  id: string;
  name: string;
  type: string;
  version: number;
  tags?: string[];
  pinned: boolean;
  parentId?: string;
  body: ArtifactBody;
  metadata?: Record<string, string>;
  createdAt: string;
};

export type Project = {
  id: string;
  name: string;
  description?: string;
  tags?: string[];
  sources?: DocumentSource[];
  createdAt: string;
  updatedAt: string;
  artifacts: Artifact[];
};

export type ProjectSummary = {
  id: string;
  name: string;
  description?: string;
  tags?: string[];
  sources: number;
  requirements: number;
  activeRequirements: number;
  artifacts: number;
  createdAt: string;
  updatedAt: string;
};

export type Requirement = {
  id: string;
  version: number;
  text: string;
  source_type?: string;
  source_ref?: string;
  status: "active" | "obsolete" | "superseded";
  origin: string;
  tags?: string[];
  metadata?: Record<string, string>;
  created_at: string;
};

export type TreeItem = {
  ref: string;
  name: string;
  family: string;
  artifactType: string;
  mediaType: string;
  version?: number;
  status?: string;
  children?: TreeItem[];
};

export type TreeGroup = {
  family: string;
  label: string;
  items: TreeItem[];
};

export type TreeResponse = {
  groups: TreeGroup[];
};

export type ViewerDocument = {
  ref: string;
  name: string;
  family: string;
  artifactType: string;
  mediaType: string;
  version?: number;
  body: unknown;
  metadata?: Record<string, string>;
};

export type ModelChoice = {
  id: string;
  description?: string;
  selector: string;
  presets: string[];
};

export type ProviderModels = {
  provider: string;
  kind: string;
  status: string;
  models: ModelChoice[];
};

export type ChatMessage = {
  role: "user" | "assistant";
  content: string;
};
