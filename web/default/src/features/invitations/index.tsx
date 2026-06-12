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
import { useQuery } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { Gift, History, Wallet } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { SectionPageLayout } from '@/components/layout'
import { getMyCode, getRebateRecords } from './api'
import { InvitationCodeCard } from './components/invitation-code-card'
import { RebateManagement } from './components/rebate-management'
import { RebateRecordsTable } from './components/rebate-records-table'
import { RebateTrendChart } from './components/rebate-trend-chart'
import { useInvitationFeatureStatus } from './hooks/use-invitation-feature-status'

type TabValue = 'invite' | 'records' | 'rebate'

const DEFAULT_TAB: TabValue = 'invite'

export function Invitations() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const search = useSearch({ from: '/_authenticated/invitations/' })
  const invitationFeature = useInvitationFeatureStatus()

  // 从 URL query 获取当前 tab，默认为 'invite'
  const currentTab = (search.tab as TabValue) || DEFAULT_TAB
  const activeTab =
    (currentTab === 'records' && !invitationFeature.rebateRecordsVisible) ||
    (currentTab === 'rebate' && !invitationFeature.rebateManagementVisible)
      ? DEFAULT_TAB
      : currentTab

  // 获取邀请码和统计信息
  const { data: statsData, isLoading: statsLoading } = useQuery({
    queryKey: ['invitationStats'],
    queryFn: async () => {
      const response = await getMyCode()
      return response.data
    },
    enabled: invitationFeature.userVisible,
  })

  // 获取返利记录（用于图表）
  const { data: recordsData } = useQuery({
    queryKey: ['rebateRecordsForChart'],
    queryFn: async () => {
      const response = await getRebateRecords({ pageSize: 1000 })
      return response.data
    },
    enabled: invitationFeature.rebateRecordsVisible,
  })

  // Tab 切换处理
  const handleTabChange = useCallback(
    (value: string) => {
      navigate({
        to: '/invitations',
        search: { tab: value as TabValue },
      })
    },
    [navigate]
  )

  if (!invitationFeature.userVisible) {
    return null
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {invitationFeature.hasAnyRebateFeature
          ? t('Invitation Rebate')
          : t('My Invitation')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Description>
        {invitationFeature.hasAnyRebateFeature
          ? t('Invite friends to earn rebates')
          : t('View your invitation count')}
      </SectionPageLayout.Description>
      <SectionPageLayout.Content>
        <div className='mx-auto w-full max-w-7xl'>
          <Tabs value={activeTab} onValueChange={handleTabChange}>
            <TabsList className='mb-6'>
              <TabsTrigger value='invite' className='gap-2'>
                <Gift className='size-4' />
                {t('My Invitation')}
              </TabsTrigger>
              {invitationFeature.rebateRecordsVisible && (
                <TabsTrigger value='records' className='gap-2'>
                  <History className='size-4' />
                  {t('Rebate Records')}
                </TabsTrigger>
              )}
              {invitationFeature.rebateManagementVisible && (
                <TabsTrigger value='rebate' className='gap-2'>
                  <Wallet className='size-4' />
                  {t('Rebate Management')}
                </TabsTrigger>
              )}
            </TabsList>

            <TabsContent value='invite'>
              <InvitationCodeCard
                stats={statsData ?? null}
                loading={statsLoading}
                showRebateStats={invitationFeature.hasAnyRebateFeature}
              />
            </TabsContent>

            {invitationFeature.rebateRecordsVisible && (
              <TabsContent value='records'>
                <div className='space-y-6'>
                  <RebateTrendChart
                    records={recordsData?.items ?? []}
                    days={30}
                  />
                  <RebateRecordsTable />
                </div>
              </TabsContent>
            )}

            {invitationFeature.rebateManagementVisible && (
              <TabsContent value='rebate'>
                <RebateManagement />
              </TabsContent>
            )}
          </Tabs>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
