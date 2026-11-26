import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { api } from '@/lib/api'
import { privateUserQueryKey } from '@/lib/private-query-cache'
import { useAuthStore } from '@/stores/auth-store'

type Endpoint = {
  id: string
  state: 'pending' | 'active' | 'retired'
  url: string
}
type Delivery = {
  task_id: string
  event_id: string
  state: string
  delivery_status?: string
  last_error?: string
}

export function VideoWebhookDialog({
  tokenId,
  close,
}: {
  tokenId: number
  close: () => void
}) {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id ?? 0)
  const [url, setUrl] = useState('')
  const [issued, setIssued] = useState<{ id: string; secret: string } | null>(
    null
  )
  const [busy, setBusy] = useState(false)
  const [revoking, setRevoking] = useState<string | null>(null)
  const [error, setError] = useState('')
  const base = `/api/token/${tokenId}/video-webhook`
  const stateLabel = {
    active: t('Active'),
    pending: t('Pending'),
    retired: t('Existing tasks only'),
  }
  const query = useQuery({
    queryKey: privateUserQueryKey(userId, 'api-keys', 'video-webhook', tokenId),
    queryFn: async () => {
      const [endpoints, deliveries] = await Promise.all([
        api.get<{ data: Endpoint[] }>(base),
        api.get<{ data: Delivery[] }>(`${base}/deliveries`),
      ])
      return {
        endpoints: endpoints.data.data,
        deliveries: deliveries.data.data,
      }
    },
    gcTime: 0,
    retry: false,
  })

  async function change(action: 'create' | 'verify' | 'revoke', id?: string) {
    setBusy(true)
    setError('')
    try {
      if (action === 'create') {
        const response = await api.put<{
          data: { endpoint: Endpoint; secret: string }
        }>(base, { url })
        setIssued({
          id: response.data.data.endpoint.id,
          secret: response.data.data.secret,
        })
        setUrl('')
      } else if (action === 'verify') {
        await api.post(`${base}/verify`, { id })
      } else {
        await api.delete(`${base}/${id}`)
        if (issued?.id === id) setIssued(null)
        setRevoking(null)
      }
      await query.refetch()
    } catch {
      setError(
        action === 'verify'
          ? t('Check the signature and challenge response at your endpoint.')
          : t('Failed to save')
      )
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog
      open
      title={t('Seedance 2.5 webhook')}
      onOpenChange={(open) => {
        if (!open && !busy) close()
      }}
      bodyClassName='space-y-4'
    >
      <form
        onSubmit={(e) => {
          e.preventDefault()
          void change('create')
        }}
        className='space-y-2'
      >
        <Label htmlFor='video-webhook-url'>{t('Webhook URL')}</Label>
        <div className='flex gap-2'>
          <Input
            id='video-webhook-url'
            type='url'
            required
            value={url}
            maxLength={2048}
            placeholder='https://example.com/video'
            onChange={(e) => setUrl(e.target.value)}
          />
          <Button type='submit' disabled={busy || !url.trim()}>
            {t('Create')}
          </Button>
        </div>
      </form>
      {issued && (
        <div role='status' className='space-y-2'>
          <Label htmlFor='webhook-key'>
            {t('Save this signing secret now. It is shown only once.')}
          </Label>
          <code className='block break-all'>{issued.id}</code>
          <Input
            id='webhook-key'
            readOnly
            value={issued.secret}
            onFocus={(e) => e.target.select()}
          />
        </div>
      )}
      {error && (
        <p role='alert' className='text-destructive'>
          {error}
        </p>
      )}
      {query.isPending && <p>{t('Loading...')}</p>}
      {query.isError && <p role='alert'>{t('Failed to load data')}</p>}
      <ul className='space-y-2'>
        {query.data?.endpoints.map((ep) => (
          <li key={ep.id} className='rounded-md border p-2'>
            <p className='break-all'>{ep.url}</p>
            <p className='text-xs break-all'>
              {ep.id} · {stateLabel[ep.state]}
            </p>
            <div className='mt-2 flex gap-2'>
              {ep.state === 'pending' && (
                <Button
                  size='sm'
                  disabled={busy}
                  onClick={() => void change('verify', ep.id)}
                >
                  {t('Verify')}
                </Button>
              )}
              {revoking === ep.id ? (
                <>
                  <Button
                    variant='destructive'
                    size='sm'
                    disabled={busy}
                    onClick={() => void change('revoke', ep.id)}
                  >
                    {t('Confirm')}
                  </Button>
                  <Button
                    size='sm'
                    variant='ghost'
                    onClick={() => setRevoking(null)}
                  >
                    {t('Cancel')}
                  </Button>
                </>
              ) : (
                <Button
                  variant='outline'
                  size='sm'
                  disabled={busy}
                  onClick={() => setRevoking(ep.id)}
                >
                  {t('Revoke')}
                </Button>
              )}
            </div>
          </li>
        ))}
      </ul>
      <div className='flex items-center justify-between'>
        <h3>{t('Recent webhook deliveries')}</h3>
        <Button
          size='sm'
          variant='outline'
          disabled={query.isFetching || busy}
          onClick={() => void query.refetch()}
        >
          {t('Refresh')}
        </Button>
      </div>
      {query.data?.deliveries.length === 0 && <p>{t('No records')}</p>}
      <ul className='space-y-2'>
        {query.data?.deliveries.map((row) => (
          <li key={row.event_id} className='border-t pt-2 text-sm break-all'>
            <p>{row.task_id}</p>
            <code>{row.event_id}</code>
            <p>
              {t('Status')} · {row.delivery_status || row.state}{' '}
              {row.last_error}
            </p>
          </li>
        ))}
      </ul>
    </Dialog>
  )
}
