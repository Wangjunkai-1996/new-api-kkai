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
import { useNavigate, useParams } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { PublicLayout } from '@/components/layout'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import { BrandPanel } from './brand-panel'
import { CatalogPanel } from './catalog-panel'
import { CustomersPanel } from './customers-panel'
import { EarningsPanel } from './earnings-panel'
import { WithdrawalsPanel } from './withdrawals-panel'

export function ResellerWorkspace() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { section } = useParams({ from: '/reseller/$section' })
  const selectedTab = section === 'pricing' ? 'catalog' : section
  return (
    <PublicLayout>
      <section className='mx-auto max-w-6xl space-y-6 py-8'>
        <header className='space-y-2'>
          <h1 className='text-2xl font-semibold'>{t('Reseller Workspace')}</h1>
          <p className='text-muted-foreground'>
            {t('Manage your brand, customers, pricing and earnings.')}
          </p>
        </header>
        <Tabs
          value={selectedTab}
          onValueChange={(value) =>
            void navigate({
              to: '/reseller/$section',
              params: { section: value },
            })
          }
        >
          <div className='overflow-x-auto pb-2'>
            <TabsList>
              <TabsTrigger value='brand'>{t('Site & Branding')}</TabsTrigger>
              <TabsTrigger value='catalog'>{t('Models & Pricing')}</TabsTrigger>
              <TabsTrigger value='customers'>{t('Customers')}</TabsTrigger>
              <TabsTrigger value='earnings'>{t('Earnings')}</TabsTrigger>
              <TabsTrigger value='withdrawals'>{t('Withdrawals')}</TabsTrigger>
            </TabsList>
          </div>
          <TabsContent value='brand'>
            <BrandPanel />
          </TabsContent>
          <TabsContent value='catalog'>
            <CatalogPanel />
          </TabsContent>
          <TabsContent value='customers'>
            <CustomersPanel />
          </TabsContent>
          <TabsContent value='earnings'>
            <EarningsPanel />
          </TabsContent>
          <TabsContent value='withdrawals'>
            <WithdrawalsPanel />
          </TabsContent>
        </Tabs>
      </section>
    </PublicLayout>
  )
}
