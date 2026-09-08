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
import { Plus, Search } from 'lucide-react'
import { useState, useMemo, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table/static/static-data-table'
import { StaticRowActions } from '@/components/data-table/static/static-row-actions'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

import { safeJsonParseWithValidation } from '../utils/json-parser'
import { isObjectRecord } from '../utils/json-validators'
import { RateLimitDialog, type RateLimitEntryData } from './rate-limit-dialog'

type RateLimitVisualEditorProps = {
  value: string
  onChange: (value: string) => void
  scope?: 'group' | 'user'
}

type RateLimitEntry = RateLimitEntryData

export function RateLimitVisualEditor({
  value,
  onChange,
  scope = 'group',
}: RateLimitVisualEditorProps) {
  const { t } = useTranslation()
  const [searchText, setSearchText] = useState('')
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editData, setEditData] = useState<RateLimitEntry | null>(null)
  const isUserScope = scope === 'user'

  const rateLimits = useMemo(() => {
    if (!value || value.trim() === '') return []

    const parsed = safeJsonParseWithValidation<Record<string, unknown>>(value, {
      fallback: {},
      validator: isObjectRecord,
      validatorMessage: 'Rate limits must be a JSON object',
      context: 'rate limits',
    })

    return Object.entries(parsed)
      .map(([target, limits]) => {
        if (
          Array.isArray(limits) &&
          limits.length === 2 &&
          typeof limits[0] === 'number' &&
          typeof limits[1] === 'number'
        ) {
          return {
            target,
            maxRequests: limits[0],
            maxSuccess: limits[1],
          }
        }
        return null
      })
      .filter((item): item is RateLimitEntry => item !== null)
  }, [value])

  const filteredRateLimits = useMemo(() => {
    if (!searchText) return rateLimits
    const lowerSearch = searchText.toLowerCase()
    return rateLimits.filter((limit) =>
      limit.target.toLowerCase().includes(lowerSearch)
    )
  }, [rateLimits, searchText])

  const handleSave = (data: RateLimitEntryData) => {
    const parsed = safeJsonParseWithValidation<Record<string, unknown>>(value, {
      fallback: {},
      validator: isObjectRecord,
      silent: true,
    })

    if (editData && editData.target !== data.target) {
      delete parsed[editData.target]
    }

    parsed[data.target] = [data.maxRequests, data.maxSuccess]

    onChange(JSON.stringify(parsed, null, 2))
  }

  const handleDelete = (target: string) => {
    const parsed = safeJsonParseWithValidation<Record<string, unknown>>(value, {
      fallback: {},
      validator: isObjectRecord,
      silent: true,
    })

    delete parsed[target]

    onChange(JSON.stringify(parsed, null, 2))
  }

  const handleEdit = (limit: RateLimitEntry) => {
    setEditData(limit)
    setDialogOpen(true)
  }

  const handleAdd = () => {
    setEditData(null)
    setDialogOpen(true)
  }

  let emptyContent: ReactNode
  if (searchText) {
    emptyContent = isUserScope
      ? t('No users match your search')
      : t('No groups match your search')
  } else {
    emptyContent = isUserScope
      ? t(
          'No user-specific rate limits configured. Click "Add user" to get started.'
        )
      : t(
          'No group-based rate limits configured. Click "Add group" to get started.'
        )
  }

  return (
    <div className='space-y-4'>
      <div className='flex items-center gap-4'>
        <div className='relative flex-1'>
          <Search className='text-muted-foreground absolute top-2.5 left-2.5 h-4 w-4' />
          <Input
            placeholder={
              isUserScope ? t('Search users...') : t('Search group names...')
            }
            value={searchText}
            onChange={(e) => setSearchText(e.target.value)}
            className='pl-9'
          />
        </div>
        <Button type='button' onClick={handleAdd}>
          <Plus className='mr-2 h-4 w-4' />
          {isUserScope ? t('Add user') : t('Add group')}
        </Button>
      </div>

      <StaticDataTable
        data={filteredRateLimits}
        getRowKey={(limit) => limit.target}
        emptyContent={emptyContent}
        columns={[
          {
            id: 'target',
            header: isUserScope ? t('User') : t('Group Name'),
            cellClassName: 'font-medium',
            cell: (limit) => limit.target,
          },
          {
            id: 'max-requests',
            header: t('Max Requests (incl. failures)'),
            className: 'text-right',
            cellClassName: 'text-right',
            cell: (limit) => (
              <span className='font-mono'>
                {limit.maxRequests === 0
                  ? t('Unlimited')
                  : limit.maxRequests.toLocaleString()}
              </span>
            ),
          },
          {
            id: 'max-success',
            header: t('Max Success'),
            className: 'text-right',
            cellClassName: 'text-right',
            cell: (limit) => (
              <span className='font-mono'>
                {limit.maxSuccess.toLocaleString()}
              </span>
            ),
          },
          {
            id: 'actions',
            header: t('Actions'),
            className: 'text-right',
            cellClassName: 'text-right',
            cell: (limit) => (
              <StaticRowActions
                editLabel={t('Edit')}
                deleteLabel={t('Delete')}
                menuLabel={t('Open menu')}
                onEdit={() => handleEdit(limit)}
                onDelete={() => handleDelete(limit.target)}
              />
            ),
          },
        ]}
      />

      <RateLimitDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        onSave={handleSave}
        editData={editData}
        scope={scope}
      />
    </div>
  )
}
