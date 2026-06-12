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
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { getRebateRules } from '../../api'
import { RebateRuleDialog } from './rebate-rule-dialog'
import { RebateRulesTable } from './rebate-rules-table'
import { SystemConfigCard } from './system-config-card'

export function RebateRulesTab() {
  const { t } = useTranslation()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingRuleId, setEditingRuleId] = useState<number | null>(null)

  // 获取返利规则列表
  const { data: rulesData, isLoading } = useQuery({
    queryKey: ['rebateRules'],
    queryFn: async () => {
      const response = await getRebateRules()
      return response.data ?? []
    },
  })

  const handleCreate = () => {
    setEditingRuleId(null)
    setDialogOpen(true)
  }

  const handleEdit = (id: number) => {
    setEditingRuleId(id)
    setDialogOpen(true)
  }

  const handleDialogClose = () => {
    setDialogOpen(false)
    setEditingRuleId(null)
  }

  return (
    <div className='space-y-6'>
      <Card>
        <CardHeader className='flex flex-row items-center justify-between'>
          <CardTitle>{t('Rebate Rules')}</CardTitle>
          <Button onClick={handleCreate} size='sm'>
            <Plus className='mr-2 size-4' />
            {t('Create Rule')}
          </Button>
        </CardHeader>
        <CardContent>
          <RebateRulesTable
            rules={rulesData ?? []}
            loading={isLoading}
            onEdit={handleEdit}
          />
        </CardContent>
      </Card>

      <SystemConfigCard />

      <RebateRuleDialog
        open={dialogOpen}
        onClose={handleDialogClose}
        editingRuleId={editingRuleId}
      />
    </div>
  )
}
