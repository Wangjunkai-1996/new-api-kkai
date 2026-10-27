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
import { z } from 'zod'

import { api, type ApiRequestConfig } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import { buildQueryParams } from './lib/query-params'
import {
  parseTaskArtifactsResponse,
  parseTaskMediaSummary,
  TaskArtifactApiError,
} from './lib/task-artifacts'
import type {
  GetLogsParams,
  GetLogsResponse,
  GetLogStatsParams,
  GetLogStatsResponse,
  GetMidjourneyLogsParams,
  GetTaskLogsParams,
  TaskArtifactsResponse,
  UserInfo,
  VideoWorksPage,
} from './types'

// ============================================================================
// Generic API Helpers
// ============================================================================

function buildApiPath(endpoint: string, isAdmin: boolean): string {
  return isAdmin ? endpoint : `${endpoint}/self`
}

async function fetchLogs<T>(
  endpoint: string,
  params: T,
  isAdmin: boolean
): Promise<GetLogsResponse> {
  const paramRecord = params as unknown as Record<string, unknown>
  const queryParams = buildQueryParams({
    p: paramRecord.p || 1,
    page_size: paramRecord.page_size || 20,
    ...params,
  })
  const path = buildApiPath(endpoint, isAdmin)
  const res = await api.get(`${path}?${queryParams}`)
  return res.data
}

async function fetchLogStats<T>(
  endpoint: string,
  params: T,
  isAdmin: boolean
): Promise<GetLogStatsResponse> {
  const queryParams = buildQueryParams(
    params as unknown as Record<string, unknown>
  )
  const path = buildApiPath(endpoint, isAdmin)
  const res = await api.get(`${path}/stat?${queryParams}`)
  return res.data
}

// ============================================================================
// Common Log APIs
// ============================================================================

export const getAllLogs = (params: GetLogsParams = {}) =>
  fetchLogs('/api/log', params, true)

export const getUserLogs = (
  params: Omit<GetLogsParams, 'username' | 'channel'> = {}
) => fetchLogs('/api/log', params, false)

export const getLogStats = (params: GetLogStatsParams = {}) =>
  fetchLogStats('/api/log', params, true)

export const getUserLogStats = (
  params: Omit<GetLogStatsParams, 'username' | 'channel'> = {}
) => fetchLogStats('/api/log', params, false)

export async function getUserInfo(
  userId: number
): Promise<{ success: boolean; message?: string; data?: UserInfo }> {
  const res = await api.get(`/api/user/${userId}`)
  return res.data
}

// ============================================================================
// MjProxy (Drawing) Logs API
// ============================================================================

export const getAllMidjourneyLogs = (params: GetMidjourneyLogsParams) =>
  fetchLogs('/api/mj', params, true)

export const getUserMidjourneyLogs = (params: GetMidjourneyLogsParams) =>
  fetchLogs('/api/mj', params, false)

// ============================================================================
// Task Logs API
// ============================================================================

export const getAllTaskLogs = (params: GetTaskLogsParams) =>
  fetchLogs('/api/task/', params, true)

export const getUserTaskLogs = (params: GetTaskLogsParams) =>
  fetchLogs('/api/task', params, false)

const taskArtifactRequestConfig = {
  skipBusinessError: true,
  skipErrorHandler: true,
} satisfies ApiRequestConfig

export async function getTaskArtifacts(
  taskId: string,
  options: {
    signal?: AbortSignal
    recordId?: number
    requireHosted?: boolean
  } = {}
) {
  const response = await api.get<TaskArtifactsResponse>(
    `/api/task/${encodeURIComponent(taskId)}/artifacts`,
    {
      ...taskArtifactRequestConfig,
      ...(options.signal ? { signal: options.signal } : {}),
    }
  )
  const projection = parseTaskArtifactsResponse(response.data, options.recordId)
  if (options.requireHosted && !projection.hostedVideo) {
    throw new TaskArtifactApiError('missing_hosted_video')
  }
  return projection
}

const videoWorkSchema = z
  .object({
    id: z.number().int().positive(),
    user_id: z.number().int().nonnegative(),
    task_id: z.string().min(1),
    platform: z.string(),
    action: z.string(),
    channel_id: z.number().int().nonnegative(),
    group: z.string(),
    quota: z.number().finite(),
    submit_time: z.number().finite(),
    status: z.string(),
    billing: z.object({ amount_available: z.boolean() }).passthrough(),
    media: z
      .unknown()
      .optional()
      .transform((value) =>
        value == null ? undefined : parseTaskMediaSummary(value)
      ),
  })
  .passthrough()
  .refine((task) => !task.billing.amount_available || task.quota >= 0, {
    message: 'Invalid available task amount',
    path: ['quota'],
  })

const videoWorksPageSchema = z.object({
  items: z
    .array(videoWorkSchema)
    .nullable()
    .transform((items) => items ?? []),
  total: z.number().int().nonnegative(),
  page: z.number().int().positive(),
  page_size: z.number().int().positive().max(100),
  status_counts: z.object({
    all: z.number().int().nonnegative(),
    success: z.number().int().nonnegative(),
    processing: z.number().int().nonnegative(),
    failed: z.number().int().nonnegative(),
    unknown: z.number().int().nonnegative(),
  }),
  media_warning: z.literal('media_temporarily_unavailable').optional(),
})

export async function getVideoWorks(
  params: GetTaskLogsParams,
  isAdmin: boolean,
  signal?: AbortSignal
): Promise<VideoWorksPage> {
  const query = buildQueryParams({ ...params, media_type: 'video' })
  const response = await api.get(
    `${isAdmin ? '/api/task/' : '/api/task/self'}?${query}`,
    { ...taskArtifactRequestConfig, signal }
  )
  return videoWorksPageSchema.parse(requireServerSuccess(response.data).data)
}
