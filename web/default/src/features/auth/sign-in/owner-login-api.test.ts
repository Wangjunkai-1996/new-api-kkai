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

import { login } from '../api'
import { encryptPassword } from '../lib/password-encryption'

vi.mock('../lib/password-encryption', () => ({
  encryptPassword: vi.fn(),
  clearPasswordEncryptionCache: vi.fn(),
}))

afterEach(() => vi.restoreAllMocks())

describe('explicit reseller owner password login', () => {
  it.each([undefined, false])(
    'keeps default customer login when owner_login=%s',
    async (ownerLogin) => {
      const post = vi
        .spyOn(api, 'post')
        .mockResolvedValue({ data: { success: true } })
      await login({
        username: 'same-name',
        password: 'customer-password',
        owner_login: ownerLogin,
      })
      expect(post).toHaveBeenCalledExactlyOnceWith(
        '/api/user/login?turnstile=',
        { username: 'same-name', password: 'customer-password' },
        { skipAuthRefresh: true }
      )
    }
  )

  it.each([false, true])(
    'preserves the owner namespace with encryption=%s',
    async (encrypted) => {
      const post = vi
        .spyOn(api, 'post')
        .mockResolvedValue({ data: { success: true } })
      vi.mocked(encryptPassword).mockResolvedValue({
        password_encrypted: 'ciphertext',
        encryption_key_id: 'key-1',
      })
      await login({
        username: 'same-name',
        password: 'owner-password',
        owner_login: true,
        passwordEncryptionEnabled: encrypted,
        turnstile: 'verified',
      })
      expect(post).toHaveBeenCalledExactlyOnceWith(
        '/api/user/login?turnstile=verified',
        {
          username: 'same-name',
          owner_login: true,
          ...(encrypted
            ? { password_encrypted: 'ciphertext', encryption_key_id: 'key-1' }
            : { password: 'owner-password' }),
        },
        { skipAuthRefresh: true }
      )
    }
  )
})
