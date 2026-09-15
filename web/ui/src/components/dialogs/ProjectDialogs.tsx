import type {DialogState} from "../../lib/desktop";
import type {AgentDefinition, OverrideRule, Project} from "../../types";
import AgentDialog from "./AgentDialog";
import ArtifactDialog from "./ArtifactDialog";
import RequirementDialog from "./RequirementDialog";
import RuleDialog from "./RuleDialog";
import SourceDialog from "./SourceDialog";
import SuiteDialog from "./SuiteDialog";

// ProjectDialogs mounts whichever create/import form the desktop asked for.
export default function ProjectDialogs({state, onClose, project}: {state: DialogState; onClose: () => void; project: Project}) {
  return (
    <>
      <RequirementDialog open={state?.kind === "requirement"} onClose={onClose} />
      {state?.kind === "artifact" && <ArtifactDialog onClose={onClose} existing={project.artifacts.find(artifact => artifact.id === state.payload)} />}
      {state?.kind === "report" && <ArtifactDialog report onClose={onClose} />}
      <SuiteDialog open={state?.kind === "suite"} onClose={onClose} />
      {state?.kind === "agent" && <AgentDialog open onClose={onClose} existing={state.payload as AgentDefinition | undefined} />}
      {state?.kind === "rule" && <RuleDialog open onClose={onClose} existing={state.payload as OverrideRule | undefined} />}
      <SourceDialog open={state?.kind === "source"} onClose={onClose} sources={project.sources || []} />
    </>
  );
}
