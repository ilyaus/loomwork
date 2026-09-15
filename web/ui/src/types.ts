export type Workspace = {
  home: string;
  projectsDir: string;
};

export type SourceType = "ado" | "confluence" | "github" | "other";

export type DocumentSource = {
  name: string;
  type: SourceType;
  url?: string;
  localPath?: string;
  s3Uri?: string;
};

export type ArtifactType = "spec" | "log" | "test-result" | "diagram" | "doc" | "generated";

export type ArtifactBody = {
  content?: string;
  ref?: string;
  mediaType?: string;
};

export type Artifact = {
  id: string;
  name: string;
  type: ArtifactType;
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
  index?: {requirements: number; activeRequirements: number};
  testDocuments?: {roots: string[]};
  import?: {format: string; sourcePath: string; importedAt: string; features: string[]; requirementsReadOnly: boolean};
};

export type ProjectImportRequest = {format: string; path: string; name?: string; features?: string[]};
export type ProjectImportPreview = {
  format: string;
  sourcePath: string;
  name: string;
  features: {id: string; title: string; requirements: number; artifacts: number; testDocuments: number}[];
  requirements: number;
  artifacts: number;
  testDocuments: number;
  warnings: string[];
};

export type Testability = {
  available: boolean;
  lastTestedAt: string | null;
  coveragePercent: number | null;
  openGaps: number | null;
};

export type RunSummary = {
  report: string;
  outcome?: string;
  total: number;
  passed: number;
  failed: number;
  skipped: number;
};

export type TestabilityReport = Testability & {
  activeRequirements: number;
  coveredRequirements: string[];
  uncoveredRequirements: string[];
  suites: number;
  incompleteSuites: number;
  cases: number;
  unlinkedCases: number;
  reports: number;
  lastRun: RunSummary | null;
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
  testability: Testability;
};

export type RequirementStatus = "active" | "obsolete" | "superseded";

export type Requirement = {
  id: string;
  display_id?: string;
  version: number;
  text: string;
  source_type?: SourceType;
  source_ref?: string;
  status: RequirementStatus;
  origin: "authored" | "extracted" | "imported";
  tags?: string[];
  metadata?: Record<string, string>;
  created_at: string;
};

export type RequirementTestLink = {
  family: "test-cases" | "artifacts";
  ref: string;
  name: string;
  version: number;
  suiteId?: string;
  suiteTitle?: string;
};

export type RequirementWrite = {
  text: string;
  tags?: string[];
  source_type?: SourceType;
  source_ref?: string;
  origin?: "authored" | "extracted";
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

export type ItemVersion = {
  version: number;
  status?: string;
  summary?: string;
  createdAt: string;
};

export type HTTPMethod = "GET" | "POST" | "PUT" | "PATCH" | "DELETE" | "HEAD" | "OPTIONS";

export type TestScenario =
  | "happy-path"
  | "missing-item"
  | "invalid-input"
  | "missing-authentication"
  | "unauthorized"
  | "conflict"
  | "rate-limit"
  | "server-error"
  | "other";

export type TestCase = {
  id: string;
  name: string;
  requirement_ids: string[];
  overrides_applied: string[];
  scenario: TestScenario;
  request: {
    method: HTTPMethod;
    path: string;
    query?: Record<string, string>;
    headers?: Record<string, string>;
    body?: string;
    body_media_type?: string;
  };
  expected: {
    status: number;
    empty_collection?: boolean;
    body_fields?: string[];
    max_latency_ms?: number;
    notes?: string;
  };
  tags?: string[];
  metadata?: Record<string, string>;
};

export type DocumentSuite = {id: string; name: string; root: string; documents: Artifact[]};
export type TestDocumentSettings = {roots: string[]; defaults: string[]};

export type TestSuite = {
  suite_id: string;
  version: number;
  origin: "generated" | "imported";
  title?: string;
  description?: string;
  cases?: TestCase[];
  case_ids?: string[];
  incomplete: boolean;
  incomplete_reasons?: string[];
  agent_definition?: string;
  requirement_versions?: Record<string, number>;
  override_rules?: string[];
  spec_ref?: string;
  tags?: string[];
  metadata?: Record<string, string>;
  created_at: string;
};

export type ImportResult = {
  projectId: string;
  suite: TestSuite;
  audit: {
    unlinked_cases?: string[];
    findings?: {kind: string; case_id: string; rule_ref?: string; detail: string}[];
  };
};

export type AgentTarget = "claude-agent-sdk" | "copilot-sdk";

export type AgentDefinition = {
  agent_name: string;
  version: number;
  target_provider: AgentTarget;
  model?: string;
  tools_allowed?: string[];
  body: string;
  description?: string;
  tags?: string[];
  metadata?: Record<string, string>;
  created_at: string;
};

export type AgentDefinitionWrite = {
  agent_name?: string;
  target_provider?: AgentTarget;
  model?: string;
  tools_allowed?: string[];
  body?: string;
  description?: string;
  tags?: string[];
};

export type OverrideActionKind = "expect-status" | "expect-empty-collection" | "skip-test";

export type OverrideRule = {
  id: string;
  version: number;
  title: string;
  condition: {
    methods?: HTTPMethod[];
    path_pattern?: string;
    scenario?: TestScenario;
    spec_status?: number;
  };
  action: {kind: OverrideActionKind; expect_status?: number};
  rationale: string;
  status: RequirementStatus;
  tags?: string[];
  metadata?: Record<string, string>;
  created_at: string;
};

export type OverrideRuleWrite = {
  id?: string;
  title?: string;
  condition?: OverrideRule["condition"];
  action?: OverrideRule["action"];
  rationale?: string;
  tags?: string[];
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
