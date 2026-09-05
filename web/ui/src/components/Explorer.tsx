import {useMemo, useState} from "react";
import {Icon, familyIcon} from "./Icons";
import {baseName, dirName} from "../lib/format";
import {familyKey, itemKey, useDesktop} from "../lib/desktop";
import type {TreeGroup, TreeItem} from "../types";

// Families whose group label opens a list view instead of only toggling.
const listFamilies = new Set(["requirements", "test-suites"]);

export default function Explorer({groups, activeKey}: {groups: TreeGroup[]; activeKey: string}) {
  const desktop = useDesktop();
  const [filter, setFilter] = useState("");
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({requirements: true});

  const needle = filter.trim().toLowerCase();
  const matches = useMemo(() => {
    if (!needle) return null;
    const found: TreeItem[] = [];
    for (const group of groups) {
      for (const item of group.items) {
        if (item.name.toLowerCase().includes(needle) || item.ref.toLowerCase().includes(needle)) found.push(item);
        for (const child of item.children || []) {
          if (child.name.toLowerCase().includes(needle) || child.ref.toLowerCase().includes(needle)) found.push(child);
        }
      }
    }
    return found;
  }, [groups, needle]);

  const total = groups.reduce((count, group) => count + group.items.length, 0);

  return (
    <div className="explorer">
      <div className="explorer-head">
        <label className="search">
          <Icon.Search size={14} />
          <input value={filter} onChange={(event) => setFilter(event.target.value)} placeholder={`Filter ${total} items`} aria-label="Filter items" />
          {filter && <button type="button" className="icon-btn tiny" onClick={() => setFilter("")} aria-label="Clear filter"><Icon.Close size={12} /></button>}
        </label>
      </div>
      <button
        type="button"
        className={`tree-row tree-overview ${activeKey === "overview" ? "selected" : ""}`}
        onClick={desktop.openOverview}
      >
        <Icon.Home size={15} />
        <span className="tree-name">Overview</span>
      </button>
      <div className="tree-scroll">
        {matches ? (
          <div className="tree-group open">
            <div className="tree-group-head static"><span className="tree-name">{matches.length} matching</span></div>
            {matches.map((item) => <TreeNode item={item} activeKey={activeKey} key={itemKey(item.family, item.ref)} />)}
            {matches.length === 0 && <div className="tree-empty">Nothing matches "{filter}"</div>}
          </div>
        ) : groups.map((group) => (
          <Group
            group={group}
            open={!collapsed[group.family]}
            onToggle={() => setCollapsed((current) => ({...current, [group.family]: !current[group.family]}))}
            activeKey={activeKey}
            key={group.family}
          />
        ))}
      </div>
    </div>
  );
}

function Group({group, open, onToggle, activeKey}: {group: TreeGroup; open: boolean; onToggle: () => void; activeKey: string}) {
  const desktop = useDesktop();
  const GroupIcon = familyIcon(group.family);
  const isList = listFamilies.has(group.family);
  const selected = activeKey === familyKey(group.family);
  return (
    <section className={`tree-group ${open ? "open" : ""}`}>
      <div className={`tree-group-head ${selected ? "selected" : ""}`}>
        <button type="button" className="tree-toggle" onClick={onToggle} aria-label={open ? "Collapse" : "Expand"} aria-expanded={open}>
          <Icon.Chevron size={12} className={open ? "rot90" : ""} />
        </button>
        <button
          type="button"
          className="tree-group-label"
          onClick={isList ? () => desktop.openFamily(group.family) : onToggle}
          title={isList ? `Open ${group.label.toLowerCase()} list` : undefined}
        >
          <GroupIcon size={14} className={`fam fam-${group.family}`} />
          <span className="tree-name">{group.label}</span>
          <span className="count">{group.items.length}</span>
        </button>
      </div>
      {open && (
        <div className="tree-children">
          {group.family === "reports" ? <ReportNodes items={group.items} activeKey={activeKey} /> : group.items.map((item) => (
            <div key={itemKey(item.family, item.ref)}>
              <TreeNode item={item} activeKey={activeKey} />
              {item.children?.map((child) => <TreeNode item={child} activeKey={activeKey} nested key={itemKey(child.family, child.ref)} />)}
            </div>
          ))}
          {group.items.length === 0 && <div className="tree-empty">None yet</div>}
        </div>
      )}
    </section>
  );
}

// ReportNodes groups report files by their folder (suite/version) so long
// timestamped names stay readable.
function ReportNodes({items, activeKey}: {items: TreeItem[]; activeKey: string}) {
  const folders = new Map<string, TreeItem[]>();
  for (const item of items) {
    const folder = dirName(item.name);
    folders.set(folder, [...(folders.get(folder) || []), item]);
  }
  return (
    <>
      {[...folders.entries()].map(([folder, files]) => (
        <div key={folder || "."}>
          {folder && <div className="tree-folder" title={folder}>{folder}</div>}
          {files.map((item) => (
            <TreeNode item={{...item, name: baseName(item.name)}} activeKey={activeKey} nested={Boolean(folder)} key={itemKey(item.family, item.ref)} />
          ))}
        </div>
      ))}
    </>
  );
}

function TreeNode({item, activeKey, nested = false}: {item: TreeItem; activeKey: string; nested?: boolean}) {
  const desktop = useDesktop();
  const key = itemKey(item.family, item.ref);
  const NodeIcon = familyIcon(item.family, item.artifactType);
  return (
    <button
      type="button"
      className={`tree-row tree-node ${nested ? "nested" : ""} ${activeKey === key ? "selected" : ""}`}
      onClick={() => desktop.openItem(item)}
      title={item.name}
    >
      <NodeIcon size={14} className={`fam fam-${item.family} type-${item.artifactType}`} />
      <span className="tree-name">{item.name}</span>
      {item.status && item.status !== "active" && item.status !== "ready" && (
        <span className={`dot dot-${item.status}`} title={item.status} />
      )}
      {item.version ? <small className="mono">v{item.version}</small> : null}
    </button>
  );
}
