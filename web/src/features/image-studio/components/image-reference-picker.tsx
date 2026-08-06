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
import { ImagePlus, LoaderCircle, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { cn } from '@/lib/utils'

export function ImageReferencePicker(props: {
  file: File | null
  processing: boolean
  disabled: boolean
  error: string | null
  onSelect: (file: File) => void
  onClear: () => void
}) {
  const { t } = useTranslation()
  const [previewUrl, setPreviewUrl] = useState<string>()
  const inputId = 'image-studio-reference-image'

  useEffect(() => {
    if (!props.file) {
      setPreviewUrl(undefined)
      return
    }
    const url = URL.createObjectURL(props.file)
    setPreviewUrl(url)
    return () => URL.revokeObjectURL(url)
  }, [props.file])

  return (
    <div className='space-y-2'>
      <div className='flex items-center justify-between gap-3'>
        <Label htmlFor={inputId}>{t('imageStudio.referenceImage')}</Label>
        {props.file && (
          <Button
            type='button'
            size='icon-sm'
            variant='ghost'
            disabled={props.disabled}
            onClick={props.onClear}
            aria-label={t('imageStudio.removeReferenceImage')}
          >
            <Trash2 aria-hidden='true' />
          </Button>
        )}
      </div>
      <label
        htmlFor={inputId}
        className={cn(
          'hover:bg-muted/50 focus-within:border-ring focus-within:ring-ring/50 relative flex min-h-36 cursor-pointer items-center justify-center overflow-hidden rounded-md border border-dashed transition-colors focus-within:ring-[3px]',
          props.disabled && 'cursor-not-allowed opacity-60'
        )}
        aria-disabled={props.disabled}
      >
        {previewUrl ? (
          <img
            src={previewUrl}
            alt={t('imageStudio.referencePreview')}
            className='max-h-72 w-full object-contain'
          />
        ) : (
          <span className='text-muted-foreground flex flex-col items-center gap-2 p-4 text-sm'>
            <ImagePlus aria-hidden='true' />
            {t('imageStudio.chooseReferenceImage')}
          </span>
        )}
        {props.processing && (
          <span className='bg-background/80 absolute inset-0 flex items-center justify-center'>
            <LoaderCircle
              className='animate-spin motion-reduce:animate-none'
              aria-label={t('imageStudio.processingReferenceImage')}
            />
          </span>
        )}
        <input
          id={inputId}
          type='file'
          className='sr-only'
          accept='image/jpeg,image/png,image/webp'
          disabled={props.disabled}
          onChange={(event) => {
            const file = event.target.files?.[0]
            event.target.value = ''
            if (file) props.onSelect(file)
          }}
        />
      </label>
      {props.file && (
        <p className='text-muted-foreground truncate text-xs'>
          {props.file.name}
        </p>
      )}
      {props.error && (
        <p className='text-destructive text-sm' role='alert'>
          {props.error}
        </p>
      )}
    </div>
  )
}
