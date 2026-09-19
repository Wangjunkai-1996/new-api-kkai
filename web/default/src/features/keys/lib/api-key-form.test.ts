import { describe, expect, test } from 'vitest'

import {
  getApiKeyFormDefaultValues,
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
})
