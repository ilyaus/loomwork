import {useEffect, useMemo, useState} from "react";
import {itemKey, useDesktop} from "../lib/desktop";
import type {TreeItem} from "../types";
import {familyIcon, Icon} from "./Icons";

type Folder = {name: string; path: string; folders: Map<string, Folder>; files: TreeItem[]; count: number};

function foldersFor(items: TreeItem[]): Folder {
  const root: Folder = {name: "", path: "", folders: new Map(), files: [], count: items.length};
  for (const item of items) {
    let parent = root;
    const parts = item.name.split("/");
    for (const name of parts.slice(0, -1)) {
      let folder = parent.folders.get(name);
      if (!folder) {
        folder = {name, path: parent.path ? `${parent.path}/${name}` : name, folders: new Map(), files: [], count: 0};
        parent.folders.set(name, folder);
      }
      folder.count++;
      parent = folder;
    }
    parent.files.push(item);
  }
  return root;
}

function readExpanded(key: string): Record<string, boolean> {
  try {
    const value: unknown = JSON.parse(sessionStorage.getItem(key) || "{}");
    if (value && typeof value === "object" && !Array.isArray(value)) {
      return Object.fromEntries(Object.entries(value).filter(([, open]) => typeof open === "boolean"));
    }
  } catch {}
  return {};
}

export default function FileTree({items, family, activeKey = "", compact = false}: {items: TreeItem[]; family: string; activeKey?: string; compact?: boolean}) {
  const desktop = useDesktop();
  const tree = useMemo(() => foldersFor(items), [items]);
  const storageKey = `loomwork.folders.${desktop.projectRef}.${family}.${compact ? "explorer" : "list"}`;
  const [expanded, setExpanded] = useState(() => readExpanded(storageKey));
  useEffect(() => { sessionStorage.setItem(storageKey, JSON.stringify(expanded)); }, [storageKey, expanded]);

  function render(folder: Folder, depth: number) {
    return <ul className="file-tree-level">
      {[...folder.folders.values()].sort((a, b) => a.name.localeCompare(b.name)).map(child => {
        const open = typeof expanded[child.path] === "boolean" ? expanded[child.path] : depth === 0;
        return <li key={child.path}>
          <button type="button" className="tree-row file-folder-button" style={{paddingLeft: 12 + depth * 14}} data-path={child.path} aria-expanded={open} aria-label={`${open ? "Collapse" : "Expand"} folder ${child.path}`} title={child.path} onClick={() => setExpanded(current => ({...current, [child.path]: !open}))}>
            <Icon.Chevron size={12} className={open ? "rot90" : ""} /><Icon.Folder size={14} />
            <span className="tree-name">{child.name}</span><span className="count">{child.count}</span>
          </button>
          {open && render(child, depth + 1)}
        </li>;
      })}
      {[...folder.files].sort((a, b) => a.name.localeCompare(b.name)).map(item => {
        const ItemIcon = familyIcon(item.family, item.artifactType);
        return <li key={itemKey(item.family, item.ref)}>
          <button type="button" className={`tree-row file-entry ${activeKey === itemKey(item.family, item.ref) ? "selected" : ""}`} style={{paddingLeft: 38 + depth * 14}} title={item.name} aria-label={item.name} onClick={() => desktop.openItem(item)}>
            <ItemIcon size={14} className={`fam fam-${item.family} type-${item.artifactType}`} />
            <span className="tree-name">{item.name.split("/").pop()}</span>
            {item.version ? <small className="mono">v{item.version}</small> : null}
          </button>
        </li>;
      })}
    </ul>;
  }

  return <div className={`file-tree ${compact ? "compact" : ""}`}>{render(tree, 0)}</div>;
}
