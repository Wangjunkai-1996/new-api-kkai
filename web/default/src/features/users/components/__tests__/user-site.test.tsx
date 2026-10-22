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
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { cleanup, render, screen, within } from '@testing-library/react'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, expect, it } from 'vitest'

import zh from '@/i18n/locales/zh.json'

import { type User, userSchema } from '../../types'
import { useUsersColumns } from '../users-columns'

function UserSiteTable(props: { users: User[] }) {
  'use no memo'
  const columns = useUsersColumns().filter(
    (column) => 'accessorKey' in column && column.accessorKey === 'tenant_name'
  )
  // oxlint-disable-next-line react/incompatible-library -- TanStack Table exposes a mutable instance; this component opts out of compiler memoization.
  const table = useReactTable({
    columns,
    data: props.users,
    getCoreRowModel: getCoreRowModel(),
  })
  return (
    <table>
      <thead>
        {table.getHeaderGroups().map((group) => (
          <tr key={group.id}>
            {group.headers.map((header) => (
              <th key={header.id}>
                {flexRender(
                  header.column.columnDef.header,
                  header.getContext()
                )}
              </th>
            ))}
          </tr>
        ))}
      </thead>
      <tbody>
        {table.getRowModel().rows.map((row) => (
          <tr key={row.id}>
            {row.getVisibleCells().map((cell) => (
              <td key={cell.id}>
                {flexRender(cell.column.columnDef.cell, cell.getContext())}
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  )
}

afterEach(cleanup)

it('distinguishes main-site users, reseller customers, and unavailable site metadata', async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const i18n = createInstance()
  await i18n.init({ lng: 'zh', resources: { zh }, initAsync: false })
  const users = [
    { tenant_id: 0 },
    {
      tenant_id: 42,
      tenant_name: '示例分站',
      tenant_domain: 'reseller.example.test',
    },
    { tenant_id: 43, tenant_name: '', tenant_domain: '' },
    {},
  ].map((site, index) =>
    userSchema.parse({
      id: index + 1,
      username: `user-${index}`,
      display_name: '',
      role: 1,
      status: 1,
      quota: 0,
      used_quota: 0,
      request_count: 0,
      group: 'default',
      ...site,
    })
  )

  render(
    <QueryClientProvider client={client}>
      <I18nextProvider i18n={i18n}>
        <UserSiteTable users={users} />
      </I18nextProvider>
    </QueryClientProvider>
  )

  expect(
    screen.getByRole('columnheader', { name: '所属站点' })
  ).toBeInTheDocument()
  const cells = screen.getAllByRole('cell')
  expect(cells[0]).toHaveTextContent(/^主站$/)
  expect(within(cells[1]).getByText('分站用户')).toBeInTheDocument()
  expect(within(cells[1]).getByText('示例分站')).toBeInTheDocument()
  expect(
    within(cells[1]).getByText('reseller.example.test')
  ).toBeInTheDocument()
  expect(within(cells[2]).getByText('分站用户')).toBeInTheDocument()
  expect(within(cells[2]).getByText('分站 #43')).toBeInTheDocument()
  expect(cells[3]).toHaveTextContent(/^—$/)
  client.clear()
})
