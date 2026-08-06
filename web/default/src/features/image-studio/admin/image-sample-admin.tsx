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
import { zodResolver } from '@hookform/resolvers/zod'
import { ImagePlus, LoaderCircle, Plus, Trash2 } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogMedia,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'

import {
  useAdminImageModels,
  useAdminImageSamples,
  useDeleteAdminImageSample,
  useSaveAdminImageSample,
  useUploadAdminImageAsset,
} from '../queries'
import type { ImageAsset, ImageSample } from '../types'
import {
  createImageSampleFormValues,
  imageSampleFormSchema,
  parseImageSampleForm,
  type ImageSampleFormValues,
} from './image-admin-forms'
import { ImageAdminWorkspace } from './image-admin-workspace'
import { ImageSampleParameterFields } from './image-sample-parameter-fields'

export function ImageSampleAdmin() {
  const { t } = useTranslation()
  const modelsQuery = useAdminImageModels()
  const samplesQuery = useAdminImageSamples()
  const saveMutation = useSaveAdminImageSample()
  const uploadMutation = useUploadAdminImageAsset()
  const deleteMutation = useDeleteAdminImageSample()
  const [selected, setSelected] = useState<ImageSample | null>(null)
  const [creating, setCreating] = useState(false)
  const [asset, setAsset] = useState<ImageAsset | null>(null)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const form = useForm<ImageSampleFormValues>({
    resolver: zodResolver(imageSampleFormSchema),
    defaultValues: createImageSampleFormValues(),
  })
  const samples = useMemo(
    () => samplesQuery.data?.pages.flatMap((page) => page.items) ?? [],
    [samplesQuery.data]
  )
  const profileId = form.watch('model_profile_id')
  const profile = modelsQuery.data?.find((model) => model.id === profileId)

  useEffect(() => {
    if (!selected) return
    const selectedProfile = modelsQuery.data?.find(
      (model) => model.id === selected.model_profile_id
    )
    form.reset(createImageSampleFormValues(selected, selectedProfile))
    setAsset(selected.asset)
  }, [form, modelsQuery.data, selected])

  const startCreate = (): void => {
    const firstProfile = modelsQuery.data?.[0]
    setSelected(null)
    setCreating(true)
    setAsset(null)
    form.reset(createImageSampleFormValues(undefined, firstProfile))
  }

  const selectProfile = (nextId: number): void => {
    const next = modelsQuery.data?.find((model) => model.id === nextId)
    if (!next) return
    form.setValue('model_profile_id', next.id)
    form.setValue('parameters', { ...next.default_parameters })
  }

  const upload = async (file?: File): Promise<void> => {
    if (!file) return
    try {
      const uploaded = await uploadMutation.mutateAsync(file)
      setAsset(uploaded)
      form.setValue('image_asset_id', uploaded.id, { shouldValidate: true })
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('imageStudio.admin.uploadFailed')
      )
    }
  }

  const submit = form.handleSubmit(async (values) => {
    try {
      const saved = await saveMutation.mutateAsync({
        id: selected?.id,
        values: parseImageSampleForm(values),
      })
      setSelected(saved)
      setCreating(false)
      setAsset(saved.asset)
      toast.success(t('imageStudio.admin.saved'))
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('imageStudio.admin.saveFailed')
      )
    }
  })

  const remove = async (): Promise<void> => {
    if (!selected) return
    try {
      await deleteMutation.mutateAsync(selected.id)
      setSelected(null)
      setAsset(null)
      setCreating(false)
      setDeleteOpen(false)
      toast.success(t('imageStudio.admin.deleted'))
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('imageStudio.admin.deleteFailed')
      )
    }
  }

  const list = (
    <div className='flex h-full min-h-0 flex-col'>
      <div className='flex items-center justify-between border-b px-3 py-2.5'>
        <span className='text-sm font-semibold'>
          {t('imageStudio.admin.samples')}
        </span>
        <Button
          size='icon-sm'
          variant='ghost'
          onClick={startCreate}
          aria-label={t('imageStudio.admin.addSample')}
        >
          <Plus aria-hidden='true' />
        </Button>
      </div>
      <div className='min-h-0 flex-1 overflow-y-auto p-2'>
        {samplesQuery.isLoading && (
          <LoaderCircle className='mx-auto mt-8 animate-spin' />
        )}
        {samples.map((sample) => (
          <button
            key={sample.id}
            type='button'
            className={cn(
              'hover:bg-muted flex w-full items-center gap-2 rounded-md px-2 py-2 text-left',
              selected?.id === sample.id && 'bg-muted'
            )}
            onClick={() => {
              setCreating(false)
              setSelected(sample)
            }}
          >
            <img
              src={sample.asset.thumbnail_url || sample.asset.content_url}
              alt=''
              className='size-10 rounded object-cover'
            />
            <span className='min-w-0 flex-1'>
              <span className='block truncate text-sm font-medium'>
                {sample.title}
              </span>
              <span className='text-muted-foreground block text-xs'>
                {t(`imageStudio.admin.status.${sample.status}`)}
              </span>
            </span>
          </button>
        ))}
        {samplesQuery.hasNextPage && (
          <Button
            className='mt-2 w-full'
            variant='ghost'
            onClick={() => void samplesQuery.fetchNextPage()}
          >
            {t('imageStudio.loadMore')}
          </Button>
        )}
      </div>
    </div>
  )

  const editor =
    !selected && !creating ? (
      <div className='text-muted-foreground flex h-full items-center justify-center p-8 text-sm'>
        {t('imageStudio.admin.selectSampleToEdit')}
      </div>
    ) : (
      <Form {...form}>
        <form
          className='flex h-full min-h-0 flex-col'
          onSubmit={submit}
          noValidate
        >
          <div className='flex items-center justify-between border-b px-4 py-2.5'>
            <h3 className='text-sm font-semibold'>
              {selected
                ? t('imageStudio.admin.editSample')
                : t('imageStudio.admin.addSample')}
            </h3>
            <div className='flex gap-1'>
              {selected && (
                <Button
                  type='button'
                  size='icon-sm'
                  variant='ghost'
                  onClick={() => setDeleteOpen(true)}
                  aria-label={t('Delete')}
                >
                  <Trash2 aria-hidden='true' />
                </Button>
              )}
              <Button
                type='submit'
                size='sm'
                disabled={saveMutation.isPending || uploadMutation.isPending}
              >
                {saveMutation.isPending && (
                  <LoaderCircle className='animate-spin' />
                )}
                {t('Save')}
              </Button>
            </div>
          </div>
          <div className='min-h-0 flex-1 space-y-5 overflow-y-auto p-4'>
            <div className='grid gap-4 sm:grid-cols-2'>
              <FormField
                control={form.control}
                name='model_profile_id'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('imageStudio.admin.model')}</FormLabel>
                    <FormControl>
                      <NativeSelect
                        value={field.value > 0 ? String(field.value) : ''}
                        disabled={Boolean(selected)}
                        onChange={(event) =>
                          selectProfile(Number(event.target.value))
                        }
                      >
                        <NativeSelectOption value=''>
                          {t('imageStudio.selectModel')}
                        </NativeSelectOption>
                        {modelsQuery.data?.map((model) => (
                          <NativeSelectOption
                            key={model.id}
                            value={String(model.id)}
                          >
                            {model.display_name}
                          </NativeSelectOption>
                        ))}
                      </NativeSelect>
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='title'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('imageStudio.admin.sampleTitle')}</FormLabel>
                    <FormControl>
                      <Input {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>
            <FormField
              control={form.control}
              name='prompt'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('imageStudio.prompt')}</FormLabel>
                  <FormControl>
                    <Textarea {...field} rows={5} maxLength={8000} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='image_asset_id'
              render={() => (
                <FormItem>
                  <AssetPicker
                    asset={asset}
                    disabled={Boolean(selected)}
                    uploading={uploadMutation.isPending}
                    onUpload={upload}
                  />
                  <FormMessage />
                </FormItem>
              )}
            />
            {profile && (
              <ImageSampleParameterFields
                control={form.control}
                profile={profile}
              />
            )}
            <div className='grid gap-4 sm:grid-cols-3'>
              <FormField
                control={form.control}
                name='category'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('imageStudio.admin.category')}</FormLabel>
                    <FormControl>
                      <Input {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='status'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Status')}</FormLabel>
                    <FormControl>
                      <NativeSelect
                        value={field.value}
                        onChange={field.onChange}
                      >
                        <NativeSelectOption value='draft'>
                          {t('imageStudio.admin.status.draft')}
                        </NativeSelectOption>
                        <NativeSelectOption value='published'>
                          {t('imageStudio.admin.status.published')}
                        </NativeSelectOption>
                      </NativeSelect>
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='sort_order'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('imageStudio.admin.sortOrder')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        value={field.value}
                        onChange={(event) =>
                          field.onChange(Number(event.target.value))
                        }
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>
          </div>
        </form>
      </Form>
    )

  return (
    <>
      <ImageAdminWorkspace list={list} editor={editor} />
      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogMedia>
              <Trash2 aria-hidden='true' />
            </AlertDialogMedia>
            <AlertDialogTitle>
              {t('imageStudio.admin.deleteSample')}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t('imageStudio.admin.deleteSampleDescription')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              disabled={deleteMutation.isPending}
              onClick={(event) => {
                event.preventDefault()
                void remove()
              }}
            >
              {t('Delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}

function AssetPicker(props: {
  asset: ImageAsset | null
  disabled: boolean
  uploading: boolean
  onUpload: (file?: File) => Promise<void>
}) {
  const { t } = useTranslation()
  const inputId = 'image-studio-sample-asset'
  return (
    <div className='space-y-2'>
      <FormLabel htmlFor={inputId}>
        {t('imageStudio.admin.sampleImage')}
      </FormLabel>
      {props.asset ? (
        <img
          src={props.asset.thumbnail_url || props.asset.content_url}
          alt=''
          className='max-h-72 rounded-lg border object-contain'
        />
      ) : (
        <label className='hover:bg-muted flex min-h-32 cursor-pointer flex-col items-center justify-center gap-2 rounded-lg border border-dashed p-4 text-sm'>
          {props.uploading ? (
            <LoaderCircle className='animate-spin' />
          ) : (
            <ImagePlus aria-hidden='true' />
          )}
          {t('imageStudio.admin.uploadImage')}
          <input
            id={inputId}
            type='file'
            className='sr-only'
            accept='image/jpeg,image/png,image/webp'
            disabled={props.disabled || props.uploading}
            onChange={(event) => void props.onUpload(event.target.files?.[0])}
          />
        </label>
      )}
    </div>
  )
}
