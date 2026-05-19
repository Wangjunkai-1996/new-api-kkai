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
import assert from 'node:assert/strict'
import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import vm from 'node:vm'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)
const repoRoot = path.resolve(__dirname, '../../..')

const scriptFiles = [
  'web/classic/src/components/table/tokens/modals/CCSwitchModal.jsx',
  'web/default/src/features/keys/components/dialogs/cc-switch-dialog.tsx',
]

const extractUsageScript = async (filePath) => {
  const source = await fs.readFile(path.join(repoRoot, filePath), 'utf8')
  const match = source.match(/const CC_SWITCH_TOKEN_USAGE_SCRIPT = `([\s\S]*?)`/)
  assert.ok(match, `${filePath} contains CC_SWITCH_TOKEN_USAGE_SCRIPT`)
  return match[1]
}

const [classicScript, defaultScript] = await Promise.all(
  scriptFiles.map(extractUsageScript)
)

assert.equal(
  defaultScript,
  classicScript,
  'classic and default CC Switch usage scripts stay identical'
)

const usageConfig = vm.runInNewContext(classicScript)
assert.equal(usageConfig.request.url, '{{baseUrl}}/api/usage/token/')
assert.equal(usageConfig.request.method, 'GET')
assert.equal(
  usageConfig.request.headers.Authorization,
  'Bearer {{apiKey}}'
)

const quotaPerUnit = 500000

const baseData = {
  user_total_used: quotaPerUnit,
  user_total_available: quotaPerUnit * 2,
  user_total_granted: quotaPerUnit * 3,
  token_total_available: quotaPerUnit * 2,
  quota_per_unit: quotaPerUnit,
  quota_display_type: 'USD',
  usd_exchange_rate: 7.2,
  custom_currency_exchange_rate: 3,
  custom_currency_symbol: 'CUSTOM',
}

const response = (data) => ({
  code: true,
  data: {
    ...baseData,
    ...data,
  },
})

const assertClose = (actual, expected, label) => {
  assert.ok(
    Math.abs(actual - expected) < Number.EPSILON * 100,
    `${label}: expected ${expected}, got ${actual}`
  )
}

const runCase = (name, input, verify) => {
  const result = usageConfig.extractor(input)
  verify(result)
  console.log(`ok - ${name}`)
}

runCase(
  'token_is_valid=false wins before balance checks',
  response({
    token_is_valid: false,
    token_invalid_reason: 'Token disabled',
    user_total_available: 0,
    token_total_available: 0,
  }),
  (result) => {
    assert.equal(result.isValid, false)
    assert.equal(result.invalidMessage, 'Token disabled')
  }
)

runCase(
  'token_is_valid=false uses default invalid reason',
  response({
    token_is_valid: false,
  }),
  (result) => {
    assert.equal(result.isValid, false)
    assert.equal(result.invalidMessage, 'Token unavailable')
  }
)

runCase(
  'user balance exhausted',
  response({
    user_total_available: 0,
    token_total_available: quotaPerUnit,
  }),
  (result) => {
    assert.equal(result.isValid, false)
    assert.equal(result.invalidMessage, 'User balance exhausted')
  }
)

runCase(
  'finite token exhausted while user has balance',
  response({
    user_total_available: quotaPerUnit,
    token_total_available: 0,
  }),
  (result) => {
    assert.equal(result.isValid, false)
    assert.equal(result.invalidMessage, 'Token unavailable')
  }
)

runCase(
  'unlimited token relies on user balance',
  response({
    user_total_available: quotaPerUnit,
    user_total_granted: quotaPerUnit * 2,
    user_total_used: quotaPerUnit,
    token_total_available: -1,
    unlimited_quota: true,
  }),
  (result) => {
    assert.equal(result.isValid, true)
    assertClose(result.remaining, 1, 'remaining')
    assertClose(result.total, 2, 'total')
    assertClose(result.used, 1, 'used')
  }
)

runCase(
  'legacy total_* response fallback',
  {
    code: true,
    data: {
      total_available: quotaPerUnit * 1.5,
      total_granted: quotaPerUnit * 2,
      total_used: quotaPerUnit * 0.5,
      quota_per_unit: quotaPerUnit,
      quota_display_type: 'USD',
    },
  },
  (result) => {
    assert.equal(result.isValid, true)
    assertClose(result.remaining, 1.5, 'remaining')
    assertClose(result.total, 2, 'total')
    assertClose(result.used, 0.5, 'used')
    assert.equal(result.unit, 'USD')
  }
)

runCase('USD conversion', response({ quota_display_type: 'USD' }), (result) => {
  assert.equal(result.unit, 'USD')
  assertClose(result.remaining, 2, 'remaining')
  assertClose(result.total, 3, 'total')
  assertClose(result.used, 1, 'used')
})

runCase('CNY conversion', response({ quota_display_type: 'CNY' }), (result) => {
  assert.equal(result.unit, 'CNY')
  assertClose(result.remaining, 14.4, 'remaining')
  assertClose(result.total, 21.6, 'total')
  assertClose(result.used, 7.2, 'used')
})

runCase(
  'CUSTOM conversion',
  response({
    quota_display_type: 'CUSTOM',
    custom_currency_exchange_rate: 3,
    custom_currency_symbol: 'POINTS',
  }),
  (result) => {
    assert.equal(result.unit, 'POINTS')
    assertClose(result.remaining, 6, 'remaining')
    assertClose(result.total, 9, 'total')
    assertClose(result.used, 3, 'used')
  }
)
