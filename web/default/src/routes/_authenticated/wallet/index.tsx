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
import { createFileRoute } from '@tanstack/react-router'

import { Wallet } from '@/features/wallet'
import { walletSearchSchema } from '@/lib/wallet-return'

export const Route = createFileRoute('/_authenticated/wallet/')({
  component: RouteComponent,
  validateSearch: walletSearchSchema,
})

function RouteComponent() {
  const { show_history, trade_no, history_scope, invalid_payment_return } =
    Route.useSearch()
  return (
    <Wallet
      key={`${trade_no ?? ''}:${history_scope ?? 'self'}:${show_history === true}`}
      initialShowHistory={show_history}
      initialTradeNo={trade_no}
      invalidPaymentReturn={invalid_payment_return}
      historyScope={history_scope}
    />
  )
}
