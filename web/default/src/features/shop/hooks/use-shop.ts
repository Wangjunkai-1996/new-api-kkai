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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useRef, useState } from 'react'
import type { z } from 'zod'

import { getCurrentHost } from '@/lib/host-scope'
import { useAuthStore } from '@/stores/auth-store'

import {
  isShopDefinitiveRejection,
  ShopError,
  shopIdentity,
  shopRequest,
} from '../api'
import { shopDigest } from '../lib'

export function useShopIdentity() {
  const userId = useAuthStore((state) => state.auth.user?.id)
  const sid = useAuthStore((state) => state.auth.session?.sid)
  return `${getCurrentHost()}:${userId ?? 'guest'}:${sid ?? 'none'}`
}

export function useShopQuery<T>(
  path: string,
  schema: z.ZodType<T>,
  options: {
    public?: boolean
    enabled?: boolean
    poll?: number | ((data: T | undefined) => number | false)
  } = {}
) {
  const identity = useShopIdentity()
  const userId = useAuthStore((state) => state.auth.user?.id)
  return useQuery({
    queryKey: options.public
      ? ['shop-public', getCurrentHost(), path]
      : [
          'private-user',
          userId,
          path.startsWith('/api/admin/') ? 'shop-admin' : 'shop',
          identity,
          path,
        ],
    queryFn: ({ signal }) => shopRequest(path, schema, { signal }),
    enabled: options.enabled !== false,
    staleTime: 0,
    retry: false,
    refetchInterval: (query) =>
      typeof options.poll === 'function'
        ? options.poll(query.state.data)
        : options.poll || false,
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: true,
    meta: { errorToast: false },
  })
}

/** Controlled requests keep private input, response and errors out of MutationCache. */
export function useShopAction() {
  const queryClient = useQueryClient()
  const identity = useShopIdentity()
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<ShopError | null>(null)
  const active = useRef<AbortController | null>(null)
  const intent = useRef<{
    key: string
    signature: string
    uncertain: boolean
    attempted: boolean
  } | null>(null)
  useEffect(() => {
    return () => {
      setPending(false)
      setError(null)
      active.current?.abort()
      active.current = null
      intent.current = null
    }
  }, [identity])
  const prepare = async (
    path: string,
    body: unknown,
    method = 'POST',
    key?: string
  ) => {
    const signature = await shopDigest(JSON.stringify([path, method, body]))
    if (identity !== shopIdentity()) {
      throw new ShopError('SHOP_REQUEST_CANCELLED')
    }
    if (
      intent.current &&
      (intent.current.signature !== signature ||
        (key && key !== intent.current.key))
    ) {
      if (intent.current.attempted) {
        throw new ShopError('SHOP_REQUEST_UNRESOLVED')
      }
      intent.current = null
    }
    intent.current ??= {
      key: key ?? crypto.randomUUID(),
      signature,
      uncertain: false,
      attempted: false,
    }
    return intent.current.key
  }
  const run = async <T>(
    path: string,
    schema: z.ZodType<T>,
    body: unknown,
    options: {
      method?: 'POST' | 'PUT' | 'PATCH'
      key?: string
      proof?: string
      read?: boolean
      onDefinitiveRejection?: () => void
    } = {}
  ) => {
    if (active.current) return undefined
    const controller = new AbortController()
    active.current = controller
    setPending(true)
    setError(null)
    try {
      const method = options.method ?? 'POST'
      const key = options.read
        ? undefined
        : await prepare(path, body, method, options.key)
      if (!options.read && intent.current) intent.current.attempted = true
      const result = await shopRequest(path, schema, {
        method: options.read ? 'GET' : method,
        body,
        key,
        proof: options.proof,
        signal: controller.signal,
      })
      if (controller.signal.aborted || identity !== shopIdentity()) {
        return undefined
      }
      if (!options.read) intent.current = null
      await queryClient.invalidateQueries({
        predicate: (query) =>
          query.queryKey.includes('shop') ||
          query.queryKey.includes('shop-admin') ||
          query.queryKey[0] === 'shop-public',
      })
      return result
    } catch (failure) {
      if (!options.read && intent.current && identity === shopIdentity()) {
        const definitive = isShopDefinitiveRejection(failure)
        if (definitive && !intent.current.uncertain) {
          intent.current = null
          if (!controller.signal.aborted) options.onDefinitiveRejection?.()
        } else intent.current.uncertain = true
      }
      if (!controller.signal.aborted && identity === shopIdentity()) {
        setError(
          failure instanceof ShopError
            ? failure
            : new ShopError('SHOP_INTERNAL_ERROR')
        )
      }
      return undefined
    } finally {
      if (active.current === controller) {
        active.current = null
        setPending(false)
      }
    }
  }
  return { run, prepare, pending, error, clearError: () => setError(null) }
}

/** A response is only renderable in the identity/resource generation that requested it. */
export function usePrivateResult<T>(resource: string) {
  const identity = useShopIdentity()
  const binding = `${identity}:${resource}`
  const currentBinding = useRef(binding)
  const generation = useRef(0)
  const controller = useRef<AbortController | null>(null)
  const [result, setResult] = useState<{ binding: string; value: T } | null>(
    null
  )
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<ShopError | null>(null)
  const clear = useCallback(() => {
    generation.current += 1
    controller.current?.abort()
    controller.current = null
    setResult(null)
    setPending(false)
  }, [])
  useEffect(() => {
    currentBinding.current = binding
    const hide = () => {
      if (document.hidden) clear()
    }
    document.addEventListener('visibilitychange', hide)
    window.addEventListener('pagehide', clear)
    window.addEventListener('pageshow', clear)
    return () => {
      clear()
      document.removeEventListener('visibilitychange', hide)
      window.removeEventListener('pagehide', clear)
      window.removeEventListener('pageshow', clear)
    }
  }, [binding, clear])
  const read = async (request: (signal: AbortSignal) => Promise<T>) => {
    if (
      currentBinding.current !== binding ||
      identity !== shopIdentity() ||
      document.hidden
    ) {
      return undefined
    }
    clear()
    const mine = generation.current
    const signalController = new AbortController()
    controller.current = signalController
    setPending(true)
    setError(null)
    try {
      const value = await request(signalController.signal)
      if (
        signalController.signal.aborted ||
        generation.current !== mine ||
        currentBinding.current !== binding ||
        identity !== shopIdentity() ||
        document.hidden
      ) {
        return undefined
      }
      setResult({ binding, value })
      return value
    } catch (failure) {
      if (!signalController.signal.aborted && generation.current === mine) {
        setError(
          failure instanceof ShopError
            ? failure
            : new ShopError('SHOP_INTERNAL_ERROR')
        )
      }
      return undefined
    } finally {
      if (generation.current === mine) setPending(false)
    }
  }
  return {
    value: result?.binding === binding ? result.value : null,
    read,
    clear,
    pending,
    error,
  }
}
