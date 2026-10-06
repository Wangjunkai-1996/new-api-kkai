import i18next from 'i18next'
import { describe, expect, test } from 'vitest'

import { apiKeySchema } from '../types'
import {
  getApiKeyFormDefaultValues,
  getApiKeyFormSchema,
  transformApiKeyToFormDefaults,
  transformFormDataToPayload,
} from './api-key-form'

describe('API key group retry defaults', () => {
  const autoGroups = new Set(['auto', 'auto2'])

  test('keeps cross-group retry enabled for an automatic group', () => {
    const payload = transformFormDataToPayload(
      {
        ...getApiKeyFormDefaultValues(false),
        name: 'auto2 key',
        group: 'auto2',
        cross_group_retry: true,
      },
      autoGroups
    )

    expect(payload.cross_group_retry).toBe(true)
  })

  test('preserves an explicit retry disable for an automatic group', () => {
    const payload = transformFormDataToPayload(
      {
        ...getApiKeyFormDefaultValues(false),
        name: 'auto2 key',
        group: 'auto2',
        cross_group_retry: false,
      },
      autoGroups
    )

    expect(payload.cross_group_retry).toBe(false)
  })

  test('always disables cross-group retry for an ordinary group', () => {
    const payload = transformFormDataToPayload(
      {
        ...getApiKeyFormDefaultValues(false),
        name: 'default key',
        group: 'default',
        cross_group_retry: true,
      },
      autoGroups
    )

    expect(payload.cross_group_retry).toBe(false)
  })
  test('preserves per-token order within a named automatic profile and clears it for a fixed group', () => {
    const values = {
      ...getApiKeyFormDefaultValues(false),
      name: 'profile key',
      group: 'auto2',
      auto_groups_mode: 'custom' as const,
      auto_groups: ['vip', 'default'],
    }
    expect(transformFormDataToPayload(values, autoGroups).auto_groups).toEqual([
      'vip',
      'default',
    ])
    expect(
      transformFormDataToPayload({ ...values, group: 'default' }, autoGroups)
        .auto_groups
    ).toEqual([])
  })

  test('rejects duplicate, unavailable, empty or oversized custom orders for a named profile', () => {
    const schema = getApiKeyFormSchema(i18next.t, 2, autoGroups, [
      'default',
      'vip',
    ])
    const values = {
      ...getApiKeyFormDefaultValues(false),
      name: 'profile key',
      group: 'auto2',
      auto_groups_mode: 'custom' as const,
    }
    for (const auto_groups of [
      [],
      ['default', 'default'],
      ['unavailable'],
      ['default', 'vip', 'extra'],
    ]) {
      expect(schema.safeParse({ ...values, auto_groups }).success).toBe(false)
    }
    expect(
      schema.safeParse({ ...values, auto_groups: ['vip', 'default'] }).success
    ).toBe(true)
  })

  test('retains a saved unavailable group so the user can explicitly repair it before saving', () => {
    const key = apiKeySchema.parse({
      id: 1,
      name: 'profile key',
      key: 'masked',
      status: 1,
      remain_quota: 0,
      used_quota: 0,
      unlimited_quota: true,
      expired_time: -1,
      created_time: 1,
      accessed_time: 1,
      group: 'auto2',
      auto_groups: ['retired', 'default'],
      model_limits_enabled: false,
    })
    const values = transformApiKeyToFormDefaults(key)
    expect(values.auto_groups).toEqual(['retired', 'default'])
    expect(values.auto_groups_mode).toBe('custom')
    expect(
      getApiKeyFormSchema(i18next.t, 5, autoGroups, ['default']).safeParse(
        values
      ).success
    ).toBe(false)
  })
})
