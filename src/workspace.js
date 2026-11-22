export function vaultWorkspace(database) {
  const groups = [];
  const entries = [];
  function visit(group, path = [], inBin = false, searchable = true) {
    const recycled = inBin || group.uuid.id === database.meta.recycleBinUuid?.id;
    const canSearch = group.enableSearching ?? searchable;
    const name = group.name || 'Unnamed group';
    if (!recycled) groups.push({ name, depth: path.length, group });
    const nextPath = [...path, name];
    for (const entry of group.entries) entries.push({ entry, group, path: nextPath, recycled, searchable: canSearch });
    for (const child of group.groups) visit(child, nextPath, recycled, canSearch);
  }
  for (const root of database.groups) visit(root);

  const tags = new Set(entries.filter(row => !row.recycled).flatMap(({ entry }) =>
    entry.tags.map(tag => tag.trim()).filter(tag => tag && tag.toLowerCase() !== 'favorite')
  ));
  const used = new Map();
  const active = entries.filter(row => !row.recycled);
  const builtIn = [
    { name: 'All Entries', rows: active },
    { name: 'Favorites', rows: active.filter(({ entry }) => entry.tags.some(tag => tag.trim().toLowerCase() === 'favorite')) },
    { name: 'Recent', rows: active, recent: true },
    { name: 'Bin', rows: entries.filter(row => row.recycled), bin: true }
  ];
  return {
    builtIn,
    tags: [...tags].sort().map(name => ({
      name, rows: active.filter(({ entry }) => entry.tags.some(tag => tag.trim() === name))
    })),
    groups: groups.map(({ name, depth, group }) => ({
      name, depth, rows: active.filter(row => row.group === group)
    })),
    recordUse(entry) { if (entry) used.set(entry, Date.now()); },
    rows(view) {
      const { rows, recent } = view ?? builtIn[0];
      if (!recent) return rows;
      const last = ({ entry }) => Math.max(entry.lastModTime || 0, used.get(entry) || 0);
      return [...rows].sort((a, b) => last(b) - last(a));
    }
  };
}
