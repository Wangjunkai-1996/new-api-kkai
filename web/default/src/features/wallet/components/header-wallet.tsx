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
  ArrowRight,
  ChevronDown,
  Eye,
  EyeOff,
  Plus,
  RefreshCw,
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

import '@/styles/header-wallet.css'

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
        className='header-wallet-trigger group focus-visible:ring-ring focus-visible:ring-offset-background inline-flex h-9 max-w-32 min-w-9 items-center gap-2 rounded-[10px] border px-1.5 transition-colors focus-visible:ring-2 focus-visible:ring-offset-2 motion-reduce:transition-none min-[420px]:max-w-52 min-[420px]:px-2 sm:ps-2.5'
      >
        <span
          className='header-wallet-mark hidden shrink-0 min-[420px]:block'
          aria-hidden='true'
        />
        <span className='header-wallet-sparks' aria-hidden='true' />
        <span className='grid min-w-0 gap-0.5 text-start'>
          <span className='text-muted-foreground truncate text-[10px] leading-none font-medium tracking-wider motion-safe:max-[420px]:pe-5'>
            {t('Wallet')}
          </span>
          <span className='text-foreground min-[360px]:font-public-sans truncate text-xs leading-none font-semibold tracking-[-0.02em] tabular-nums min-[360px]:text-[13px]'>
            {triggerValue}
          </span>
        </span>
        <ChevronDown
          className='text-muted-foreground hidden size-3 shrink-0 transition-transform group-aria-expanded:rotate-180 motion-reduce:transition-none sm:block'
          aria-hidden='true'
        />
      </PopoverTrigger>
      <PopoverContent
        align='end'
        sideOffset={8}
        onMouseEnter={clearHoverTimeouts}
        onMouseLeave={scheduleClose}
        className='w-[min(21rem,calc(100vw-1rem))] gap-0 overflow-hidden rounded-2xl p-0 motion-reduce:animate-none!'
      >
        <div className='header-wallet-summary px-5 pt-3 pb-5'>
          <span className='header-wallet-sparks' aria-hidden='true' />
          <PopoverHeader className='flex-row items-center justify-between gap-2'>
            <PopoverTitle className='sr-only'>
              {t('Wallet quick actions')}
            </PopoverTitle>
            <span className='text-muted-foreground text-xs font-medium tracking-wide'>
              {t('Current Balance')}
            </span>
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
          </PopoverHeader>
          <strong className='font-public-sans text-popover-foreground mt-1 block text-3xl leading-tight font-semibold tracking-[-0.035em] wrap-anywhere tabular-nums'>
            {balanceHidden ? '••••••' : (amount ?? t('Not available'))}
          </strong>
          {updatedAt && (
            <p className='text-muted-foreground mt-2 text-xs tabular-nums'>
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
            <ArrowRight className='size-3.5' aria-hidden='true' />
          </Link>
          <Link
            to='/wallet'
            hash='wallet-recharge-heading'
            className='bg-foreground text-background hover:bg-foreground/85 inline-flex min-h-11 items-center justify-center gap-1.5 rounded-xl px-3 text-sm font-medium transition-colors motion-reduce:transition-none'
          >
            <Plus className='size-3.5' aria-hidden='true' />
            {t('Go to recharge')}
          </Link>
        </div>
      </PopoverContent>
    </Popover>
  )
}
