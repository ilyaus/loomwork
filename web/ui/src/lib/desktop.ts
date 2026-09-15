import {createContext, useContext} from "react";
import type {TreeItem} from "../types";

// A tab is the overview, a family list (requirements, test suites), or one
// entity document. Keys are stable so reopening an item focuses its tab.
export type Tab =
  | {key: "overview"; kind: "overview"; name: string}
  | {key: string; kind: "family"; family: string; name: string}
  | {key: string; kind: "item"; family: string; ref: string; name: string; version?: number; artifactType?: string; selectedTest?: string};

export const overviewTab: Tab = {key: "overview", kind: "overview", name: "Overview"};

export const familyKey = (family: string) => `family:${family}`;
export const itemKey = (family: string, ref: string) => `${family}:${ref}`;

export function itemTab(item: Pick<TreeItem, "family" | "ref" | "name"> & Partial<TreeItem>): Extract<Tab, {kind: "item"}> {
  return {
    key: itemKey(item.family, item.ref),
    kind: "item",
    family: item.family,
    ref: item.ref,
    name: item.name,
    version: item.version,
    artifactType: item.artifactType,
  };
}

export const familyLabels: Record<string, string> = {
  requirements: "Requirements",
  "agent-definitions": "Agent definitions",
  "override-rules": "Override rules",
  "test-suites": "Test suites",
  "document-suites": "Document suites",
  "test-cases": "Test cases",
  reports: "Reports",
  artifacts: "Artifacts",
};

export type DesktopActions = {
  projectRef: string;
  requirementsReadOnly: boolean;
  openItem: (item: Pick<TreeItem, "family" | "ref" | "name"> & Partial<TreeItem>, selectedTest?: string) => void;
  requirementName: (ref: string) => string;
  openFamily: (family: string) => void;
  openOverview: () => void;
  // openDialog shows one of the project's create/import forms; payload carries
  // the entity being edited when the form writes a new version.
  openDialog: (dialog: DialogKind, payload?: unknown) => void;
  refresh: () => void;
};

export const familyActions: Record<string, {kind: DialogKind; label: string; description: string}> = {
  requirements: {kind: "requirement", label: "New requirement", description: "Versioned requirements with references to their source documents."},
  artifacts: {kind: "artifact", label: "Add artifact", description: "Specs, documentation, and test scenarios. Reuse a name to save a new version."},
  reports: {kind: "report", label: "Add report", description: "Upload execution results. Every report is retained; existing files are never overwritten."},
  "test-suites": {kind: "suite", label: "Import suite", description: "Versioned test suites linked to requirements."},
  "agent-definitions": {kind: "agent", label: "New agent definition", description: "Versioned instructions and tool permissions for test generation."},
  "override-rules": {kind: "rule", label: "New override rule", description: "Business rules that take precedence over the literal API specification."},
};

export type DialogKind = "requirement" | "artifact" | "report" | "suite" | "agent" | "rule" | "source";
export type DialogState = {kind: DialogKind; payload?: unknown} | null;

export const DesktopContext = createContext<DesktopActions | null>(null);

export function useDesktop(): DesktopActions {
  const context = useContext(DesktopContext);
  if (!context) throw new Error("useDesktop must be used inside a project desktop");
  return context;
}
