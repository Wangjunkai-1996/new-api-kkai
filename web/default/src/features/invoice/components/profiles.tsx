import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Skeleton } from '@/components/ui/skeleton'
import { useAuthStore } from '@/stores/auth-store'

import {
  deleteInvoiceProfile,
  getInvoiceProfile,
  getInvoiceProfiles,
  invoiceKey,
  saveInvoiceProfile,
} from '../api'
import { useInvoiceDraft } from '../lib/draft'
import {
  applyInvoiceProfileErrors,
  emptyInvoiceProfile,
  invoiceProfileSchema,
} from '../lib/profile'
import { useInvoiceOperation } from '../lib/use-invoice-operation'
import type { InvoicePolicy, InvoiceProfile } from '../types'
import { InvoiceProfileFields } from './profile-form'
import { InvoiceEmpty, InvoiceError } from './shared'

export function InvoiceProfiles(props: { policy?: InvoicePolicy }) {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const { draft, setDraft } = useInvoiceDraft()
  const navigate = useNavigate()
  const profiles = useQuery({
    queryKey: [...invoiceKey(userId), 'profiles'],
    queryFn: getInvoiceProfiles,
    retry: false,
    gcTime: 0,
  })
  const [editing, setEditing] = useState<InvoiceProfile | null>(null)
  const [removing, setRemoving] = useState<InvoiceProfile | null>(null)
  const op = useInvoiceOperation()
  async function edit(profile: InvoiceProfile) {
    const result = await op.run(
      'profile-read',
      profile.id,
      () => getInvoiceProfile(profile.id || ''),
      { invalidate: false }
    )
    if (result) setEditing(result)
  }
  return (
    <div className='space-y-4'>
      <div className='flex items-center justify-between gap-3'>
        <div>
          <h2 className='text-base font-medium'>{t('Saved invoice titles')}</h2>
          <p className='text-muted-foreground mt-1 text-xs'>
            {t('Editing a saved title does not change submitted requests.')}
          </p>
        </div>
        <Button onClick={() => setEditing({ ...emptyInvoiceProfile })}>
          {t('New title')}
        </Button>
      </div>
      <InvoiceError
        error={profiles.error || op.error}
        onRetry={profiles.isError ? () => void profiles.refetch() : undefined}
      />
      {profiles.isPending && <Skeleton className='h-40' />}
      {profiles.data?.items.length === 0 && (
        <InvoiceEmpty>
          {t('Save an invoice title to use it in future requests.')}
        </InvoiceEmpty>
      )}
      <div className='grid gap-4 sm:grid-cols-2'>
        {profiles.data?.items.map((profile) => (
          <article key={profile.id} className='bg-card rounded-xl border p-5'>
            <p className='text-muted-foreground text-xs'>
              {profile.kind === 'company' ? t('Company') : t('Individual')}
            </p>
            <h3 className='mt-2 font-medium wrap-anywhere'>{profile.title}</h3>
            {profile.kind === 'company' && (
              <p className='text-muted-foreground mt-2 text-xs'>
                {t('Tax ID')}: {profile.tax_id}
              </p>
            )}
            <p className='text-muted-foreground mt-1 text-xs wrap-anywhere'>
              {profile.email}
            </p>
            <div className='mt-4 flex gap-2'>
              <Button
                size='sm'
                disabled={op.busy || !!draft.pendingCreate}
                onClick={async () => {
                  const value = await op.run(
                    'profile-read',
                    profile.id,
                    () => getInvoiceProfile(profile.id || ''),
                    { invalidate: false }
                  )
                  if (value) {
                    setDraft((previous) =>
                      previous.pendingCreate
                        ? previous
                        : {
                            ...previous,
                            profile: value,
                            step: previous.orders.length ? 2 : 1,
                            consent: false,
                          }
                    )
                    void navigate({ to: '/invoices/new' })
                  }
                }}
              >
                {t('Use this title')}
              </Button>
              <Button
                variant='outline'
                size='sm'
                disabled={op.busy}
                onClick={() => void edit(profile)}
              >
                {t('Edit')}
              </Button>
              <Button
                variant='ghost'
                size='sm'
                onClick={() => setRemoving(profile)}
              >
                {t('Delete')}
              </Button>
            </div>
          </article>
        ))}
      </div>
      {editing && (
        <InvoiceProfileEditor
          key={editing.id || 'new'}
          profile={editing}
          policy={props.policy}
          onClose={() => setEditing(null)}
        />
      )}
      <Dialog
        open={!!removing}
        onOpenChange={(open) => {
          if (!open && !op.busy) setRemoving(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('Delete saved invoice title?')}</DialogTitle>
            <DialogDescription>
              {t('Submitted requests keep their original invoice information.')}
            </DialogDescription>
          </DialogHeader>
          <InvoiceError error={op.error} />
          <DialogFooter>
            <Button
              variant='outline'
              disabled={op.busy}
              onClick={() => setRemoving(null)}
            >
              {t('Cancel')}
            </Button>
            <Button
              variant='destructive'
              disabled={op.busy}
              onClick={async () => {
                if (removing) {
                  const result = await op.run(
                    'profile-delete',
                    removing,
                    (key) =>
                      deleteInvoiceProfile(removing, key).then(() => true)
                  )
                  if (result) setRemoving(null)
                }
              }}
            >
              {t('Delete')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
function InvoiceProfileEditor(props: {
  profile: InvoiceProfile
  policy?: InvoicePolicy
  onClose: () => void
}) {
  const { t } = useTranslation()
  const form = useForm<InvoiceProfile>({
    resolver: zodResolver(invoiceProfileSchema),
    defaultValues: props.profile,
  })
  const op = useInvoiceOperation()
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !op.busy) props.onClose()
      }}
    >
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>
            {props.profile.id ? t('Edit invoice title') : t('New title')}
          </DialogTitle>
          <DialogDescription>
            {t('Your invoice information is stored privately.')}
          </DialogDescription>
        </DialogHeader>
        <form
          onSubmit={form.handleSubmit(async (input) => {
            const result = await op.run(
              'profile-save',
              input,
              (key) =>
                saveInvoiceProfile(
                  {
                    ...input,
                    id: props.profile.id,
                    version: props.profile.version,
                  },
                  key
                ),
              {
                onError: (error) => {
                  applyInvoiceProfileErrors(form, error)
                },
              }
            )
            if (result) props.onClose()
          })}
          className='space-y-4'
        >
          <fieldset disabled={op.busy}>
            <InvoiceProfileFields
              form={form}
              policy={props.policy}
              error={op.error}
            />
          </fieldset>
          <InvoiceError error={op.error} />
          <DialogFooter>
            <Button
              type='button'
              variant='outline'
              disabled={op.busy}
              onClick={props.onClose}
            >
              {t('Cancel')}
            </Button>
            <Button type='submit' disabled={op.busy}>
              {op.busy ? t('Saving...') : t('Save')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
