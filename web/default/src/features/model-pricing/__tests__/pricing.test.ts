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
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import {
  buildPricingChanges,
  prepareExpressionPricingChanges,
  previewModelPricingConversion,
  saveModelPricing,
  type ModelPricingConfig,
} from '../api'
import {
  applyPriceSyncSelections,
  applyPricingDraft,
  pricingFromDraft,
  pricingOptions,
  pricingRow,
} from '../pricing'

describe('shared model pricing', () => {
  it('preserves explicit zero prices and cache-write configuration', () => {
    expect(
      pricingFromDraft({
        name: 'example',
        billingMode: 'per-token',
        ratio: '0',
        completionRatio: '2',
        cacheRatio: '0',
        createCacheRatio: '1.25',
      })
    ).toEqual({
      'billing_setting.billing_mode': 'ratio',
      ModelRatio: 0,
      CompletionRatio: 2,
      CacheRatio: 0,
      CreateCacheRatio: 1.25,
    })
    expect(
      pricingFromDraft({
        name: 'example',
        billingMode: 'per-request',
        price: '',
      })
    ).not.toHaveProperty('ModelPrice')
    expect(
      pricingFromDraft({
        name: 'example',
        billingMode: 'per-request',
        price: '0',
      })
    ).toHaveProperty('ModelPrice', 0)
  })

  it('keeps token and task expressions intact through both editing and sync', () => {
    for (const expression of [
      'len <= 200000 ? tier("short", p * 2 + cr * 0.2 + cc * 2.5) : tier("long", p * 4)',
      'tier("base", u("seconds") * 0.4)',
    ]) {
      const values = {
        'billing_setting.billing_mode': 'tiered_expr',
        'billing_setting.billing_expr': expression,
        ModelRatio: 1,
      }
      expect(pricingFromDraft(pricingRow('example', values))).toEqual({
        'billing_setting.billing_mode': 'tiered_expr',
        'billing_setting.billing_expr': expression,
      })
    }
  })

  it('does not persist another model’s built-in display expression when editing one price', () => {
    const options = pricingOptions({
      ModelPrice: '{"edited":1}',
      BillingMode: '{"builtin":"tiered_expr"}',
      BillingExpr: '{"builtin":"tier(\\"base\\", p * 2)"}',
    })
    const snapshot: ModelPricingConfig = {
      options,
      empty_version: 'empty',
      entries: [
        {
          model_name: 'edited',
          version: 'v1',
          configured: { ModelPrice: 1 },
          effective: { ModelPrice: 1 },
        },
        {
          model_name: 'builtin',
          version: 'empty',
          configured: {},
          effective: {
            'billing_setting.billing_mode': 'tiered_expr',
            'billing_setting.billing_expr': 'tier("base", p * 2)',
          },
        },
      ],
    }
    const after = applyPricingDraft(options, {
      name: 'edited',
      billingMode: 'per-request',
      price: '2',
    })
    expect(buildPricingChanges(snapshot, options, after)).toEqual([
      {
        model_name: 'edited',
        expected_version: 'v1',
        pricing: { ModelPrice: 2, 'billing_setting.billing_mode': 'ratio' },
      },
    ])
  })

  it('clears conflicting expression settings when a fixed price is selected for sync', () => {
    const options = pricingOptions({
      ModelRatio: '{"example":1}',
      CreateCacheRatio: '{"example":1.25}',
      BillingMode: '{"example":"tiered_expr"}',
      BillingExpr: '{"example":"tier(\\"base\\", p * 2)"}',
    })
    const after = applyPriceSyncSelections(options, {
      example: { model_price: 0 },
    })
    expect(JSON.parse(after.ModelPrice)).toEqual({ example: 0 })
    expect(JSON.parse(after.ModelRatio)).toEqual({})
    expect(JSON.parse(after.CreateCacheRatio)).toEqual({})
    expect(JSON.parse(after['billing_setting.billing_expr'])).toEqual({})
    expect(JSON.parse(after['billing_setting.billing_mode'])).toEqual({
      example: 'ratio',
    })
  })

  it('rejects invalid prices instead of silently coercing them', () => {
    for (const price of ['-1', 'NaN', 'Infinity', 'invalid']) {
      expect(() =>
        pricingFromDraft({ name: 'example', billingMode: 'per-request', price })
      ).toThrow()
    }
  })
})

