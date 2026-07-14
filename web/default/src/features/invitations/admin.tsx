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
import { useNavigate, useSearch } from '@tanstack/react-router'
import {
  BarChart3,
  CheckCircle2,
  ReceiptText,
  Settings,
  UserRoundPlus,
} from 'lucide-react'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import { InvitationRegistrations } from './components/admin/invitation-registrations'
import { RebateApprovals } from './components/admin/rebate-approvals'
import { RebateOrderRecords } from './components/admin/rebate-order-records'
import { RebatePayoutRecords } from './components/admin/rebate-payout-records'
import { RebateRules } from './components/admin/rebate-rules'
import { RebateStatistics } from './components/admin/rebate-statistics'
import { SystemConfigForm } from './components/admin/system-config-form'
import { useInvitationFeatureStatus } from './hooks/use-invitation-feature-status'

type AdminTab =
  | 'settings'
  | 'records'
  | 'registrations'
  | 'approvals'
  | 'statistics'

export const InvitationsAdmin = () => {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const search = useSearch({ from: '/_authenticated/invitations/admin' })
  const feature = useInvitationFeatureStatus()
  const activeTab = (search.tab ?? 'settings') as AdminTab

  useEffect(() => {
    if (feature.query.isPending || feature.adminVisible) return
    void navigate({
      to: '/dashboard/$section',
      params: { section: 'overview' },
      replace: true,
    })
  }, [feature.adminVisible, feature.query.isPending, navigate])

  if (!feature.adminVisible) return null

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Invitation Operations')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <Tabs
          value={activeTab}
          onValueChange={(value) => {
            void navigate({
              to: '/invitations/admin',
              search: { tab: value as AdminTab },
            })
          }}
        >
          <TabsList className='mb-4 h-auto max-w-full flex-wrap justify-start'>
            <TabsTrigger value='settings'>
              <Settings aria-hidden='true' />
              {t('Settings')}
            </TabsTrigger>
            <TabsTrigger value='records'>
              <ReceiptText aria-hidden='true' />
              {t('Records')}
            </TabsTrigger>
            <TabsTrigger value='registrations'>
              <UserRoundPlus aria-hidden='true' />
              {t('Registrations')}
            </TabsTrigger>
            <TabsTrigger value='approvals'>
              <CheckCircle2 aria-hidden='true' />
              {t('Approvals')}
            </TabsTrigger>
            <TabsTrigger value='statistics'>
              <BarChart3 aria-hidden='true' />
              {t('Statistics')}
            </TabsTrigger>
          </TabsList>
          <TabsContent value='settings' className='space-y-4'>
            <RebateRules />
            <SystemConfigForm />
          </TabsContent>
          <TabsContent value='records' className='space-y-4'>
            <RebateOrderRecords />
            <RebatePayoutRecords />
          </TabsContent>
          <TabsContent value='registrations'>
            <InvitationRegistrations />
          </TabsContent>
          <TabsContent value='approvals'>
            <RebateApprovals />
          </TabsContent>
          <TabsContent value='statistics'>
            <RebateStatistics />
          </TabsContent>
        </Tabs>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
