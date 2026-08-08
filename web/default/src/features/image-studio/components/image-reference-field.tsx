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
import {
  ArrowDown,
  ArrowUp,
  ImagePlus,
  LoaderCircle,
  Trash2,
} from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { cn } from '@/lib/utils'

import type { ImageReferencesController } from '../hooks/use-image-references'

function ImageReferencePreview(props: { file: File; alt: string }) {
  const [previewUrl, setPreviewUrl] = useState<string | null>(null)

  useEffect(() => {
    const url = URL.createObjectURL(props.file)
    setPreviewUrl(url)
    return () => URL.revokeObjectURL(url)
  }, [props.file])

  if (!previewUrl) return null
  return (
    <img src={previewUrl} alt={props.alt} className='size-full object-cover' />
  )
}

export function ImageReferenceField(props: {
  controller: ImageReferencesController
  maxReferenceImages: number
  disabled: boolean
}) {
  const { t } = useTranslation()
  const inputId = 'image-studio-reference-images'
  const errorId = `${inputId}-error`
  const inputDisabled =
    props.disabled || props.controller.files.length >= props.maxReferenceImages
  const fileRows = props.controller.files.map((file, index) => ({
    file,
    id: props.controller.fileIds[index],
    index,
  }))

  return (
    <div className='space-y-2'>
      <div className='flex items-center justify-between gap-3'>
        <Label htmlFor={inputId}>{t('imageStudio.referenceImages')}</Label>
        <span className='text-muted-foreground text-xs tabular-nums'>
          {t('imageStudio.referenceCount', {
            count: props.controller.files.length,
            max: props.maxReferenceImages,
          })}
        </span>
      </div>
      {props.controller.files.length > 0 && (
        <ul className='space-y-2'>
          {fileRows.map((row) => (
            <li
              key={row.id}
              className='flex min-w-0 items-center gap-2 rounded-md border p-2'
            >
              <div className='bg-muted relative size-14 shrink-0 overflow-hidden rounded-sm'>
                <ImageReferencePreview
                  file={row.file}
                  alt={t('imageStudio.referencePreviewPosition', {
                    position: row.index + 1,
                  })}
                />
                <span className='bg-background/85 absolute start-1 top-1 min-w-5 rounded-sm px-1 text-center text-[11px] font-medium tabular-nums'>
                  {row.index + 1}
                </span>
              </div>
              <span
                className='min-w-0 flex-1 truncate text-sm'
                title={row.file.name}
              >
                {row.file.name}
              </span>
              <div className='flex shrink-0 items-center gap-0.5'>
                <Button
                  type='button'
                  size='icon-sm'
                  variant='ghost'
                  disabled={props.disabled || row.index === 0}
                  onClick={() => props.controller.move(row.index, -1)}
                  aria-label={t('imageStudio.moveReferenceUp', {
                    position: row.index + 1,
                  })}
                  title={t('imageStudio.moveReferenceUp', {
                    position: row.index + 1,
                  })}
                >
                  <ArrowUp aria-hidden='true' />
                </Button>
                <Button
                  type='button'
                  size='icon-sm'
                  variant='ghost'
                  disabled={
                    props.disabled ||
                    row.index === props.controller.files.length - 1
                  }
                  onClick={() => props.controller.move(row.index, 1)}
                  aria-label={t('imageStudio.moveReferenceDown', {
                    position: row.index + 1,
                  })}
                  title={t('imageStudio.moveReferenceDown', {
                    position: row.index + 1,
                  })}
                >
                  <ArrowDown aria-hidden='true' />
                </Button>
                <Button
                  type='button'
                  size='icon-sm'
                  variant='ghost'
                  disabled={props.disabled}
                  onClick={() => props.controller.remove(row.index)}
                  aria-label={t('imageStudio.removeReferenceImage', {
                    position: row.index + 1,
                  })}
                  title={t('imageStudio.removeReferenceImage', {
                    position: row.index + 1,
                  })}
                >
                  <Trash2 aria-hidden='true' />
                </Button>
              </div>
            </li>
          ))}
        </ul>
      )}
      <label
        htmlFor={inputId}
        className={cn(
          'hover:bg-muted/50 focus-within:border-ring focus-within:ring-ring/50 relative flex min-h-24 cursor-pointer items-center justify-center overflow-hidden rounded-md border border-dashed transition-colors focus-within:ring-[3px]',
          inputDisabled && 'cursor-not-allowed opacity-60'
        )}
        aria-disabled={inputDisabled}
      >
        <span className='text-muted-foreground flex flex-col items-center gap-2 p-4 text-sm'>
          <ImagePlus aria-hidden='true' />
          {props.controller.files.length > 0
            ? t('imageStudio.addReferenceImages')
            : t('imageStudio.chooseReferenceImages')}
        </span>
        {props.controller.processing && (
          <span className='bg-background/80 absolute inset-0 flex items-center justify-center'>
            <LoaderCircle
              className='animate-spin motion-reduce:animate-none'
              aria-label={t('imageStudio.processingReferenceImages')}
            />
          </span>
        )}
        <input
          id={inputId}
          type='file'
          className='sr-only'
          accept='image/jpeg,image/png,image/webp'
          multiple
          disabled={inputDisabled}
          aria-describedby={props.controller.error ? errorId : undefined}
          onChange={(event) => {
            const files = [...(event.target.files ?? [])]
            event.target.value = ''
            if (files.length > 0) void props.controller.select(files)
          }}
        />
      </label>
      {props.controller.error && (
        <p id={errorId} className='text-destructive text-sm' role='alert'>
          {props.controller.error}
        </p>
      )}
    </div>
  )
}
