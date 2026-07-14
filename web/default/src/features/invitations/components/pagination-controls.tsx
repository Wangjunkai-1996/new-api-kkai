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
import { ChevronLeft, ChevronRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

export const PaginationControls = (props: {
  page: number
  pageSize: number
  total: number
  onPageChange: (page: number) => void
}) => {
  const { t } = useTranslation()
  const pageCount = Math.max(1, Math.ceil(props.total / props.pageSize))

  if (props.total <= props.pageSize) return null

  return (
    <div className='flex items-center justify-between gap-3 pt-4'>
      <span className='text-muted-foreground text-sm tabular-nums'>
        {t('Page {{page}} of {{pageCount}}', {
          page: props.page,
          pageCount,
        })}
      </span>
      <div className='flex gap-2'>
        <Button
          type='button'
          size='icon-sm'
          variant='outline'
          disabled={props.page <= 1}
          aria-label={t('Previous page')}
          title={t('Previous page')}
          onClick={() => props.onPageChange(props.page - 1)}
        >
          <ChevronLeft />
        </Button>
        <Button
          type='button'
          size='icon-sm'
          variant='outline'
          disabled={props.page >= pageCount}
          aria-label={t('Next page')}
          title={t('Next page')}
          onClick={() => props.onPageChange(props.page + 1)}
        >
          <ChevronRight />
        </Button>
      </div>
    </div>
  )
}
