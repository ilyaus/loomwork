import {createContext, useContext} from "react";
import type {TreeItem} from "../types";

// A tab is the overview, a family list (requirements, test suites), or one
// entity document. Keys are stable so reopening an item focuses its tab.
export type Tab =
  | {key: "overview"; kind: "overview"; name: string}
  | {key: string; kind: "family"; family: string; name: string}
  | {key: string; kind: "item"; family: string; ref: string; name: string; version?: number; artifactType?: string};

export const overviewTab: Tab = {key: "overview", kind: "overview", name: "Overview"};

export const familyKey = (family: string) => `family:${family}`;
export const itemKey = (family: string, ref: string) => `${family}:${ref}`;

export function itemTab(item: Pick<TreeItem, "family" | "ref" | "name"> & Partial<TreeItem>): Tab {
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
  "test-cases": "Test cases",
  reports: "Reports",
  artifacts: "Artifacts",
};

export type DesktopActions = {
  projectRef: string;
  openItem: (item: Pick<TreeItem, "family" | "ref" | "name"> & Partial<TreeItem>) => void;
  openFamily: (family: string) => void;
  openOverview: () => void;
  // openDialog shows one of the project's create/import forms; payload carries
  // the entity being edited when the form writes a new version.
  openDialog: (dialog: DialogKind, payload?: unknown) => void;
  refresh: () => void;
};

export type DialogKind = "requirement" | "artifact" | "suite" | "agent" | "rule" | "source";
export type DialogState = {kind: DialogKind; payload?: unknown} | null;

export const DesktopContext = createContext<DesktopActions | null>(null);

export function useDesktop(): DesktopActions {
  const context = useContext(DesktopContext);
  if (!context) throw new Error("useDesktop must be used inside a project desktop");
  return context;
}
