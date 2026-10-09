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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { RatioSettingsCard } from './ratio-settings-card'

const mocks = vi.hoisted(() => ({
  modelPricing: vi.fn(),
  saveModelPricing: vi.fn(() => ({
    isError: false,
    isPending: false,
    mutateAsync: vi.fn(),
    reset: vi.fn(),
  })),
  updateOption: vi.fn(() => ({ isPending: false, mutateAsync: vi.fn() })),
}))

vi.mock('@/features/model-pricing/api', async (importOriginal) => {
  const original =
    await importOriginal<typeof import('@/features/model-pricing/api')>()
  return {
    ...original,
    useModelPricing: mocks.modelPricing,
    useSaveModelPricing: mocks.saveModelPricing,
  }
})

vi.mock('../hooks/use-update-option', () => ({
  useUpdateOption: mocks.updateOption,
}))

vi.mock('./group-ratio-form', () => ({
  GroupRatioForm: () => <div data-testid='group-ratio-form' />,
}))

vi.mock('./model-ratio-form', () => ({
  ModelRatioForm: () => <div data-testid='model-ratio-form' />,
}))

vi.mock('./tool-price-settings', () => ({
  ToolPriceSettings: () => <div data-testid='tool-price-settings' />,
}))

vi.mock('./upstream-ratio-sync', () => ({
  UpstreamRatioSync: () => <div data-testid='upstream-ratio-sync' />,
}))

const modelDefaults = {
  ModelPrice: '{}',
  ModelRatio: '{}',
  CacheRatio: '{}',
  CreateCacheRatio: '{}',
  CompletionRatio: '{}',
  ImageRatio: '{}',
  AudioRatio: '{}',
  AudioCompletionRatio: '{}',
  ExposeRatioEnabled: false,
  BillingMode: '{}',
  BillingExpr: '{}',
  DisplayBillingExpr: '{}',
  PluginBillingExpr: '{}',
}

const groupDefaults = {
  GroupRatio: '{}',
  TopupGroupRatio: '{}',
  UserUsableGroups: '{}',
  GroupDisplayNames: '{}',
  GroupGroupRatio: '{}',
  AutoGroups: '[]',
  MaxTokenAutoGroups: 5,
  AutoGroupProfiles: '{}',
  DefaultUseAutoGroup: false,
  GroupSpecialUsableGroup: '{}',
}

function renderCard(visibleTabs: Array<'groups' | 'models'>) {
  const queryClient = new QueryClient()
  return render(
    <QueryClientProvider client={queryClient}>
      <RatioSettingsCard
        modelDefaults={modelDefaults}
        groupDefaults={groupDefaults}
        toolPricesDefault='{}'
        visibleTabs={visibleTabs}
      />
    </QueryClientProvider>
  )
}

describe('RatioSettingsCard model pricing query', () => {
  test('does not load model pricing on the group-only page', () => {
    mocks.modelPricing.mockReturnValue({
      data: undefined,
      isError: false,
      refetch: vi.fn(),
    })
    renderCard(['groups'])

    expect(mocks.modelPricing).toHaveBeenCalledWith([], false)
  })
})