it('tracks nested provider prices by model, preserves :: in model names, and ignores key order', () => {
  const key = 'billing_setting.plugin_billing_expr'
  const before = pricingOptions({
    [key]: JSON.stringify({
      'alpha::shared::model': 'u("seconds")',
      'beta::shared::model': 'u("credits")',
      'alpha::other': '1',
    }),
  })
  const snapshot: ModelPricingConfig = {
    options: before,
    empty_version: 'empty',
    entries: [
      {
        model_name: 'shared::model',
        version: 'v1',
        configured: { [key]: { alpha: 'u("seconds")', beta: 'u("credits")' } },
        effective: {},
      },
    ],
  }
  const reordered = {
    ...before,
    [key]: JSON.stringify({
      'alpha::other': '1',
      'beta::shared::model': 'u("credits")',
      'alpha::shared::model': 'u("seconds")',
    }),
  }
  expect(buildPricingChanges(snapshot, before, reordered)).toEqual([])
  const after = {
    ...before,
    [key]: JSON.stringify({
      'beta::shared::model': 'u("credits") * 2',
      'alpha::other': '1',
    }),
  }
  expect(buildPricingChanges(snapshot, before, after)).toEqual([
    {
      model_name: 'shared::model',
      expected_version: 'v1',
      pricing: { [key]: { beta: 'u("credits") * 2' } },
    },
  ])
  const draft = pricingRow('shared::model', snapshot.entries[0].configured)
  expect(pricingFromDraft(draft)[key]).toEqual(
    snapshot.entries[0].configured[key]
  )
  expect(
    pricingRow('alpha::model', {
      ModelPrice: 0.25,
      'billing_setting.plugin_billing_expr': { beta: 'u("images")' },
    })
  ).toMatchObject({
    name: 'alpha::model',
    price: '0.25',
    billingMode: 'per-request',
  })
})

it('retains provider overrides during model-only price synchronization', () => {
  const key = 'billing_setting.plugin_billing_expr'
  const options = pricingOptions({
    [key]: JSON.stringify({ 'alpha::example': 'u("seconds") * 2' }),
  })
  const after = applyPriceSyncSelections(options, {
    example: { billing_expr: 'tier("base", u("seconds"))' },
  })
  expect(after[key]).toEqual(options[key])
})

it('commits source provider edits while preserving target providers during batch copy', () => {
  const key = 'billing_setting.plugin_billing_expr'
  const options = pricingOptions({
    [key]: JSON.stringify({ 'alpha::source': '1', 'beta::target': '2' }),
  })
  const after = applyPricingDraft(
    options,
    {
      name: 'source',
      billingMode: 'tiered_expr',
      billingExpr: 'tier("base", u("seconds"))',
      pluginBillingExpr: { alpha: '3' },
    },
    ['source', 'target']
  )
  expect(JSON.parse(after[key])).toEqual({
    'alpha::source': '3',
    'beta::target': '2',
  })
})

it('saves, copies, and clears cache display expressions through the managed pricing contract', () => {
  const actual = 'tier("standard", p * 10 + cr * 2)'
  const display = 'tier("standard", p * 10 + cr * 0)'
  const requestRuleExpr = 'header("fast") == "true" ? 2 : 1'
  const draft = {
    name: 'source',
    billingMode: 'tiered_expr' as const,
    billingExpr: actual,
    displayBillingExpr: display,
    requestRuleExpr,
  }
  const values = pricingFromDraft(draft)
  expect(values['billing_setting.display_billing_expr']).toContain('cr * 0')
  expect(pricingFromDraft(pricingRow('source', values))).toEqual(values)
  const before = pricingOptions({})
  const snapshot: ModelPricingConfig = {
    options: before,
    empty_version: 'empty',
    entries: [],
  }
  const after = applyPricingDraft(before, draft, ['source', 'copy'])
  const changes = buildPricingChanges(snapshot, before, after)
  expect(changes).toHaveLength(2)
  for (const change of changes) expect(change.pricing).toEqual(values)
  const withoutDisplay = applyPricingDraft(after, {
    ...draft,
    displayBillingExpr: '',
  })
  const configured = {
    ...snapshot,
    entries: [
      {
        model_name: 'source',
        version: 'v1',
        configured: values,
        effective: values,
      },
    ],
  }
  expect(buildPricingChanges(configured, after, withoutDisplay)).toEqual([
    {
      model_name: 'source',
      expected_version: 'v1',
      pricing: {
        'billing_setting.billing_mode': 'tiered_expr',
        'billing_setting.billing_expr': values['billing_setting.billing_expr'],
      },
    },
  ])
})

