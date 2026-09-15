import {useState} from "react";
import {familyActions, familyLabels, useDesktop} from "../lib/desktop";
import {baseName, dirName} from "../lib/format";
import type {TreeItem} from "../types";
import {Icon, familyIcon} from "./Icons";
import {Badge, EmptyState, StatusBadge} from "./ui";
import FileTree from "./FileTree";

export default function EntityListView({family, items}: {family: string; items: TreeItem[]}) {
  const desktop = useDesktop();
  const [search, setSearch] = useState("");
  const action = familyActions[family];
  const FamilyIcon = familyIcon(family);
  const fileFamily = family === "artifacts" || family === "reports";
  const needle = search.trim().toLowerCase();
  const visible = items.filter(item => `${item.name} ${item.ref} ${item.artifactType}`.toLowerCase().includes(needle));
  return (
    <div className="list-view">
      <header className="list-head">
        <div><h1>{familyLabels[family]}</h1><p className="muted">{action.description}</p></div>
        <button type="button" className="btn primary" onClick={() => desktop.openDialog(action.kind)}><Icon.Plus size={14} /> {action.label}</button>
      </header>
      <div className="filters">
        <label className="search"><Icon.Search size={14} /><input value={search} onChange={event => setSearch(event.target.value)} placeholder={`Search ${familyLabels[family].toLowerCase()}`} /></label>
        <span className="muted small">{visible.length} of {items.length}</span>
      </div>
      {items.length === 0 ? <EmptyState title={`No ${familyLabels[family].toLowerCase()} yet`} icon={<FamilyIcon size={26} />}>
        <button type="button" className="btn" onClick={() => desktop.openDialog(action.kind)}>{action.label}</button>
      </EmptyState> : visible.length === 0 ? <EmptyState title="No items match that search" /> : fileFamily && !needle ? <FileTree items={items} family={family} /> : <ul className="row-list">
        {visible.map(item => <li key={item.ref}>
          <button type="button" className="link-row" aria-label={item.name} onClick={() => desktop.openItem(item)}>
            <FamilyIcon size={16} className={`fam fam-${family}`} />
            <span className="row-text entity-file-label" title={item.name}>
              <span>{fileFamily ? baseName(item.name) : item.name}</span>
              {fileFamily && dirName(item.name) && <span className="muted small">{dirName(item.name)}/</span>}
            </span>
            <Badge>{item.artifactType}</Badge><StatusBadge status={item.status} />
            {item.version && <small className="mono muted">v{item.version}</small>}
          </button>
        </li>)}
      </ul>}
    </div>
  );
}
