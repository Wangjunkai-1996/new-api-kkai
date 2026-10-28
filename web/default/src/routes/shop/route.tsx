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

import { PublicLayout } from '@/components/layout/components/public-layout'
import { getSiteConfig } from '@/features/site-config/api'

import '@/styles/shop.css'

export const Route = createFileRoute('/shop')({
  beforeLoad: async () => {
    const site = await getSiteConfig()
    if (site.tenant_id !== 0) throw redirect({ to: '/403' })
  },
  component: ShopLayout,
})
function ShopLayout() {
  const { t } = useTranslation()
  return (
    <PublicLayout>
      <div className='shop'>
        <nav className='shop-subnav' aria-label={t('KK Shop')}>
          <Link to='/shop' search={{ page: 1 }}>
            {t('Shop')}
          </Link>
          <Link to='/shop/orders' search={{ page: 1 }}>
            {t('My orders')}
          </Link>
        </nav>
        <Outlet />
      </div>
    </PublicLayout>
  )
}
