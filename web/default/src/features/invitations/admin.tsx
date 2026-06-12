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
import { useCallback } from 'react'
import { useNavigate, useSearch } from '@tanstack/react-router'
import {
  Settings,
  ReceiptText,
  CheckCircle,
  BarChart3,
  UserRoundPlus,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { SectionPageLayout } from '@/components/layout'
import { InvitationRegistrationsTab } from './components/admin/invitation-registrations-tab'
import { RebateApprovalsTab } from './components/admin/rebate-approvals-tab'
import { RebateOrderRecordsTab } from './components/admin/rebate-order-records-tab'
import { RebateRulesTab } from './components/admin/rebate-rules-tab'
import { StatisticsTab } from './components/admin/statistics-tab'
import { useInvitationFeatureStatus } from './hooks/use-invitation-feature-status'

type TabValue =
  | 'rules'
  | 'records'
  | 'registrations'
  | 'approvals'
  | 'statistics'

const DEFAULT_TAB: TabValue = 'rules'

export function InvitationsAdmin() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const search = useSearch({ strict: false }) as { tab?: TabValue }
  const invitationFeature = useInvitationFeatureStatus()

  // 从 URL query 获取当前 tab
  const currentTab = search.tab || DEFAULT_TAB

  // Tab 切换处理
  const handleTabChange = useCallback(
    (value: string) => {
      navigate({
        to: '/invitations/admin',
        search: { tab: value as TabValue },
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
      } as any)
    },
    [navigate]
  )

  if (!invitationFeature.available) {
    return null
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Rebate Management')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Description>
        {t(
          'Manage rebate rules, rebate records, invitation registrations, rebate approvals, and statistics'
        )}
      </SectionPageLayout.Description>
      <SectionPageLayout.Content>
        <div className='mx-auto w-full max-w-7xl'>
          <Tabs value={currentTab} onValueChange={handleTabChange}>
            <TabsList className='mb-6 h-auto flex-wrap justify-start'>
              <TabsTrigger value='rules' className='gap-2'>
                <Settings className='size-4' />
                {t('Rebate Rules')}
              </TabsTrigger>
              <TabsTrigger value='records' className='gap-2'>
                <ReceiptText className='size-4' />
                {t('Rebate Records')}
              </TabsTrigger>
              <TabsTrigger value='registrations' className='gap-2'>
                <UserRoundPlus className='size-4' />
                {t('Invitation Registrations')}
              </TabsTrigger>
              <TabsTrigger value='approvals' className='gap-2'>
                <CheckCircle className='size-4' />
                {t('Rebate Approvals')}
              </TabsTrigger>
              <TabsTrigger value='statistics' className='gap-2'>
                <BarChart3 className='size-4' />
                {t('Statistics')}
              </TabsTrigger>
            </TabsList>

            <TabsContent value='rules'>
              <RebateRulesTab />
            </TabsContent>

            <TabsContent value='records'>
              <RebateOrderRecordsTab />
            </TabsContent>

            <TabsContent value='registrations'>
              <InvitationRegistrationsTab />
            </TabsContent>

            <TabsContent value='approvals'>
              <RebateApprovalsTab />
            </TabsContent>

            <TabsContent value='statistics'>
              <StatisticsTab />
            </TabsContent>
          </Tabs>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
