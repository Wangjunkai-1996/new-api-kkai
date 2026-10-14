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
import { useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { Gift, History, RefreshCw, WalletCards, Users } from 'lucide-react'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'

import { InvitationSummary } from './components/user/invitation-summary'
import { RebateRecords } from './components/user/rebate-records'
import { RebateRequests } from './components/user/rebate-requests'
import { RebateTransfer } from './components/user/rebate-transfer'
import { SalesCustomers } from './components/user/sales-customers'
import { useInvitationFeatureStatus } from './hooks/use-invitation-feature-status'

type InvitationTab = 'invite' | 'customers' | 'records' | 'rebate'

export const Invitations = () => {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const search = useSearch({ from: '/_authenticated/invitations/' })
  const feature = useInvitationFeatureStatus()
  const requestedTab = (search.tab ??
    (feature.salesVisible ? 'customers' : 'invite')) as InvitationTab
  const activeTab =
    (requestedTab === 'customers' && !feature.salesVisible) ||
    (requestedTab === 'records' && !feature.rebateRecordsVisible) ||
    (requestedTab === 'rebate' && !feature.rebateManagementVisible)
      ? 'invite'
      : requestedTab

  useEffect(() => {
    if (feature.isPending || feature.userVisible) return
    void navigate({ to: '/wallet', replace: true })
  }, [feature.isPending, feature.userVisible, navigate])

  if (!feature.userVisible) return null

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {feature.salesVisible ? t('Sales Invitations') : t('Invitation Rebate')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          type='button'
          size='icon-sm'
          variant='outline'
          disabled={feature.query.isFetching}
          aria-label={t('Refresh')}
          title={t('Refresh')}
          onClick={() =>
            void queryClient.invalidateQueries({
              queryKey: ['kkai', 'invitations'],
            })
          }
        >
          <RefreshCw
            className={cn(feature.query.isFetching && 'animate-spin')}
          />
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <Tabs
          value={activeTab}
          onValueChange={(value) =>
            void navigate({
              to: '/invitations',
              search: { tab: value as InvitationTab },
            })
          }
        >
          <TabsList className='mb-4 h-auto max-w-full flex-wrap justify-start'>
            <TabsTrigger value='invite'>
              <Gift aria-hidden='true' />
              {t('My Invitation')}
            </TabsTrigger>
            {feature.salesVisible && (
              <TabsTrigger value='customers'>
                <Users aria-hidden='true' />
                {t('Invited Customers')}
              </TabsTrigger>
            )}
            {feature.rebateRecordsVisible && (
              <TabsTrigger value='records'>
                <History aria-hidden='true' />
                {t('Rebate Records')}
              </TabsTrigger>
            )}
            {feature.rebateManagementVisible && (
              <TabsTrigger value='rebate'>
                <WalletCards aria-hidden='true' />
                {feature.salesVisible
                  ? t('Rebate Requests')
                  : t('Transfer to Balance')}
              </TabsTrigger>
            )}
          </TabsList>
          <TabsContent value='invite'>
            <InvitationSummary
              showRebates={
                !feature.salesVisible && feature.rebateRecordsVisible
              }
            />
          </TabsContent>
          {feature.salesVisible && (
            <TabsContent value='customers'>
              <SalesCustomers />
            </TabsContent>
          )}
          {feature.rebateRecordsVisible && (
            <TabsContent value='records'>
              <RebateRecords sales={feature.salesVisible} />
            </TabsContent>
          )}
          {feature.rebateManagementVisible && (
            <TabsContent value='rebate' className='space-y-4'>
              <RebateTransfer
                sales={feature.salesVisible}
                disabled={feature.salesVisible && !feature.salesActive}
              />
              <RebateRequests />
            </TabsContent>
          )}
        </Tabs>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
