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
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, test, vi } from 'vitest'

import type { UpdateOptionRequest } from '@/features/system-settings/types'
import { api } from '@/lib/api'

import { ImagePricingAdmin } from './image-pricing-admin'
import type { ImagePricingPolicy } from './image-pricing-policy'

const initialPolicy: ImagePricingPolicy = {
  version: 'original',
  enabled: true,
  audit_label: 'preserved',
  models: {
    'gpt-image-2': {
      default_size: '1024x1024',
      allow_custom_size: true,
      tiers: {
        '1k': { unit_price: 0.67, sizes: ['1024x1024'] },
        '2k': { unit_price: 1, sizes: ['2048x2048'] },
        '4k': { unit_price: 1.34, sizes: ['3840x2160'] },
      },
    },
    'gpt-image-2.5-flare': {
      default_size: '1024x1024',
      tiers: {
        '1k': { unit_price: 0.12, sizes: ['1024x1024'] },
        '2k': { unit_price: 0.24, sizes: ['2048x2048'] },
        '4k': { unit_price: 0.48, sizes: ['3840x2160'] },
      },
    },
    'gpt-image-2.5-sunburst': {
      default_size: '1024x1024',
      tiers: {
        '1k': { unit_price: 0.15, sizes: ['1024x1024'] },
        '2k': { unit_price: 0.3, sizes: ['2048x2048'] },
        '4k': { unit_price: 0.6, sizes: ['3840x2160'] },
      },
    },
    'other-tier-model': {
      default_size: '512x512',
      tiers: { standard: { unit_price: 0.1, sizes: ['512x512'] } },
    },
  },
}

test('edits selected models independently, preserves selection after save, and protects unsaved changes', async () => {
  const user = userEvent.setup()
  let storedPolicy = initialPolicy
  vi.spyOn(api, 'get')
    .mockResolvedValueOnce({
      data: {
        success: true,
        data: [
          { key: 'ImagePricingPolicy', value: JSON.stringify(storedPolicy) },
        ],
      },
    })
    .mockImplementation(() => new Promise(() => {}))
  const put = vi.spyOn(api, 'put').mockImplementation(async (_url, request) => {
    storedPolicy = JSON.parse(
      String((request as UpdateOptionRequest).value)
    ) as ImagePricingPolicy
    return { data: { success: true } }
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <ImagePricingAdmin />
    </QueryClientProvider>
  )
  await screen.findByRole('combobox', { name: 'Model' })
  await user.selectOptions(
    screen.getByRole('combobox', { name: 'Model' }),
    'gpt-image-2.5-flare'
  )
  expect(
    screen.getByRole('spinbutton', {
      name: 'imageStudio.admin.pricing.price1k',
    })
  ).toHaveValue(0.12)
  expect(
    screen.getByRole('spinbutton', {
      name: 'imageStudio.admin.pricing.price2k',
    })
  ).toHaveValue(0.24)
  expect(
    screen.getByRole('spinbutton', {
      name: 'imageStudio.admin.pricing.price4k',
    })
  ).toHaveValue(0.48)
  await user.click(
    screen.getByRole('switch', { name: 'imageStudio.admin.customSize' })
  )
  await user.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
  )
  expect(put).toHaveBeenCalledTimes(1)
  expect(put).toHaveBeenCalledWith(
    '/api/option/',
    expect.objectContaining({ key: 'ImagePricingPolicy' })
  )
  expect(storedPolicy).toEqual({
    ...initialPolicy,
    version: expect.any(String),
    models: {
      ...initialPolicy.models,
      'gpt-image-2.5-flare': {
        ...initialPolicy.models['gpt-image-2.5-flare'],
        allow_custom_size: true,
      },
    },
  })
  expect(screen.getByRole('combobox', { name: 'Model' })).toHaveValue(
    'gpt-image-2.5-flare'
  )
  expect(
    screen.getByRole('switch', { name: 'imageStudio.admin.customSize' })
  ).toBeChecked()

  fireEvent.change(
    screen.getByRole('spinbutton', {
      name: 'imageStudio.admin.pricing.price1k',
    }),
    { target: { value: '9' } }
  )
  await user.selectOptions(
    screen.getByRole('combobox', { name: 'Model' }),
    'gpt-image-2.5-sunburst'
  )
  await user.click(screen.getByRole('button', { name: 'Cancel' }))
  expect(screen.getByRole('combobox', { name: 'Model' })).toHaveValue(
    'gpt-image-2.5-flare'
  )
  expect(
    screen.getByRole('spinbutton', {
      name: 'imageStudio.admin.pricing.price1k',
    })
  ).toHaveValue(9)

  await user.selectOptions(
    screen.getByRole('combobox', { name: 'Model' }),
    'gpt-image-2.5-sunburst'
  )
  await user.click(screen.getByRole('button', { name: 'Discard changes' }))
  expect(
    screen.getByRole('spinbutton', {
      name: 'imageStudio.admin.pricing.price1k',
    })
  ).toHaveValue(0.15)
  expect(
    screen.getByRole('spinbutton', {
      name: 'imageStudio.admin.pricing.price2k',
    })
  ).toHaveValue(0.3)
  expect(
    screen.getByRole('spinbutton', {
      name: 'imageStudio.admin.pricing.price4k',
    })
  ).toHaveValue(0.6)
  expect(
    screen.getByRole('switch', { name: 'imageStudio.admin.customSize' })
  ).not.toBeChecked()
  await user.click(
    screen.getByRole('switch', { name: 'imageStudio.admin.customSize' })
  )
  await user.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(put).toHaveBeenCalledTimes(2))
  expect(storedPolicy.models['gpt-image-2.5-sunburst']).toEqual({
    ...initialPolicy.models['gpt-image-2.5-sunburst'],
    allow_custom_size: true,
  })
  expect(storedPolicy.models['gpt-image-2.5-flare']).toEqual({
    ...initialPolicy.models['gpt-image-2.5-flare'],
    allow_custom_size: true,
  })

  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
  )
  await user.selectOptions(
    screen.getByRole('combobox', { name: 'Model' }),
    'other-tier-model'
  )
  expect(screen.getByRole('alert')).toHaveTextContent(
    'imageStudio.admin.pricing.invalidPolicy'
  )
  expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument()
  await user.selectOptions(
    screen.getByRole('combobox', { name: 'Model' }),
    'gpt-image-2'
  )
  expect(
    screen.getByRole('spinbutton', {
      name: 'imageStudio.admin.pricing.price1k',
    })
  ).toHaveValue(0.67)
  client.clear()
})
