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
import { createFileRoute } from '@tanstack/react-router'
import { BookOpen, RefreshCw, ShieldAlert } from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

export const Route = createFileRoute('/cn-restricted')({
  component: MainlandRestrictedNotice,
})

type RecheckStatus = 'idle' | 'checking' | 'restricted' | 'failed'

export function isAccessProbeAllowed(response: Response): boolean {
  return (
    response.type !== 'opaqueredirect' &&
    response.status >= 200 &&
    response.status < 300
  )
}

function MainlandRestrictedNotice() {
  const { t } = useTranslation()
  const [status, setStatus] = useState<RecheckStatus>('idle')

  const recheckAccess = useCallback(
    async (manual = false) => {
      if (manual) {
        setStatus('checking')
      }

      try {
        const response = await fetch(`/?kkai_access_probe=${Date.now()}`, {
          cache: 'no-store',
          credentials: 'same-origin',
          redirect: 'manual',
        })

        if (isAccessProbeAllowed(response)) {
          // A full document navigation is required so Cloudflare evaluates the current IP again.
          window.location.replace('/')
          return
        }
      } catch {
        if (manual) {
          setStatus('failed')
        }
        return
      }

      if (manual) {
        setStatus('restricted')
      }
    },
    [setStatus]
  )

  useEffect(() => {
    document.title = t('Access Notice - KKAI')

    const recheckTimer = window.setTimeout(() => {
      void recheckAccess(false)
    }, 600)
    const interval = window.setInterval(() => {
      void recheckAccess(false)
    }, 8000)

    return () => {
      window.clearTimeout(recheckTimer)
      window.clearInterval(interval)
    }
  }, [recheckAccess, t])

  let statusText = ''
  if (status === 'checking') {
    statusText = t('Checking access...')
  }
  if (status === 'restricted') {
    statusText = t('Access is still restricted from this IP.')
  }
  if (status === 'failed') {
    statusText = t('Unable to recheck right now.')
  }

  return (
    <main className='bg-background text-foreground min-h-svh overflow-hidden'>
      <section className='relative mx-auto flex min-h-svh w-full max-w-3xl items-center px-5 py-10 sm:px-8'>
        <div className='border-border/70 bg-card/90 w-full rounded-lg border p-6 shadow-2xl shadow-black/10 backdrop-blur sm:p-9'>
          <div className='text-muted-foreground flex items-center gap-3 text-xs font-semibold tracking-normal uppercase'>
            <span className='grid size-9 place-items-center rounded-lg bg-teal-400 text-sm font-black text-slate-950'>
              K
            </span>
            <span>KKAI</span>
          </div>

          <div className='mt-8 flex items-start gap-4'>
            <div className='hidden size-12 shrink-0 place-items-center rounded-lg border border-amber-300/35 bg-amber-300/10 text-amber-300 sm:grid'>
              <ShieldAlert className='size-6' aria-hidden='true' />
            </div>
            <div className='min-w-0'>
              <h1 className='text-4xl leading-none font-bold tracking-normal sm:text-5xl'>
                {t('Web Console Unavailable')}
              </h1>
              <p className='text-muted-foreground mt-5 max-w-2xl text-base leading-7'>
                {t(
                  'The KKAI web console is currently unavailable from mainland China IP addresses. If you recently changed networks or enabled a proxy, this page will recheck your access automatically.'
                )}
              </p>
            </div>
          </div>

          <div className='border-border/70 bg-muted/30 mt-7 grid gap-3 rounded-lg border p-4 text-sm leading-6'>
            <p>
              <strong>{t('API traffic is not affected.')}</strong>{' '}
              {t(
                'OpenAI-compatible requests under /v1 can continue to use your existing base URL and keys.'
              )}
            </p>
            <p className='text-muted-foreground'>
              {t(
                'If access is still restricted after switching networks, please contact support and include your current public IP location.'
              )}
            </p>
          </div>

          <div className='mt-7 flex flex-col gap-3 sm:flex-row'>
            <Button
              className='h-10 gap-2'
              size='lg'
              onClick={() => void recheckAccess(true)}
            >
              <RefreshCw
                className={cn(
                  'size-4',
                  status === 'checking' && 'animate-spin'
                )}
                aria-hidden='true'
              />
              {t('Recheck Access')}
            </Button>
            <Button
              className='h-10 gap-2'
              variant='outline'
              size='lg'
              render={<a href='/docs/' />}
            >
              <BookOpen className='size-4' aria-hidden='true' />
              {t('Open API Docs')}
            </Button>
          </div>

          <div
            className='mt-4 min-h-5 text-sm font-medium text-teal-500'
            aria-live='polite'
          >
            {statusText}
          </div>
        </div>
      </section>
    </main>
  )
}