describe('model pricing save contract', () => {
  afterEach(() => vi.restoreAllMocks())

  it('removes legacy prices from an expression save and keeps provider/display prices', async () => {
    const values = {
      'billing_setting.billing_mode': 'tiered_expr',
      'billing_setting.billing_expr': 'tier("base", p * 8 + c * 28 + cr * 2)',
      'billing_setting.display_billing_expr':
        'tier("base", p * 8 + c * 28 + cr * 1)',
      'billing_setting.plugin_billing_expr': {
        provider: 'tier("base", u("seconds") * 0.2)',
      },
      ModelPrice: 3,
      ModelRatio: 4,
      CompletionRatio: 3.5,
      CacheRatio: 0.25,
      CreateCacheRatio: 1.25,
      ImageRatio: 1,
      AudioRatio: 2,
      AudioCompletionRatio: 3,
    }
    const post = vi.spyOn(api, 'post')
    const patch = vi.spyOn(api, 'patch').mockResolvedValue({
      data: { success: true },
    })
    await saveModelPricing(
      await prepareExpressionPricingChanges([
        { model_name: 'expression', expected_version: 'v1', pricing: values },
      ])
    )
    expect(patch).toHaveBeenCalledExactlyOnceWith('/api/option/model_pricing', {
      changes: [
        {
          model_name: 'expression',
          expected_version: 'v1',
          reset: false,
          pricing: Object.fromEntries(
            Object.entries(values).filter(([key]) =>
              key.startsWith('billing_setting.')
            )
          ),
        },
      ],
    })
    expect(post).not.toHaveBeenCalled()
  })

  it('saves legacy token, fixed and zero prices without converting their billing path', async () => {
    const changes = [
      {
        model_name: 'token',
        expected_version: 'v1',
        pricing: {
          ModelRatio: 4,
          CompletionRatio: 3.5,
          CacheRatio: 0.25,
          CreateCacheRatio: 1.25,
        },
      },
      {
        model_name: 'video',
        expected_version: 'v2',
        pricing: {
          ModelPrice: 0.6,
          'billing_setting.plugin_billing_expr': { provider: 'u("seconds")' },
        },
      },
      {
        model_name: 'free',
        expected_version: 'v3',
        pricing: { ModelPrice: 0, 'billing_setting.billing_mode': 'ratio' },
      },
    ]
    const post = vi.spyOn(api, 'post')
    const patch = vi.spyOn(api, 'patch').mockResolvedValue({
      data: { success: true },
    })
    await saveModelPricing(await prepareExpressionPricingChanges(changes))
    expect(patch).toHaveBeenCalledExactlyOnceWith('/api/option/model_pricing', {
      changes: changes.map((change) => ({ ...change, reset: false })),
    })
    expect(post).not.toHaveBeenCalled()
  })

  it('sends mixed resets unchanged so defaults and built-in expressions remain inherited', async () => {
    const resetChanges = [
      'gpt-4o',
      'gpt-4o-realtime-preview',
      'builtin',
      'unpriced',
    ].map((model_name) => ({
      model_name,
      expected_version: 'version',
      pricing: {},
      reset: true,
    }))
    const legacyChange = {
      model_name: 'edited',
      expected_version: 'v2',
      pricing: { ModelPrice: 0.25 },
    }
    const post = vi.spyOn(api, 'post')
    const patch = vi.spyOn(api, 'patch').mockResolvedValue({
      data: { success: true },
    })
    await saveModelPricing(
      await prepareExpressionPricingChanges([...resetChanges, legacyChange])
    )
    expect(patch).toHaveBeenCalledExactlyOnceWith('/api/option/model_pricing', {
      changes: [...resetChanges, { ...legacyChange, reset: false }],
    })
    expect(post).not.toHaveBeenCalled()
  })

  it.each([
    { failure: 'business error', message: 'Pricing changed; reload and retry' },
    { failure: 'transport error', message: 'Connection failed' },
  ])(
    'preserves a PATCH $failure for the complete batch',
    async ({ failure, message }) => {
      const post = vi.spyOn(api, 'post')
      const patch = vi.spyOn(api, 'patch')
      if (failure === 'transport error') {
        patch.mockRejectedValue(new Error(message))
      } else {
        patch.mockResolvedValue({ data: { success: false, message } })
      }
      const changes = [
        {
          model_name: 'token',
          expected_version: 'v1',
          pricing: { ModelRatio: 4 },
        },
        {
          model_name: 'reset',
          expected_version: 'v2',
          pricing: {},
          reset: true,
        },
      ]
      await expect(
        prepareExpressionPricingChanges(changes).then(saveModelPricing)
      ).rejects.toThrow(message)
      expect(patch).toHaveBeenCalledExactlyOnceWith(
        '/api/option/model_pricing',
        {
          changes: [{ ...changes[0], reset: false }, changes[1]],
        }
      )
      expect(post).not.toHaveBeenCalled()
    }
  )

  it('keeps explicit conversion previews available without saving them', async () => {
    const expression = 'tier("base", p * 8 + c * 28 + cr * 2)'
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      data: { success: true, data: { expression } },
    })
    const patch = vi.spyOn(api, 'patch')
    const request = {
      model_name: 'token',
      pricing: { ModelRatio: 4, CompletionRatio: 3.5, CacheRatio: 0.25 },
    }
    await expect(previewModelPricingConversion(request)).resolves.toEqual({
      expression,
    })
    expect(post).toHaveBeenCalledExactlyOnceWith(
      '/api/option/model_pricing/convert',
      request
    )
    expect(patch).not.toHaveBeenCalled()
  })
})
