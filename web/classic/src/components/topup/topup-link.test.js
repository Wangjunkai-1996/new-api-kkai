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
  DEFAULT_TOP_UP_LINK,
  isSafeHttpCheckoutUrl,
  resolveTopUpLink,
} from './topup-link.js';

describe('classic topup link resolution', () => {
  test('keeps configured web links', () => {
    assert.equal(isSafeHttpCheckoutUrl('https://example.com/checkout'), true);
    assert.equal(
      resolveTopUpLink(' https://example.com/checkout '),
      'https://example.com/checkout',
    );
  });

  test('uses the fixed fallback for unsafe targets', () => {
    for (const value of [
      '',
      '/checkout',
      'javascript:alert(1)',
      'data:text/html,checkout',
      'ftp://example.com/checkout',
    ]) {
      assert.equal(resolveTopUpLink(value), DEFAULT_TOP_UP_LINK);
    }
  });
});
