import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useId, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import type { SystemStatus } from '@/features/auth/types'
import { updateSystemOption } from '@/features/system-settings/api'
import { ROLE } from '@/lib/roles'
import {
  getServerErrorMessage,
  requireServerSuccess,
} from '@/lib/server-error-message'
import { statusQueryOptions } from '@/lib/status-query'
import { useAuthStore } from '@/stores/auth-store'

export function VideoMultimodalSetting() {
  const isRoot = useAuthStore(
    (state) => state.auth.user?.role === ROLE.SUPER_ADMIN
  )
  return isRoot ? <VideoMultimodalControl /> : null
}

function VideoMultimodalControl() {
  const { t } = useTranslation()
  const id = useId()
  const queryClient = useQueryClient()
  const [pending, setPending] = useState<{
    enabled: boolean
    savedAt: number
    timedOut?: boolean
  } | null>(null)
  const statusQuery = useQuery({
    ...statusQueryOptions,
    staleTime: 0,
    refetchOnMount: 'always',
    retry: false,
    refetchIntervalInBackground: true,
    refetchInterval: (query) => {
      if (!pending || pending.timedOut) return false
      const status = query.state.data as SystemStatus | null | undefined
      // The handling instance updates immediately; allow service synchronization first.
      if (
        query.state.status === 'success' &&
        query.state.dataUpdatedAt >= pending.savedAt + 60_000 &&
        status?.video_studio?.multimodal_enabled === pending.enabled
      ) {
        return false
      }
      return 3000
    },
  })
  const actualEnabled = (statusQuery.data as SystemStatus | null | undefined)
    ?.video_studio?.multimodal_enabled
  const known =
    statusQuery.isFetchedAfterMount && typeof actualEnabled === 'boolean'
  const confirmed = Boolean(
    pending &&
    !statusQuery.isError &&
    statusQuery.dataUpdatedAt >= pending.savedAt + 60_000 &&
    actualEnabled === pending.enabled
  )
  // Retire this write so later changes by another administrator stay editable.
  if (confirmed) setPending(null)
  const awaitingSync = Boolean(pending && !confirmed)
  useEffect(() => {
    if (!pending || confirmed || pending.timedOut) return
    const timeout = window.setTimeout(
      () => setPending((current) => current && { ...current, timedOut: true }),
      Math.max(0, pending.savedAt + 75_000 - Date.now())
    )
    return () => window.clearTimeout(timeout)
  }, [pending, confirmed])

  const mutation = useMutation({
    mutationFn: async (enabled: boolean) =>
      requireServerSuccess(
        await updateSystemOption({
          key: 'video_studio.multimodal_enabled',
          value: String(enabled),
        })
      ),
    onMutate: () => setPending(null),
    onSuccess: (_data, enabled) => {
      setPending({ enabled, savedAt: Date.now() })
      void queryClient.invalidateQueries({ queryKey: ['system-options'] })
      void statusQuery.refetch({ cancelRefetch: false })
    },
  })
  let message = t('Loading')
  if (known) message = actualEnabled ? t('Enabled') : t('Disabled')
  if (statusQuery.isError || (!statusQuery.isFetching && !known)) {
    message = t('Loading failed')
  }
  if (mutation.isPending) message = t('Saving...')
  if (awaitingSync) {
    message = pending?.timedOut
      ? t(
          'Saved, but the running service has not confirmed this setting. Refresh the status before trying again.'
        )
      : t('Saved. Waiting for the running service to apply this setting...')
  }

  return (
    <section className='mb-4 flex items-start justify-between gap-4 rounded-lg border p-4'>
      <div className='space-y-2'>
        <Label htmlFor={id}>{t('Multimodal video references')}</Label>
        <p id={`${id}-description`} className='text-muted-foreground text-xs'>
          {t(
            'Enable only after the adapter and both the current and rollback versions support multimodal references.'
          )}
        </p>
        <p className='text-muted-foreground text-xs' role='status'>
          {message}
        </p>
        {mutation.isError && (
          <p className='text-destructive text-xs' role='alert'>
            {getServerErrorMessage(
              mutation.error,
              t('Failed to update setting')
            )}
          </p>
        )}
      </div>
      <div className='flex shrink-0 items-center gap-3'>
        <Button
          type='button'
          size='sm'
          variant='ghost'
          disabled={statusQuery.isFetching || mutation.isPending}
          onClick={async () => {
            const refreshed = await statusQuery.refetch({
              cancelRefetch: false,
            })
            const status = refreshed.data as SystemStatus | null | undefined
            if (
              pending?.timedOut &&
              !refreshed.isError &&
              typeof status?.video_studio?.multimodal_enabled === 'boolean'
            ) {
              setPending(null)
            }
          }}
        >
          {t('Refresh')}
        </Button>
        <Switch
          id={id}
          aria-describedby={`${id}-description`}
          checked={actualEnabled === true}
          disabled={
            !known || statusQuery.isError || mutation.isPending || awaitingSync
          }
          onCheckedChange={(enabled) => mutation.mutate(enabled)}
        />
      </div>
    </section>
  )
}
