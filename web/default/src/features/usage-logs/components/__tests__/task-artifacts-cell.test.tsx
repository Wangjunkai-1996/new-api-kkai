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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import type { TaskLog } from '../../types'
import { TaskArtifactsCell } from '../task-artifacts'

function taskLog(overrides: Partial<TaskLog> = {}): TaskLog {
  return {
    id: 1,
    user_id: 7,
    platform: 'alibaba',
    task_id: 'task-public',
    action: 'text_to_image',
    channel_id: 357,
    group: 'default',
    quota: 25000,
    submit_time: 1,
    status: 'SUCCESS',
    admin_info: {
      task_plugin: {
        key: 'alibaba',
        name: 'Alibaba Bailian',
        version: '1.4.0',
      },
    },
    ...overrides,
  }
}

function renderCell(log: TaskLog) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <TaskArtifactsCell log={log} />
    </QueryClientProvider>
  )
}

describe('task artifacts cell', () => {
  test('given a synchronous result the gateway did not retain, a hint replaces the artifacts button', () => {
    renderCell(taskLog({ result_discarded: true }))

    expect(screen.getByText('Result not retained')).toBeVisible()
    expect(
      screen.queryByRole('button', { name: /Artifacts/ })
    ).not.toBeInTheDocument()
  })

  test('given a retained successful plugin task, the artifacts button is offered', () => {
    renderCell(taskLog())

    expect(screen.getByRole('button', { name: /Artifacts/ })).toBeVisible()
    expect(screen.queryByText('Result not retained')).not.toBeInTheDocument()
  })

  test('given an unfinished task, neither the button nor the hint is shown', () => {
    renderCell(taskLog({ status: 'IN_PROGRESS', result_discarded: true }))

    expect(screen.getByText('-')).toBeVisible()
    expect(screen.queryByText('Result not retained')).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: /Artifacts/ })
    ).not.toBeInTheDocument()
  })
  test('fetches retained files only when opened and renders the signed download URL', async () => {
    const contentUrl = `https://media.example/v1/tasks/task-public/artifacts/result/content?access=${'a'.repeat(43)}`
    const get = vi.spyOn(api, 'get').mockResolvedValue({
      data: {
        success: true,
        data: {
          artifacts: [
            { key: 'result', type: 'image', content_url: contentUrl },
          ],
        },
      },
    })
    renderCell(taskLog())
    expect(get).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: 'Artifacts' }))

    expect(await screen.findByRole('img', { name: 'result' })).toHaveAttribute(
      'src',
      contentUrl
    )
    expect(screen.getByRole('button', { name: 'Download' })).toHaveAttribute(
      'href',
      contentUrl
    )
    expect(get).toHaveBeenCalledExactlyOnceWith(
      '/api/task/task-public/artifacts',
      {
        skipBusinessError: true,
        skipErrorHandler: true,
      }
    )
  })

  test('shows a retriable error when files cannot be loaded', async () => {
    const get = vi
      .spyOn(api, 'get')
      .mockRejectedValueOnce(new Error('transport failed'))
      .mockResolvedValueOnce({
        data: { success: true, data: { artifacts: [] } },
      })
    renderCell(taskLog())
    await userEvent.click(screen.getByRole('button', { name: 'Artifacts' }))

    expect(await screen.findByText('Failed to load artifacts')).toBeVisible()
    await userEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByText('None')).toBeVisible()
    expect(get).toHaveBeenCalledTimes(2)
  })
})
