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

  const byEntry = new Map(entries.map(row => [row.entry, row]));
  const byGroup = new Map(groups.map(({ group }) => [group, []]));
  const byTag = new Map();
  const used = new Map();
  const active = [];
  const favorites = [];
  const bin = [];
  for (const row of entries) {
    if (row.recycled) {
      bin.push(row);
      continue;
    }
    active.push(row);
    byGroup.get(row.group).push(row);
    const tags = new Set(row.entry.tags.map(tag => tag.trim()).filter(Boolean));
    if ([...tags].some(tag => tag.toLowerCase() === 'favorite')) favorites.push(row);
    for (const tag of tags) {
      if (tag.toLowerCase() === 'favorite') continue;
      if (!byTag.has(tag)) byTag.set(tag, []);
      byTag.get(tag).push(row);
    }
  }
  const builtIn = [
    { name: 'All Entries', rows: active },
    { name: 'Favorites', rows: favorites },
    { name: 'Recent', rows: active, recent: true },
    { name: 'Bin', rows: bin, bin: true }
  ];
  return {
    builtIn,
    counts: {
      entries: active.length,
      favorites: favorites.length,
      recycled: bin.length,
      groups: groups.length,
      tags: byTag.size,
      attachments: active.reduce((total, { entry }) => total + entry.binaries.size, 0),
    },
    tags: [...byTag.keys()].sort().map(name => ({ name, rows: byTag.get(name) })),
    groups: groups.map(({ name, depth, group }) => ({
      name, depth, rows: byGroup.get(group)
    })),
    row(entry) { return byEntry.get(entry); },
    recordUse(entry) { if (entry) used.set(entry, Date.now()); },
    rows(view) {
      const { rows, recent } = view ?? builtIn[0];
      if (!recent) return rows;
      const last = ({ entry }) => Math.max(entry.lastModTime || 0, used.get(entry) || 0);
      return [...rows].sort((a, b) => last(b) - last(a));
    }
  };
}
