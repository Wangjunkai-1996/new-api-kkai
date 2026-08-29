/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

function parseJSON(str, fallback) {
  if (!str || !str.trim()) return fallback;
  try {
    return JSON.parse(str);
  } catch {
    return fallback;
  }
}

function hasOwn(object, key) {
  return Object.prototype.hasOwnProperty.call(object, key);
}

/**
 * Build editable rows while retaining which source maps actually contain a
 * key. A group can be referenced by a display label without being a pricing,
 * selectable, or top-up group; those rows must stay display-only until an
 * operator explicitly enables another field.
 */
export function buildGroupTableRows(
  groupRatioStr,
  userUsableGroupsStr,
  groupDisplayNamesStr,
  createId,
) {
  const ratioMap = parseJSON(groupRatioStr, {});
  const usableMap = parseJSON(userUsableGroupsStr, {});
  const displayNamesMap = parseJSON(groupDisplayNamesStr, {});

  const allNames = new Set([
    ...Object.keys(ratioMap),
    ...Object.keys(usableMap),
    ...Object.keys(displayNamesMap),
  ]);

  return Array.from(allNames).map((name) => ({
    _id: createId(),
    name,
    displayName:
      typeof displayNamesMap[name] === 'string' ? displayNamesMap[name] : '',
    // Keep the old visual default for the input, but do not persist it unless
    // this key was present originally or the ratio is explicitly edited.
    ratio: ratioMap[name] ?? 1,
    selectable: hasOwn(usableMap, name),
    description: usableMap[name] ?? '',
    hasRatio: hasOwn(ratioMap, name),
    hasUserUsable: hasOwn(usableMap, name),
    editedFields: {},
  }));
}

/**
 * Serialize rows without materializing missing canonical entries. Existing
 * rows are round-tripped by their exact identifier; only newly-added names
 * are trimmed before persistence.
 */
export function serializeGroupTable(rows) {
  const groupRatio = {};
  const userUsableGroups = {};
  const groupDisplayNames = {};

  rows.forEach((row) => {
    const rawName = String(row.name || '');
    const name = row.isNew ? rawName.trim() : rawName;
    if (!name) return;

    const editedFields = row.editedFields || {};
    if (row.isNew || row.hasRatio || editedFields.ratio) {
      groupRatio[name] = row.ratio;
    }

    const displayName = String(row.displayName || '').trim();
    if (displayName) {
      groupDisplayNames[name] = displayName;
    }

    if (
      row.isNew ||
      row.hasUserUsable ||
      editedFields.selectable ||
      editedFields.description
    ) {
      if (row.selectable) {
        userUsableGroups[name] = row.description;
      }
    }
  });

  return {
    GroupRatio: JSON.stringify(groupRatio, null, 2),
    UserUsableGroups: JSON.stringify(userUsableGroups, null, 2),
    GroupDisplayNames: JSON.stringify(groupDisplayNames, null, 2),
  };
}
