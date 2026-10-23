/*
Copyright (C) 2023-2026 QuantumNous

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
import { readFileSync } from 'node:fs'
import path from 'node:path'

import { describe, expect, it } from 'vitest'

import { PAYMENT_BLOCK_LABELS, PAYMENT_STATUS_LABELS } from './lib/payment-flow'

const sources = [
  'src/features/wallet/index.tsx',
  'src/features/wallet/components/current-topup-order.tsx',
  'src/features/wallet/components/payment-amounts.tsx',
  'src/features/wallet/components/payment-evidence.tsx',
  'src/features/wallet/components/dialogs/billing-history-dialog.tsx',
  'src/features/system-info/components/wallet-topup-reconcile-run.tsx',
]
const keys = new Set<string>([
  ...Object.values(PAYMENT_STATUS_LABELS),
  ...Object.values(PAYMENT_BLOCK_LABELS),
])
for (const file of sources) {
  const source = readFileSync(path.resolve(file), 'utf8')
  for (const match of source.matchAll(
    /\bt\(\s*(['"])((?:\\.|(?!\1).)*?)\1/gs
  )) {
    if (match[2]) keys.add(match[2].replaceAll("\\'", "'"))
  }
  for (const match of source.matchAll(/label: '([^']+)'/g)) {
    if (match[1]) keys.add(match[1])
  }
  for (const name of [
    'GROUPS',
    'RUN_LABELS',
    'COUNT_LABELS',
    'RECEIPT_STATUS_LABELS',
  ]) {
    const block = new RegExp(
      `const ${name}[^=]*=\\s*\\{([\\s\\S]*?)\\n\\}`
    ).exec(source)?.[1]
    if (block) {
      for (const match of block.matchAll(/:\s*'([^']+)'/g)) {
        if (match[1]) keys.add(match[1])
      }
    }
  }
}
describe('wallet lifecycle translations', () => {
  it.each(['en', 'zh', 'zh-TW', 'fr', 'ja', 'ru', 'vi'])(
    'provides all wallet and task labels and placeholders in %s',
    (locale) => {
      const translations = JSON.parse(
        readFileSync(path.resolve(`src/i18n/locales/${locale}.json`), 'utf8')
      ).translation as Record<string, string>
      for (const key of keys) {
        expect(translations[key], `${locale}: ${key}`).toBeTruthy()
        const expected = [...key.matchAll(/\{\{([^}]+)\}\}/g)]
          .map((match) => match[1])
          .sort()
        const actual = [
          ...(translations[key] ?? '').matchAll(/\{\{([^}]+)\}\}/g),
        ]
          .map((match) => match[1])
          .sort()
        expect(actual, `${locale}: ${key}`).toEqual(expected)
      }
      if (locale !== 'en') {
        expect(translations['Return to top-up']).not.toBe('Return to top-up')
        expect(translations['Top-up ended']).not.toBe('Top-up ended')
        expect(translations['Top-up timed out']).not.toBe('Top-up timed out')
        expect(translations['End this top-up']).not.toBe('End this top-up')
        expect(
          translations[
            'This order timed out and was automatically cancelled. It can no longer be paid.'
          ]
        ).not.toBe(
          'This order timed out and was automatically cancelled. It can no longer be paid.'
        )
      }
    }
  )
})
