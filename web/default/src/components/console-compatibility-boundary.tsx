import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { useConsoleContract } from '@/hooks/use-console-contract'

export function ConsoleCompatibilityBoundary(props: { children: ReactNode }) {
  const { t } = useTranslation()
  const console = useConsoleContract()
  const blocked = console.required && !console.compatible
  return (
    <>
      <div inert={blocked}>{props.children}</div>
      {blocked && (
        <div
          className='bg-background/95 fixed inset-0 z-50 flex flex-col items-center justify-center gap-4 p-6'
          role={console.loading ? 'status' : 'alert'}
          aria-busy={console.loading}
        >
          <p>
            {console.loading
              ? t('Loading...')
              : t(
                  'The page is temporarily unavailable. Your edits are preserved. Please retry shortly.'
                )}
          </p>
          {!console.loading && (
            <Button onClick={() => void console.retry()}>{t('Retry')}</Button>
          )}
        </div>
      )}
    </>
  )
}
