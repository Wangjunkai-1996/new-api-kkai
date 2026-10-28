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
import axios from 'axios'
import { z } from 'zod'

import { api } from '@/lib/api'
import { getCurrentHost } from '@/lib/host-scope'
import { useAuthStore } from '@/stores/auth-store'

export class ShopError extends Error {
  constructor(
    readonly code: string,
    readonly status = 0,
    readonly fields: Record<string, string> = {},
    readonly requestOutcome?: 'not_committed'
  ) {
    super(code)
    this.name = 'ShopError'
  }
}

export function isShopDefinitiveRejection(failure: unknown) {
  return (
    failure instanceof ShopError &&
    failure.status >= 400 &&
    failure.status < 500 &&
    ![408, 429].includes(failure.status) &&
    ![
      'SHOP_IDEMPOTENCY_CONFLICT',
      'SHOP_REQUEST_UNRESOLVED',
      'SHOP_PAYMENT_UNRESOLVED',
    ].includes(failure.code)
  )
}

export function shopIdentity() {
  const auth = useAuthStore.getState().auth
  return `${getCurrentHost()}:${auth.user?.id ?? 'guest'}:${auth.session?.sid ?? 'none'}`
}

export async function shopRequest<T>(
  path: string,
  schema: z.ZodType<T>,
  options: {
    method?: 'GET' | 'POST' | 'PUT' | 'PATCH'
    body?: unknown
    key?: string
    proof?: string
    signal?: AbortSignal
  } = {}
): Promise<T> {
  const identity = shopIdentity()
  try {
    const response = await api.request({
      url: path,
      method: options.method ?? 'GET',
      data: options.body,
      signal: options.signal,
      timeout: 20_000,
      disableDuplicate: true,
      skipErrorHandler: true,
      singleUseAuthorization: Boolean(options.proof),
      headers: {
        ...(options.key ? { 'Idempotency-Key': options.key } : {}),
        ...(options.proof ? { 'X-Security-Proof': options.proof } : {}),
      },
    })
    if (identity !== shopIdentity() || options.signal?.aborted) {
      throw new ShopError('SHOP_REQUEST_CANCELLED')
    }
    const envelope = z
      .object({
        success: z.boolean(),
        code: z.string().optional(),
        data: z.unknown(),
      })
      .safeParse(response.data)
    if (!envelope.success) throw new ShopError('SHOP_PROTOCOL_ERROR')
    if (!envelope.data.success) {
      throw new ShopError(
        envelope.data.code ?? 'SHOP_INTERNAL_ERROR',
        response.status
      )
    }
    const parsed = schema.safeParse(envelope.data.data)
    if (!parsed.success) throw new ShopError('SHOP_PROTOCOL_ERROR')
    return parsed.data
  } catch (error) {
    if (error instanceof ShopError) throw error
    if (axios.isCancel(error)) throw new ShopError('SHOP_REQUEST_CANCELLED')
    if (axios.isAxiosError(error)) {
      const body = z
        .object({
          code: z.string().optional(),
          data: z
            .object({
              field_errors: z.record(z.string(), z.string()).optional(),
              request_outcome: z.literal('not_committed').optional(),
            })
            .optional(),
        })
        .safeParse(error.response?.data)
      // Never retain Axios config/response, raw payload, proof, or validation values.
      throw new ShopError(
        body.success
          ? (body.data.code ?? 'SHOP_INTERNAL_ERROR')
          : 'SHOP_INTERNAL_ERROR',
        error.response?.status ?? 0,
        {},
        body.success ? body.data.data?.request_outcome : undefined
      )
    }
    throw new ShopError('SHOP_INTERNAL_ERROR')
  }
}

export function queryString(
  values: Record<string, string | number | undefined>
) {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(values)) {
    if (value !== undefined && value !== '') params.set(key, String(value))
  }
  return params.toString() ? `?${params}` : ''
}

export function shopPath(id: string) {
  return encodeURIComponent(id)
}
