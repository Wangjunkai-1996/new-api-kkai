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
import { defaultStringifySearch } from '@tanstack/react-router'
import { z } from 'zod'

const tradeNumberPattern = /^[A-Za-z0-9_-]{1,128}$/

// Raw gateway parameters are normalized by the root before authentication.
// Validation must not turn malformed return links into a route error first.
export const walletSearchSchema = z.object({
  show_history: z.boolean().optional().catch(undefined),
  trade_no: z.string().regex(tradeNumberPattern).optional().catch(undefined),
  history_scope: z.enum(['self', 'admin']).optional().catch(undefined),
  invalid_payment_return: z.boolean().optional().catch(undefined),
})

export function getWalletReturnRedirect(pathname: string, searchStr: string) {
  const path = pathname.replace(/\/$/, '')
  if (path !== '/wallet' && path !== '/console/topup') return null

  const params = new URLSearchParams(searchStr)
  const gatewayReturn = [
    'out_trade_no',
    'pid',
    'sign',
    'sign_type',
    'trade_status',
    'money',
    'type',
    'name',
  ].some((key) => params.has(key))
  const orderValues = params.getAll(gatewayReturn ? 'out_trade_no' : 'trade_no')
  let tradeNo = orderValues.length === 1 ? orderValues[0] : undefined
  // Router-generated numeric-looking strings are JSON quoted. Decode only
  // quoted internal values; never parse a gateway ID as a JavaScript number.
  if (!gatewayReturn && tradeNo?.startsWith('"')) {
    try {
      const decoded: unknown = JSON.parse(tradeNo)
      tradeNo = typeof decoded === 'string' ? decoded : undefined
    } catch {
      tradeNo = undefined
    }
  }
  const validTradeNo = tradeNo !== undefined && tradeNumberPattern.test(tradeNo)
  const invalidReturn =
    params.get('invalid_payment_return') === 'true' ||
    ((gatewayReturn || params.has('trade_no')) && !validTradeNo)
  const search: z.infer<typeof walletSearchSchema> = {}
  if (validTradeNo) search.trade_no = tradeNo
  if (invalidReturn) search.invalid_payment_return = true
  if (
    params.getAll('show_history').length === 1 &&
    params.get('show_history') === 'true'
  ) {
    search.show_history = true
  } else if (path === '/console/topup') {
    search.show_history = true
  }
  const historyScope = params.get('history_scope')
  if (
    params.getAll('history_scope').length === 1 &&
    (historyScope === 'self' || historyScope === 'admin')
  ) {
    search.history_scope = historyScope
  }
  if (pathname === '/wallet' && searchStr === defaultStringifySearch(search)) {
    return null
  }
  return { to: '/wallet' as const, search, replace: true as const }
}
