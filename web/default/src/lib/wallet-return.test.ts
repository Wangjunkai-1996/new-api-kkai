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
import {
  createMemoryHistory,
  createRootRoute,
  createRootRouteWithContext,
  createRoute,
  createRouter,
  defaultStringifySearch,
  redirect,
  type RouterHistory,
} from '@tanstack/react-router'
import { describe, expect, test, vi } from 'vitest'

import { createAppQueryClient } from './query-client'
import { getWalletReturnRedirect, walletSearchSchema } from './wallet-return'

describe('wallet payment return normalization', () => {
  test('uses the exact merchant order from a gateway return, never its payment ID', () => {
    const gatewayTradeNo = '9'.repeat(180)
    const redirect = getWalletReturnRedirect(
      '/wallet',
      `?pid=2026101814360916&trade_no=${gatewayTradeNo}&out_trade_no=2026102523001457571419100748&type=alipay&name=Wallet&sign=abc&sign_type=MD5`
    )

    expect(redirect).toEqual({
      to: '/wallet',
      search: { trade_no: '2026102523001457571419100748' },
      replace: true,
    })
  })

  test('preserves a normal internal trade number including numeric strings', () => {
    expect(
      getWalletReturnRedirect('/wallet', '?trade_no=WT-abc_123&noise=ignored')
    ).toEqual({
      to: '/wallet',
      search: { trade_no: 'WT-abc_123' },
      replace: true,
    })
    expect(getWalletReturnRedirect('/wallet', '?trade_no=123456789')).toEqual({
      to: '/wallet',
      search: { trade_no: '123456789' },
      replace: true,
    })
  })

  test.each([
    ['missing merchant order', '?pid=1&trade_no=987654321&type=alipay'],
    [
      'duplicate merchant orders',
      '?pid=1&trade_no=987654321&out_trade_no=WT-one&out_trade_no=WT-two&type=alipay',
    ],
    ['invalid merchant order', '?out_trade_no=bad%2Forder&pid=1&type=alipay'],
    ['duplicate internal orders', '?trade_no=WT-one&trade_no=WT-two'],
    ['invalid internal order', '?trade_no=bad%2Forder'],
  ])(
    'marks %s for manual recovery without selecting an order',
    (_name, search) => {
      expect(getWalletReturnRedirect('/wallet', search)).toEqual({
        to: '/wallet',
        search: { invalid_payment_return: true },
        replace: true,
      })
    }
  )

  test('redirects legacy console returns to a cleaned wallet URL and shows history', () => {
    expect(
      getWalletReturnRedirect(
        '/console/topup',
        '?pid=1&trade_no=untrusted&out_trade_no=WT-merchant-9&type=alipay'
      )
    ).toEqual({
      to: '/wallet',
      search: { trade_no: 'WT-merchant-9', show_history: true },
      replace: true,
    })
  })

  test('replaces gateway parameters once, then treats the canonical URL as settled', () => {
    const redirect = getWalletReturnRedirect(
      '/wallet',
      '?pid=1&trade_no=provider-1&out_trade_no=WT-merchant-9&type=alipay&name=Wallet'
    )

    expect(redirect?.replace).toBe(true)
    expect(
      getWalletReturnRedirect(
        '/wallet',
        defaultStringifySearch(redirect?.search ?? {})
      )
    ).toBeNull()
  })

  test('does not interpret unrelated routes as payment returns', () => {
    expect(
      getWalletReturnRedirect('/billing', '?pid=1&out_trade_no=WT-order')
    ).toBeNull()
  })

  test.each([
    'WTMUiUJaHNUCwXu8FmaoGU8ujEJVAsJ',
    '2026102523001457571419100748',
  ])(
    'loads a gateway return for %s through the real router with one clean replacement',
    async (merchantTradeNo) => {
      let normalizations = 0
      const history = createMemoryHistory({
        initialEntries: [
          `/wallet?pid=2026101814360916&trade_no=202610252300145757141910074899999999999999999999&out_trade_no=${merchantTradeNo}&type=alipay&name=Wallet&money=100.00&trade_status=TRADE_SUCCESS&sign=secret&sign_type=MD5`,
        ],
      })
      const root = createRootRouteWithContext<{ history: RouterHistory }>()({
        beforeLoad: ({ location, context, preload }) => {
          const rawLocation = context.history.location
          const destination = getWalletReturnRedirect(
            location.pathname,
            !preload && rawLocation.pathname === location.pathname
              ? rawLocation.search
              : location.searchStr
          )
          if (destination) {
            normalizations++
            throw redirect(destination)
          }
        },
      })
      const wallet = createRoute({
        getParentRoute: () => root,
        path: '/wallet',
        validateSearch: walletSearchSchema,
      })
      const router = createRouter({
        routeTree: root.addChildren([wallet]),
        history,
        context: { history },
      })
      const unsubscribe = history.subscribe(() => {
        void router.load()
      })
      try {
        await router.load()

        expect(normalizations).toBe(1)
        expect(router.state.location.pathname).toBe('/wallet')
        expect(router.state.matches.every((match) => !match.error)).toBe(true)
        expect(router.state.matches.at(-1)?.search).toMatchObject({
          trade_no: merchantTradeNo,
        })
        const cleaned = new URLSearchParams(router.state.location.searchStr)
        expect([...cleaned.keys()]).toEqual(['trade_no'])
        expect(history.length).toBe(1)

        await router.invalidate()
        expect(normalizations).toBe(1)
        expect(router.state.matches.every((match) => !match.error)).toBe(true)

        await router.navigate({
          to: '/wallet',
          search: { trade_no: '900719925474099312345' },
        })
        expect(router.state.matches.at(-1)?.search).toMatchObject({
          trade_no: '900719925474099312345',
        })
        expect(normalizations).toBe(1)
        expect(router.state.matches.every((match) => !match.error)).toBe(true)
      } finally {
        unsubscribe()
      }
    }
  )

  test('keeps persistent root query failures in the error boundary without navigation', async () => {
    const client = createAppQueryClient()
    const error = Object.assign(new Error('Site configuration unavailable'), {
      response: { status: 500 },
    })
    const fetchSite = vi.fn(async () => {
      throw error
    })
    const root = createRootRoute({
      beforeLoad: async () => {
        await client.fetchQuery({
          queryKey: ['wallet-return-site-config'],
          queryFn: fetchSite,
          retry: false,
          meta: { errorToast: false },
        })
      },
    })
    const wallet = createRoute({
      getParentRoute: () => root,
      path: '/wallet',
    })
    const errorPage = createRoute({
      getParentRoute: () => root,
      path: '/500',
    })
    const history = createMemoryHistory({ initialEntries: ['/wallet'] })
    const router = createRouter({
      routeTree: root.addChildren([wallet, errorPage]),
      history,
    })
    const navigate = vi.spyOn(router, 'navigate')
    const historyChanges = vi.fn()
    const unsubscribe = history.subscribe(historyChanges)
    try {
      await router.load()

      expect(fetchSite).toHaveBeenCalledTimes(1)
      expect(router.state.matches[0]?.error).toBe(error)
      expect(router.state.location.pathname).toBe('/wallet')
      expect(navigate).not.toHaveBeenCalled()
      expect(historyChanges).not.toHaveBeenCalled()

      await router.invalidate()

      expect(fetchSite).toHaveBeenCalledTimes(2)
      expect(router.state.matches[0]?.error).toBe(error)
      expect(router.state.location.pathname).toBe('/wallet')
      expect(navigate).not.toHaveBeenCalled()
      expect(historyChanges).not.toHaveBeenCalled()
    } finally {
      unsubscribe()
      client.clear()
    }
  })
})
