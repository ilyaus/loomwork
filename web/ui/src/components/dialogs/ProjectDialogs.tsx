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
      <ArtifactDialog open={state?.kind === "artifact"} onClose={onClose} />
      <SuiteDialog open={state?.kind === "suite"} onClose={onClose} />
      <AgentDialog open={state?.kind === "agent"} onClose={onClose} existing={state?.kind === "agent" ? state.payload as AgentDefinition | undefined : undefined} />
      <RuleDialog open={state?.kind === "rule"} onClose={onClose} existing={state?.kind === "rule" ? state.payload as OverrideRule | undefined : undefined} />
      <SourceDialog open={state?.kind === "source"} onClose={onClose} sources={project.sources || []} />
    </>
  );
}
