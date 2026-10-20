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
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useAuthStore } from '@/stores/auth-store'

import { getManagedResellers } from './admin-api'
import { AdminApplications } from './admin-applications'
import { AdminTenants } from './admin-tenants'
import { AdminWithdrawals } from './admin-withdrawals'
import { resellerQueryKey } from './api'

export function ResellerAdmin() {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const query = useQuery({
    queryKey: resellerQueryKey(userId, 'admin'),
    queryFn: getManagedResellers,
  })
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Reseller Management')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          variant='outline'
          disabled={query.isFetching}
          onClick={() => void query.refetch()}
        >
          {t('Refresh')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <Tabs defaultValue='applications'>
          <TabsList>
            <TabsTrigger value='applications'>{t('Applications')}</TabsTrigger>
            <TabsTrigger value='sites'>{t('Sites')}</TabsTrigger>
            <TabsTrigger value='withdrawals'>{t('Withdrawals')}</TabsTrigger>
          </TabsList>
          {query.isPending && <Skeleton className='mt-4 h-64 w-full' />}
          {query.isError && (
            <ErrorState
              title={t('Failed to load data')}
              description={query.error.message}
              onRetry={() => void query.refetch()}
            />
          )}
          {query.data && (
            <>
              <TabsContent value='applications'>
                <AdminApplications
                  applications={query.data.applications}
                  rootDomain={query.data.root_domain}
                />
              </TabsContent>
              <TabsContent value='sites'>
                <AdminTenants
                  tenants={query.data.tenants}
                  applications={query.data.applications}
                  rootDomain={query.data.root_domain}
                />
              </TabsContent>
            </>
          )}
          <TabsContent value='withdrawals'>
            <AdminWithdrawals />
          </TabsContent>
        </Tabs>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
