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

import { SectionPageLayout } from '@/components/layout/components/section-page-layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'

import { getMembershipAdminConfig } from './api'
import { MembershipBillingReviews } from './components/billing-reviews'
import { MembershipCatalogEditor } from './components/catalog-editor'
import { MembershipOrders } from './components/orders'

export function MembershipAdmin() {
  const { t } = useTranslation()
  const config = useQuery({
    queryKey: ['membership-admin', 'config'],
    queryFn: getMembershipAdminConfig,
  })
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Membership management')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          variant='outline'
          disabled={config.isFetching}
          onClick={() => void config.refetch()}
        >
          {t('Reload configuration')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='mx-auto w-full max-w-7xl space-y-8 pb-8'>
          {config.isPending && <Skeleton className='h-80 w-full' />}
          {config.isError && (
            <Alert variant='destructive'>
              <AlertDescription>
                {t('Could not load membership configuration.')}
              </AlertDescription>
            </Alert>
          )}
          {config.data?.runtime_enabled === false &&
            config.data.schema_ready !== false && (
              <Alert>
                <AlertDescription>
                  {t('Memberships are not available yet.')}
                  <span className='block'>
                    {t('Runtime')}: {t('Disabled')}
                  </span>
                </AlertDescription>
              </Alert>
            )}
          {config.data?.schema_ready === false && (
            <Alert>
              <AlertDescription>
                {t('Memberships are not available yet.')}
                <span className='block'>
                  {t('Database')}: {t('Unavailable')}
                </span>
              </AlertDescription>
            </Alert>
          )}
          {config.data && config.data.schema_ready !== false && (
            <>
              <MembershipCatalogEditor
                key={config.data.release_id}
                catalog={config.data}
              />
              <MembershipOrders admin />
              <MembershipBillingReviews />
            </>
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
