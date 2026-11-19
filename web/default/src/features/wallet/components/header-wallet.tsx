/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.
*/
import { Link } from '@tanstack/react-router'
import {
  ArrowUpRight,
  ChevronDown,
  Eye,
  EyeOff,
  RefreshCw,
  Wallet as WalletIcon,
} from 'lucide-react'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  Popover,
  PopoverContent,
  PopoverHeader,
  PopoverTitle,
  PopoverTrigger,
} from '@/components/ui/popover'
import { useIsSidebarModuleVisible } from '@/hooks/use-sidebar-config'
import { formatQuota } from '@/lib/format'
import { cn } from '@/lib/utils'

import { useWalletBalance } from '../hooks/use-wallet-balance'

export function HeaderWallet() {
  const { t } = useTranslation()
  const visible = useIsSidebarModuleVisible('/wallet')
  const balance = useWalletBalance({ enabled: visible })
  const [balanceHidden, setBalanceHidden] = useState(false)
  const [open, setOpen] = useState(false)
  const openTimeout = useRef<number | undefined>(undefined)
  const closeTimeout = useRef<number | undefined>(undefined)

  const clearHoverTimeouts = () => {
    if (openTimeout.current !== undefined) {
      window.clearTimeout(openTimeout.current)
      openTimeout.current = undefined
    }
    if (closeTimeout.current !== undefined) {
      window.clearTimeout(closeTimeout.current)
      closeTimeout.current = undefined
    }
  }

  const scheduleOpen = () => {
    clearHoverTimeouts()
    if (open) return
    openTimeout.current = window.setTimeout(() => setOpen(true), 75)
  }

  const scheduleClose = () => {
    clearHoverTimeouts()
    closeTimeout.current = window.setTimeout(() => setOpen(false), 140)
  }

  if (!visible) return null

  const quota = balance.data?.quota
  const amount = quota === undefined ? undefined : formatQuota(quota)
  const triggerValue = balanceHidden ? '••••' : (amount ?? t('Not available'))
  const triggerLabel = `${t('Wallet')}: ${balanceHidden ? t('Hide') : triggerValue}`
  const toggleLabel = balanceHidden ? t('Show') : t('Hide')
  const updatedAt = balance.dataUpdatedAt
    ? new Intl.DateTimeFormat(undefined, {
        hour: '2-digit',
        minute: '2-digit',
      }).format(balance.dataUpdatedAt)
    : undefined

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        type='button'
        openOnHover
        delay={75}
        closeDelay={140}
        aria-label={triggerLabel}
        title={triggerLabel}
        onMouseEnter={scheduleOpen}
        onMouseLeave={scheduleClose}
        className={cn(
          'group inline-flex min-h-9 min-w-9 items-center gap-1.5 rounded-xl border border-transparent px-2 text-sm transition-[background-color,border-color,box-shadow] duration-200',
          'hover:border-border/80 hover:bg-muted/70 data-pressed:border-border data-pressed:bg-muted/80',
          'focus-visible:ring-ring/50 focus-visible:ring-3 motion-reduce:transition-none'
        )}
      >
        <span className='bg-primary/10 text-primary grid size-6 shrink-0 place-items-center rounded-lg transition-transform duration-200 group-hover:scale-105 motion-reduce:transition-none'>
          <WalletIcon className='size-3.5' aria-hidden='true' />
        </span>
        <span className='hidden max-w-0 overflow-hidden text-xs font-medium whitespace-nowrap opacity-0 transition-[max-width,opacity] duration-200 group-hover:max-w-20 group-hover:opacity-100 motion-reduce:transition-none sm:inline'>
          {t('Wallet')}
        </span>
        <span className='font-semibold text-[#865A16] tabular-nums dark:text-[#F2C879]'>
          {triggerValue}
        </span>
        <ChevronDown
          className='text-muted-foreground size-3.5 transition-transform duration-200 group-data-pressed:rotate-180 data-[state=open]:rotate-180 motion-reduce:transition-none'
          aria-hidden='true'
        />
      </PopoverTrigger>
      <PopoverContent
        align='end'
        sideOffset={8}
        onMouseEnter={clearHoverTimeouts}
        onMouseLeave={scheduleClose}
        className='w-[min(21rem,calc(100vw-1rem))] gap-0 overflow-hidden rounded-2xl p-0'
      >
        <div className='from-primary/[0.08] via-background to-background bg-gradient-to-br p-4'>
          <PopoverHeader className='gap-1'>
            <PopoverTitle className='sr-only'>
              {t('Wallet quick actions')}
            </PopoverTitle>
            <span className='text-muted-foreground text-xs font-medium tracking-wide uppercase'>
              {t('Current Balance')}
            </span>
          </PopoverHeader>
          <div className='mt-2 flex items-center justify-between gap-3'>
            <strong className='min-w-0 truncate text-2xl font-semibold tracking-tight text-[#865A16] dark:text-[#F2C879]'>
              {balanceHidden ? '••••••' : (amount ?? t('Not available'))}
            </strong>
            <div className='flex shrink-0 items-center gap-1'>
              <button
                type='button'
                aria-label={t('Refresh')}
                title={t('Refresh')}
                className='text-muted-foreground hover:text-foreground hover:bg-muted focus-visible:ring-ring/50 inline-flex size-10 items-center justify-center rounded-xl transition-colors focus-visible:ring-3 motion-reduce:transition-none'
                onClick={() => void balance.refetch()}
              >
                <RefreshCw
                  className={cn(
                    'size-4',
                    balance.isFetching &&
                      'animate-spin motion-reduce:animate-none'
                  )}
                  aria-hidden='true'
                />
              </button>
              <button
                type='button'
                aria-label={toggleLabel}
                aria-pressed={balanceHidden}
                title={balanceHidden ? t('Show balance') : t('Hide balance')}
                className='text-muted-foreground hover:text-foreground hover:bg-muted focus-visible:ring-ring/50 inline-flex size-10 items-center justify-center rounded-xl transition-colors focus-visible:ring-3 motion-reduce:transition-none'
                onClick={() => setBalanceHidden((current) => !current)}
              >
                {balanceHidden ? (
                  <EyeOff className='size-4' aria-hidden='true' />
                ) : (
                  <Eye className='size-4' aria-hidden='true' />
                )}
              </button>
            </div>
          </div>
          {updatedAt && (
            <p className='text-muted-foreground mt-1 text-xs'>
              {t('Last updated')} {updatedAt}
            </p>
          )}
          {balance.error && (
            <div
              role='status'
              className='bg-destructive/10 text-destructive mt-3 flex items-center justify-between gap-3 rounded-xl px-3 py-2 text-xs'
            >
              <span>{t('Not available')}</span>
              <button
                type='button'
                className='font-medium underline underline-offset-2'
                onClick={() => void balance.refetch()}
              >
                {t('Retry balance')}
              </button>
            </div>
          )}
        </div>
        <div className='border-border/70 grid grid-cols-2 gap-2 border-t p-3'>
          <Link
            to='/wallet'
            className='text-foreground hover:bg-muted inline-flex min-h-11 items-center justify-center gap-1.5 rounded-xl px-3 text-sm font-medium transition-colors motion-reduce:transition-none'
          >
            {t('View wallet')}
            <ArrowUpRight className='size-3.5' aria-hidden='true' />
          </Link>
          <Link
            to='/wallet'
            hash='wallet-recharge-heading'
            className='bg-primary text-primary-foreground hover:bg-primary/90 inline-flex min-h-11 items-center justify-center gap-1.5 rounded-xl px-3 text-sm font-medium shadow-sm transition-colors motion-reduce:transition-none'
          >
            {t('Go to recharge')}
            <ArrowUpRight className='size-3.5 opacity-70' aria-hidden='true' />
          </Link>
        </div>
      </PopoverContent>
    </Popover>
  )
}
