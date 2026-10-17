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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import {
  getOrCreateImageStudioSubmissionKey,
  listPendingImageStudioSubmissions,
  markImageStudioSubmissionStarted,
} from '@/stores/image-studio-draft-store'

import { lookupImageSubmission } from '../api'
import type { ImageGeneration } from '../types'
import { ImageSubmissionRecovery } from './image-submission-recovery'

vi.mock('../api', () => ({ lookupImageSubmission: vi.fn() }))

const fingerprint = 'a'.repeat(64)
const userId = 27

function renderRecovery() {
  const idempotencyKey = getOrCreateImageStudioSubmissionKey(
    userId,
    fingerprint
  )
  expect(idempotencyKey).toBeTruthy()
  expect(markImageStudioSubmissionStarted(userId, fingerprint)).toBe(true)
  const onRecovered = vi.fn()
  render(
    <QueryClientProvider client={new QueryClient()}>
      <ImageSubmissionRecovery userId={userId} onRecovered={onRecovered} />
    </QueryClientProvider>
  )
  return { idempotencyKey, onRecovered }
}

describe('submission recovery after leaving the page', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.mocked(lookupImageSubmission).mockReset()
  })

  test.each(['not_found', 'pending'] as const)(
    'retains an uncertain %s receipt without requiring prompt or reference files',
    async (state) => {
      vi.mocked(lookupImageSubmission).mockResolvedValue({ state })
      const { idempotencyKey, onRecovered } = renderRecovery()
      await userEvent.click(screen.getByRole('button'))
      await waitFor(() => expect(screen.getByRole('button')).toBeEnabled())
      expect(lookupImageSubmission).toHaveBeenCalledExactlyOnceWith(
        idempotencyKey
      )
      expect(listPendingImageStudioSubmissions(userId)).toHaveLength(1)
      expect(onRecovered).not.toHaveBeenCalled()
    }
  )

  test('keeps lookup available after a found task remains unknown', async () => {
    const generation = { id: 9, status: 'unknown' } as ImageGeneration
    vi.mocked(lookupImageSubmission).mockResolvedValue({
      state: 'found',
      generation,
    })
    const { onRecovered } = renderRecovery()
    await userEvent.click(screen.getByRole('button'))
    await waitFor(() => expect(onRecovered).toHaveBeenCalledWith(generation))
    expect(listPendingImageStudioSubmissions(userId)).toHaveLength(1)
    expect(screen.getByRole('button')).toBeEnabled()
    await userEvent.click(screen.getByRole('button'))
    await waitFor(() => expect(lookupImageSubmission).toHaveBeenCalledTimes(2))
  })

  test('preserves the receipt when lookup fails and only clears it after confirmed acceptance', async () => {
    const generation = { id: 9, status: 'submitting' } as ImageGeneration
    vi.mocked(lookupImageSubmission)
      .mockRejectedValueOnce(new Error('network failed'))
      .mockResolvedValueOnce({ state: 'found', generation })
    const { onRecovered } = renderRecovery()
    await userEvent.click(screen.getByRole('button'))
    await waitFor(() => expect(screen.getByRole('button')).toBeEnabled())
    expect(listPendingImageStudioSubmissions(userId)).toHaveLength(1)
    expect(onRecovered).not.toHaveBeenCalled()
    await userEvent.click(screen.getByRole('button'))
    await waitFor(() => expect(onRecovered).toHaveBeenCalledWith(generation))
    expect(listPendingImageStudioSubmissions(userId)).toHaveLength(0)
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })
})
