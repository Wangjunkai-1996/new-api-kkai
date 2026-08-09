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

const CARD_KEYS = ['card-a', 'card-b', 'card-c', 'card-d', 'card-e', 'card-f']

export function GroupStatusSkeleton() {
  return (
    <div className='space-y-4' aria-hidden='true'>
      <div className='flex min-h-11 items-center justify-between rounded-lg border px-3 py-2'>
        <div className='flex gap-2'>
          <Skeleton className='h-5 w-20 rounded-full' />
          <Skeleton className='h-5 w-16 rounded-full' />
        </div>
        <Skeleton className='h-3 w-24' />
      </div>
      <div className='grid grid-cols-[repeat(auto-fill,minmax(min(100%,24rem),1fr))] gap-3'>
        {CARD_KEYS.map((key) => (
          <Card key={key} size='sm' className='min-h-[22rem] rounded-lg py-0'>
            <CardContent className='flex h-full flex-col gap-4 p-4'>
              <div className='flex items-center gap-3'>
                <Skeleton className='size-11 rounded-lg' />
                <div className='min-w-0 flex-1 space-y-2'>
                  <Skeleton className='h-4 w-32' />
                  <Skeleton className='h-3 w-44 max-w-full' />
                </div>
                <Skeleton className='h-5 w-14 rounded-full' />
              </div>
              <div className='grid grid-cols-2 gap-2.5'>
                <Skeleton className='h-20 rounded-lg' />
                <Skeleton className='h-20 rounded-lg' />
              </div>
              <div className='flex items-end justify-between border-y py-4'>
                <Skeleton className='h-8 w-28' />
                <Skeleton className='h-9 w-24' />
              </div>
              <div className='mt-auto space-y-2'>
                <Skeleton className='h-3 w-28' />
                <Skeleton className='h-10 w-full' />
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  )
}
