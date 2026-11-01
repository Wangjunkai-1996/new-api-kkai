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
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { LoadingState } from '@/components/loading-state'
import { useApiInfo } from '@/features/dashboard/hooks/use-status-data'
import { useStatus } from '@/hooks/use-status'
import { getBgColorClass } from '@/lib/colors'

export function ApiKeysAddresses() {
  const { t } = useTranslation()
  const { status, loading } = useStatus()
  const { items } = useApiInfo()
  const serverAddress =
    (typeof status?.server_address === 'string' &&
      status.server_address.trim()) ||
    ''
  const addresses = items.length
    ? items
    : [
        {
          url: serverAddress || window.location.origin,
          route: serverAddress ? t('Default API address') : t('Current domain'),
          description: '',
          color: 'blue',
        },
      ]

  return (
    <section
      className='bg-muted/10 min-w-0 shrink-0 overflow-hidden rounded-[9px] border'
      aria-labelledby='api-keys-addresses-title'
    >
      <div className='flex min-h-11 items-center gap-2 border-b px-4 py-2.5'>
        <h3
          id='api-keys-addresses-title'
          className='text-[13px] leading-none font-semibold'
        >
          {t('API Addresses')}
        </h3>
        {!loading && (
          <span className='text-muted-foreground text-[11px]'>
            {t('Routes: {{count}}', { count: addresses.length })}
          </span>
        )}
      </div>

      {loading ? (
        <div className='px-4 py-4'>
          <LoadingState inline size='sm' message={t('Loading...')} />
        </div>
      ) : (
        <div
          className='flex flex-nowrap gap-2.5 overflow-x-auto px-3.5 pt-3 pb-3.5'
          role='list'
          aria-label={t('API Addresses')}
        >
          {addresses.map((address) => (
            <div
              key={address.url}
              className='bg-muted/30 flex min-w-[250px] flex-1 flex-col gap-2.5 rounded-[8px] border p-[13px] max-sm:min-w-[235px] max-sm:flex-none max-sm:basis-[235px]'
              role='listitem'
            >
              <div className='flex min-w-0 items-center gap-2'>
                <span
                  className={`size-2 shrink-0 rounded-full ${getBgColorClass(address.color)}`}
                  aria-hidden='true'
                />
                <div className='flex min-w-0 items-baseline gap-2'>
                  <span className='truncate text-[13px] font-semibold'>
                    {address.route}
                  </span>
                  {address.description && (
                    <span className='text-muted-foreground truncate text-[11px]'>
                      {address.description}
                    </span>
                  )}
                </div>
              </div>
              <code className='text-foreground/80 max-h-16 min-h-4 overflow-y-auto text-xs leading-4 break-all select-text max-sm:min-h-8 max-sm:text-[11px]'>
                {address.url}
              </code>
              <CopyButton
                value={address.url}
                variant='outline'
                size='sm'
                className='h-[30px] min-w-[65px] self-start rounded-[6px] text-xs'
                tooltip={t('Copy API URL')}
                successChildren={t('Copied')}
                aria-label={`${t('Copy API URL')}: ${address.url}`}
              >
                {t('Copy')}
              </CopyButton>
            </div>
          ))}
        </div>
      )}
    </section>
  )
}
