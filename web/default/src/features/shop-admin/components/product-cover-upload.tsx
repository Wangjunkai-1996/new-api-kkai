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
import { Upload01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { ShopError, shopRequest } from '@/features/shop/api'
import { productImage } from '@/features/shop/assets'
import { useShopIdentity } from '@/features/shop/hooks/use-shop'
import { coverSchema } from '@/features/shop/schemas'

export function ProductCoverUpload(props: {
  value: string
  onChange: (url: string) => void
  onBusy: (busy: boolean) => void
}) {
  const { t } = useTranslation()
  const identity = useShopIdentity()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const active = useRef<AbortController | null>(null)
  useEffect(
    () => () => {
      active.current?.abort()
    },
    [identity]
  )

  async function upload(file: File | undefined) {
    if (!file || active.current) return
    setError('')
    if (
      !file.size ||
      file.size > 5 * 1024 * 1024 ||
      !['image/jpeg', 'image/png', 'image/webp'].includes(file.type)
    ) {
      setError(t('Choose a JPEG, PNG or WebP image up to 5 MiB.'))
      return
    }
    const controller = new AbortController()
    active.current = controller
    setBusy(true)
    props.onBusy(true)
    try {
      const body = new FormData()
      body.append('file', file)
      const cover = await shopRequest('/api/admin/shop/covers', coverSchema, {
        method: 'POST',
        body,
        signal: controller.signal,
      })
      if (!controller.signal.aborted) props.onChange(cover.url)
    } catch (cause) {
      if (controller.signal.aborted) return
      if (
        cause instanceof ShopError &&
        cause.code === 'SHOP_COVER_STORAGE_UNAVAILABLE'
      ) {
        setError(t('Image storage is unavailable. Try again later.'))
      } else if (
        cause instanceof ShopError &&
        ['SHOP_COVER_INVALID', 'SHOP_COVER_TOO_LARGE'].includes(cause.code)
      ) {
        setError(
          t(
            'Use a valid JPEG, PNG or WebP image up to 5 MiB and 4096 pixels per side.'
          )
        )
      } else {
        setError(
          t('Cover upload failed. Your previous cover is unchanged. Try again.')
        )
      }
    } finally {
      active.current = null
      setBusy(false)
      props.onBusy(false)
    }
  }

  const preview = productImage(props.value)
  return (
    <div className='mt-5 grid gap-4 sm:grid-cols-[160px_1fr]'>
      <div className='bg-muted flex aspect-square items-center justify-center overflow-hidden rounded-xl border'>
        {preview ? (
          <img
            src={preview}
            alt={t('Cover preview')}
            width={160}
            height={160}
            className='h-full w-full object-cover'
          />
        ) : (
          <HugeiconsIcon
            icon={Upload01Icon}
            size={32}
            className='text-muted-foreground'
            aria-hidden='true'
          />
        )}
      </div>
      <div className='flex flex-col justify-center gap-2'>
        <label htmlFor='shop-cover-file' className='text-sm font-medium'>
          {t('Cover image')}
        </label>
        <p className='text-muted-foreground text-sm'>
          {t('JPEG, PNG or WebP · up to 5 MiB · recommended square image')}
        </p>
        <input
          id='shop-cover-file'
          type='file'
          accept='image/jpeg,image/png,image/webp'
          disabled={busy}
          aria-describedby='shop-cover-visibility'
          className='w-full min-w-0 text-sm file:mr-3 file:rounded-md file:border file:px-3 file:py-2'
          onChange={(event) => {
            void upload(event.target.files?.[0])
            event.target.value = ''
          }}
        />
        <p id='shop-cover-visibility' className='text-muted-foreground text-xs'>
          {t(
            'Cover images are public. Do not upload account credentials or redemption codes.'
          )}
        </p>
        {busy && (
          <p role='status' className='text-sm'>
            {t('Uploading…')}
          </p>
        )}
        {error && (
          <p role='alert' className='text-destructive text-sm'>
            {error}
          </p>
        )}
        {props.value && (
          <Button
            type='button'
            variant='outline'
            className='w-fit'
            disabled={busy}
            onClick={() => props.onChange('')}
          >
            {t('Remove cover')}
          </Button>
        )}
      </div>
    </div>
  )
}
