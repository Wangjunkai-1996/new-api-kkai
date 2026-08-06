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
import { useQuery } from '@tanstack/react-query'
import { Copy, QrCode, Users } from 'lucide-react'
import { QRCodeSVG } from 'qrcode.react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'

import { getMyInvitationCode } from '../../api'
import { requireInvitationData } from '../../api/result'
import { formatRebateAmount } from '../../format'

export const InvitationSummary = (props: { showRebates: boolean }) => {
  const { t } = useTranslation()
  const [showQrCode, setShowQrCode] = useState(false)
  const query = useQuery({
    queryKey: ['kkai', 'invitations', 'my-code'],
    queryFn: async () =>
      requireInvitationData(
        await getMyInvitationCode(),
        'Failed to load invitation code'
      ),
  })
  const invitationLink = useMemo(() => {
    if (!query.data?.invitationCode) return ''
    return `${window.location.origin}/?aff=${query.data.invitationCode}`
  }, [query.data?.invitationCode])

  const copy = async (value: string, message: string) => {
    try {
      await navigator.clipboard.writeText(value)
      toast.success(message)
    } catch {
      toast.error(t('Copy failed'))
    }
  }

  if (query.isPending) return <InvitationSummarySkeleton />
  if (query.isError || !query.data) {
    return (
      <ErrorState
        title={t('Failed to load invitation code')}
        description={query.error?.message}
        onRetry={() => void query.refetch()}
      />
    )
  }

  const stats = query.data
  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>{t('My Invitation')}</CardTitle>
        </CardHeader>
        <CardContent className='space-y-6'>
          <div className='grid gap-3 lg:grid-cols-[minmax(0,20rem)_minmax(0,1fr)]'>
            <div className='flex min-w-0 items-center gap-2'>
              <div className='bg-muted min-w-0 flex-1 rounded-md border px-4 py-3 font-mono text-xl font-semibold break-all'>
                {stats.invitationCode}
              </div>
              <Button
                type='button'
                size='icon'
                variant='outline'
                aria-label={t('Copy invitation code')}
                title={t('Copy invitation code')}
                onClick={() =>
                  void copy(stats.invitationCode, t('Invitation code copied'))
                }
              >
                <Copy />
              </Button>
              <Button
                type='button'
                size='icon'
                variant='outline'
                aria-label={t('Show invitation QR code')}
                title={t('Show invitation QR code')}
                onClick={() => setShowQrCode(true)}
              >
                <QrCode />
              </Button>
            </div>
            <div className='flex min-w-0 items-center gap-2'>
              <Input value={invitationLink} readOnly className='font-mono' />
              <Button
                type='button'
                size='icon'
                variant='outline'
                aria-label={t('Copy invitation link')}
                title={t('Copy invitation link')}
                onClick={() =>
                  void copy(invitationLink, t('Invitation link copied'))
                }
              >
                <Copy />
              </Button>
            </div>
          </div>
          <div className='grid divide-y border-y sm:grid-cols-2 sm:divide-x sm:divide-y-0 lg:grid-cols-5'>
            <SummaryValue
              label={t('Invited Users')}
              value={String(stats.invitedCount)}
              icon={Users}
            />
            {props.showRebates && (
              <>
                <SummaryValue
                  label={t('Confirming Rebate')}
                  value={formatRebateAmount(stats.confirmingRebate)}
                />
                <SummaryValue
                  label={t('Pending Rebate')}
                  value={formatRebateAmount(stats.pendingRebate)}
                />
                <SummaryValue
                  label={t('Completed Rebate')}
                  value={formatRebateAmount(stats.completedRebate)}
                />
                <SummaryValue
                  label={t('Total Rebate')}
                  value={formatRebateAmount(stats.totalRebate)}
                />
              </>
            )}
          </div>
        </CardContent>
      </Card>
      <Dialog open={showQrCode} onOpenChange={setShowQrCode}>
        <DialogContent className='sm:max-w-sm'>
          <DialogHeader>
            <DialogTitle>{t('Invitation QR Code')}</DialogTitle>
          </DialogHeader>
          <div className='flex justify-center py-4'>
            <div className='rounded-md border bg-white p-4'>
              <QRCodeSVG value={invitationLink} size={220} level='H' />
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}

const SummaryValue = (props: {
  label: string
  value: string
  icon?: typeof Users
}) => (
  <div className='min-w-0 px-4 py-3'>
    <div className='text-muted-foreground flex items-center gap-2 text-sm'>
      {props.icon && <props.icon className='size-4' aria-hidden='true' />}
      {props.label}
    </div>
    <div className='mt-1 truncate text-xl font-semibold tabular-nums'>
      {props.value}
    </div>
  </div>
)

const InvitationSummarySkeleton = () => (
  <Card>
    <CardHeader>
      <Skeleton className='h-5 w-32' />
    </CardHeader>
    <CardContent className='space-y-5'>
      <Skeleton className='h-12 w-full' />
      <Skeleton className='h-20 w-full' />
    </CardContent>
  </Card>
)
