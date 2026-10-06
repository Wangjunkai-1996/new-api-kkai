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
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { updateUserSettings } from '../api'
import { NotificationTab } from '../components/tabs/notification-tab'
import type { UserProfile } from '../types'

const profile: UserProfile = {
  id: 1,
  username: 'alice',
  display_name: 'Alice',
  role: 1,
  group: 'default',
  quota: 1000000,
  used_quota: 0,
  request_count: 0,
  status: 1,
  aff_count: 0,
  aff_quota: 0,
  aff_history_quota: 0,
  created_time: 0,
}
const settings = {
  notify_type: 'webhook',
  quota_warning_threshold: 1200,
  notification_email: '',
  webhook_url: 'https://example.com/notify',
  webhook_secret: 'webhook-secret',
  bark_url: '',
  gotify_url: '',
  gotify_token: '',
  gotify_priority: 5,
  accept_unset_model_ratio_model: true,
  record_ip_log: true,
  upstream_model_update_notify_enabled: true,
}

afterEach(() => vi.restoreAllMocks())

describe('user settings saves across profile and security', () => {
  it('updates IP recording without resubmitting unrelated notification settings', async () => {
    const get = vi.spyOn(api, 'get')
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    await updateUserSettings({ record_ip_log: false })
    expect(get).not.toHaveBeenCalled()
    expect(put).toHaveBeenCalledWith('/api/user/setting', {
      record_ip_log: false,
    })
  })

  it('preserves explicit false and zero values in partial settings updates', async () => {
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    await updateUserSettings({
      record_ip_log: false,
      quota_warning_threshold: 0,
    })
    expect(put).toHaveBeenCalledWith('/api/user/setting', {
      record_ip_log: false,
      quota_warning_threshold: 0,
    })
  })

  it('returns transport failures to the caller for retry', async () => {
    vi.spyOn(api, 'put').mockRejectedValue(new Error('offline'))
    await expect(updateUserSettings({ record_ip_log: false })).rejects.toThrow(
      'offline'
    )
  })

  it('an unsuccessful settings write is returned to the caller', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: profile },
    })
    vi.spyOn(api, 'put').mockResolvedValue({
      data: { success: false, message: 'Save failed' },
    })
    expect(await updateUserSettings({ record_ip_log: true })).toEqual({
      success: false,
      message: 'Save failed',
    })
  })

  it('saving a stale notification form keeps the current IP setting and the notification edits', async () => {
    const onUpdate = vi.fn()
    vi.spyOn(api, 'get').mockResolvedValue({
      data: {
        success: true,
        data: {
          ...profile,
          setting: JSON.stringify({ ...settings, record_ip_log: false }),
        },
      },
    })
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    render(
      <NotificationTab
        profile={{ ...profile, setting: JSON.stringify(settings) }}
        onUpdate={onUpdate}
      />
    )
    expect(
      screen.queryByRole('switch', { name: 'Record IP Address' })
    ).not.toBeInTheDocument()
    fireEvent.change(
      screen.getByRole('spinbutton', { name: 'Quota Warning Threshold' }),
      { target: { value: '2700' } }
    )
    fireEvent.click(screen.getByRole('button', { name: 'Save Settings' }))
    await waitFor(() => expect(onUpdate).toHaveBeenCalled())
    expect(put).toHaveBeenCalledWith(
      '/api/user/setting',
      expect.objectContaining({
        notify_type: settings.notify_type,
        webhook_url: settings.webhook_url,
        quota_warning_threshold: 2700,
      })
    )
    expect(put.mock.calls[0]?.[1]).not.toHaveProperty('record_ip_log')
  })
})
