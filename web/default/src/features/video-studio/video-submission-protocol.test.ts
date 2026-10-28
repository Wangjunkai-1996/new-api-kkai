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
import { webcrypto } from 'node:crypto'

import { AxiosError } from 'axios'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import { videoComposerSchema, videoQuoteSchema } from './schemas'
import type { VideoQuoteRequest } from './types'
import {
  getUnknownVideoSubmissionTaskId,
  getVideoSubmissionFingerprint,
  getVideoSubmissionIdentity,
  isCurrentVideoSubmissionIdentity,
  isVideoSubmissionRejected,
  parseVideoSubmissionReceipt,
} from './video-submission-protocol'

const request: VideoQuoteRequest = {
  token_id: 7,
  model: 'video',
  mode: 'first_last_frame',
  prompt: 'Rain at dusk',
  parameters: { duration: 5, audio: false, seed: 0 },
  reference_assets: [
    { role: 'first_frame', asset_id: 1 },
    { role: 'last_frame', asset_id: 2 },
  ],
}
beforeEach(() => {
  vi.stubGlobal('crypto', webcrypto)
  useAuthStore.getState().auth.setUser({ id: 1, username: 'one', role: 1 })
})
afterEach(() => {
  useAuthStore.getState().auth.reset()
  vi.unstubAllGlobals()
})

test('fingerprints canonical input while retaining role, type, token, owner and group distinctions', async () => {
  const identity = getVideoSubmissionIdentity()
  const original = await getVideoSubmissionFingerprint(
    identity,
    'video',
    request
  )
  expect(
    await getVideoSubmissionFingerprint(identity, 'video', {
      ...request,
      prompt: '\u0085Rain at dusk\u0085',
      parameters: { seed: 0, audio: false, duration: 5 },
      reference_assets: [...request.reference_assets].reverse(),
    })
  ).toBe(original)
  for (const different of [
    { ...request, parameters: { duration: 5 } },
    { ...request, parameters: { duration: 5, audio: 'false', seed: 0 } },
    { ...request, token_id: 8 },
    { ...request, prompt: 'Rain  at dusk' },
    { ...request, prompt: '\ufeffRain at dusk' },
    {
      ...request,
      reference_assets: [
        { role: 'first_frame' as const, asset_id: 2 },
        { role: 'last_frame' as const, asset_id: 1 },
      ],
    },
  ]) {
    expect(
      await getVideoSubmissionFingerprint(identity, 'video', different)
    ).not.toBe(original)
  }
  expect(
    await getVideoSubmissionFingerprint(identity, 'other', request)
  ).not.toBe(original)
  expect(
    await getVideoSubmissionFingerprint(
      { ...identity, userId: 2 },
      'video',
      request
    )
  ).not.toBe(original)
  await expect(
    getVideoSubmissionFingerprint(identity, 'video', {
      ...request,
      reference_assets: [
        request.reference_assets[0],
        request.reference_assets[0],
      ],
    })
  ).rejects.toThrow('Duplicate')
})
test('leaving and returning never makes an old identity current again', () => {
  const identity = getVideoSubmissionIdentity()
  useAuthStore.getState().auth.reset()
  useAuthStore.getState().auth.setUser({ id: 1, username: 'one', role: 1 })
  expect(isCurrentVideoSubmissionIdentity(identity)).toBe(false)
})
test('quote and prompt boundaries reject unsafe amounts and UTF-8 overflow', () => {
  const quote = { quota: 0, request_hash: 'a'.repeat(64), expires_at: 1 }
  expect(videoQuoteSchema.safeParse(quote).success).toBe(true)
  for (const patch of [
    { quota: -1 },
    { quota: Number.MAX_SAFE_INTEGER + 1 },
    { quota: Infinity },
    { request_hash: 'short' },
    { expires_at: 0 },
    { display_amount: 1 },
  ]) {
    expect(videoQuoteSchema.safeParse({ ...quote, ...patch }).success).toBe(
      false
    )
  }
  const values = {
    model_profile_id: 1,
    mode: 'text_to_video',
    reference_asset_ids: [],
    parameters: {},
  }
  expect(
    videoComposerSchema.safeParse({ ...values, prompt: '汉'.repeat(2666) })
      .success
  ).toBe(true)
  expect(
    videoComposerSchema.safeParse({ ...values, prompt: '汉'.repeat(2667) })
      .success
  ).toBe(false)
})
test('receipt validation preserves uncertainty, including malformed and conflicting task identities', () => {
  expect(
    parseVideoSubmissionReceipt({ id: ' task_abc ', status: 'queued' })
  ).toEqual({ id: 'task_abc', status: 'queued' })
  for (const value of [
    {},
    { id: 12 },
    { task_id: 'other' },
    { task_id: `task_${'x'.repeat(124)}` },
    { id: 'task_a', task_id: 'task_b' },
    { id: 'task_a', status: 'unknown' },
    { id: 'task_a', error: {} },
    { id: 'task_a', status: 'processing' },
  ]) {
    expect(() => parseVideoSubmissionReceipt(value)).toThrow(
      'could not be confirmed'
    )
  }
  try {
    parseVideoSubmissionReceipt({ id: 'task_a', status: 'unknown' })
  } catch (error) {
    expect(getUnknownVideoSubmissionTaskId(error)).toBe('task_a')
  }
})
test.each([
  [409, 'quote_stale', true],
  [400, 'video_quote_required', true],
  [403, 'video_token_invalid', true],
  [500, 'quote_stale', false],
  [409, 'video_studio_conflict', false],
  [400, 'unknown', false],
  [403, 'task_submission_unknown', false],
])(
  'only audited status/code evidence ends pending: %s %s',
  (status, code, expected) => {
    const error = new AxiosError(
      'request failed',
      undefined,
      undefined,
      undefined,
      {
        status: Number(status),
        statusText: '',
        headers: {},
        config: {} as never,
        data: { code, data: 'task_example' },
      }
    )
    expect(isVideoSubmissionRejected(error)).toBe(expected)
  }
)
