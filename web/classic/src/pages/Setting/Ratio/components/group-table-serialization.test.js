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

import assert from 'node:assert/strict';
import { describe, test } from 'node:test';

import {
  buildGroupTableRows,
  serializeGroupTable,
} from './group-table-serialization.js';

const parseObject = (value) => JSON.parse(value);

describe('classic group table serialization', () => {
  test('keeps display-only labels without creating a ratio or selectable entry', () => {
    const rows = buildGroupTableRows(
      '{"stable": 0.5}',
      '{"selectable": "Selectable only"}',
      '{"auto-only": "Automatic plan"}',
      () => 'row',
    );

    const displayOnly = rows.find((row) => row.name === 'auto-only');
    displayOnly.displayName = 'Renamed automatic plan';

    const serialized = serializeGroupTable(rows);
    assert.deepEqual(parseObject(serialized.GroupRatio), { stable: 0.5 });
    assert.deepEqual(parseObject(serialized.UserUsableGroups), {
      selectable: 'Selectable only',
    });
    assert.deepEqual(parseObject(serialized.GroupDisplayNames), {
      'auto-only': 'Renamed automatic plan',
    });
  });

  test('adds a missing canonical field only when the field is explicitly edited', () => {
    const rows = buildGroupTableRows(
      '{"stable": 1}',
      '{}',
      '{"stable": "Stable plan"}',
      () => 'row',
    );
    const row = rows[0];
    row.selectable = true;
    row.description = 'Selectable now';
    row.editedFields.selectable = true;

    const serialized = serializeGroupTable(rows);
    assert.deepEqual(parseObject(serialized.GroupRatio), { stable: 1 });
    assert.deepEqual(parseObject(serialized.UserUsableGroups), {
      stable: 'Selectable now',
    });
  });

  test('preserves whitespace in existing identifiers', () => {
    const rows = buildGroupTableRows(
      '{" stable ": 1}',
      '{}',
      '{" stable ": "Legacy"}',
      () => 'row',
    );
    rows[0].displayName = 'New label';

    const serialized = serializeGroupTable(rows);
    assert.deepEqual(parseObject(serialized.GroupRatio), { ' stable ': 1 });
    assert.deepEqual(parseObject(serialized.GroupDisplayNames), {
      ' stable ': 'New label',
    });
  });
});
