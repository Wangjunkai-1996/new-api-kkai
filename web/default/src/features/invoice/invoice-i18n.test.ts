import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { expect, test } from 'vitest'

test('invoice minimum and submission recovery copy is translated in every supported locale', () => {
  const keys = [
    'Minimum per request: {{amount}}. Eligible payments can be combined.',
    'Select {{amount}} more in eligible payments to continue.',
    'Add eligible payments to reach the minimum invoice amount.',
    'Check submission result',
    'Your submission result is not confirmed. Check the result before editing or submitting another request.',
    'Payment evidence, invoice information and policy compatibility will be checked again. Resuming adopts the current policy version and restores the previous processing stage.',
  ]
  for (const locale of ['en', 'zh', 'zh-TW', 'fr', 'ja', 'ru', 'vi']) {
    const translations = JSON.parse(
      readFileSync(resolve('src/i18n/locales', `${locale}.json`), 'utf8')
    ).translation as Record<string, string>
    for (const key of keys) {
      expect(translations[key], `${locale}: ${key}`).toBeTruthy()
      if (locale !== 'en') expect(translations[key]).not.toBe(key)
      expect(translations[key].match(/\{\{\w+\}\}/g) || []).toEqual(
        key.match(/\{\{\w+\}\}/g) || []
      )
    }
  }
})
