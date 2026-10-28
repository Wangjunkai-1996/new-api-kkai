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
import { Link, Outlet, createFileRoute, redirect } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { getSiteConfig } from '@/features/site-config/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import '@/styles/shop.css'

export const Route = createFileRoute('/_authenticated/shop-admin')({
  beforeLoad: async () => {
    const user = useAuthStore.getState().auth.user
    if (!user || user.role < ROLE.ADMIN) throw redirect({ to: '/403' })
    const site = await getSiteConfig()
    if (site.tenant_id !== 0) throw redirect({ to: '/403' })
  },
  component: ShopAdminLayout,
})
function ShopAdminLayout() {
  const { t } = useTranslation()
  return (
    <main className='shop p-4 md:p-6'>
      <nav className='shop-subnav' aria-label={t('Shop administration')}>
        <Link to='/shop-admin' activeOptions={{ exact: true }}>
          {t('Overview')}
        </Link>
        <Link to='/shop-admin/products'>{t('Products')}</Link>
        <Link to='/shop-admin/inventory'>{t('Inventory')}</Link>
        <Link to='/shop-admin/orders'>{t('Orders')}</Link>
        <Link to='/shop-admin/aftersales'>{t('After-sales support')}</Link>
        <Link to='/shop-admin/receipts'>{t('Payment receipts')}</Link>
        <Link to='/shop-admin/refunds'>{t('Refunds')}</Link>
        <Link to='/shop-admin/settings'>{t('Settings')}</Link>
      </nav>
      <Outlet />
    </main>
  )
}
