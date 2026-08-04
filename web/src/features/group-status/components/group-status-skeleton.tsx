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

import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'

const SUMMARY_KEYS = ['summary-a', 'summary-b', 'summary-c', 'summary-d']
const MOBILE_KEYS = ['mobile-a', 'mobile-b', 'mobile-c']
const TABLE_KEYS = ['row-a', 'row-b', 'row-c', 'row-d', 'row-e']

export function GroupStatusSkeleton() {
  return (
    <div className='space-y-4' aria-hidden='true'>
      <div className='grid grid-cols-2 border-y sm:grid-cols-4'>
        {SUMMARY_KEYS.map((key) => (
          <div
            key={key}
            className='space-y-2 border-r px-3 py-4 last:border-r-0'
          >
            <Skeleton className='h-3 w-16' />
            <Skeleton className='h-6 w-12' />
          </div>
        ))}
      </div>
      <div className='grid gap-3 lg:hidden'>
        {MOBILE_KEYS.map((key) => (
          <Card key={key} size='sm' className='rounded-lg'>
            <CardContent className='space-y-3'>
              <Skeleton className='h-5 w-40' />
              <Skeleton className='h-20 w-full' />
              <Skeleton className='h-7 w-full' />
            </CardContent>
          </Card>
        ))}
      </div>
      <Card className='hidden rounded-lg py-3 lg:block'>
        <CardContent className='space-y-2'>
          {TABLE_KEYS.map((key) => (
            <Skeleton key={key} className='h-12 w-full' />
          ))}
        </CardContent>
      </Card>
    </div>
  )
}
