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
import { LoaderCircle, Plus, Trash2 } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useForm, type UseFormReturn } from 'react-hook-form'
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
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'

import {
  useAdminImageModelCandidates,
  useAdminImageModels,
  useDeleteAdminImageModel,
  useSaveAdminImageModel,
} from '../queries'
import type { ImageModelProfile } from '../types'
import {
  createImageModelFormValues,
  imageModelFormSchema,
  parseImageModelForm,
  type ImageModelFormValues,
} from './image-admin-forms'
import { ImageAdminWorkspace } from './image-admin-workspace'
import { ImageParameterSpecEditor } from './image-parameter-spec-editor'

export function ImageModelAdmin() {
  const { t } = useTranslation()
  const modelsQuery = useAdminImageModels()
  const candidatesQuery = useAdminImageModelCandidates()
  const saveMutation = useSaveAdminImageModel()
  const deleteMutation = useDeleteAdminImageModel()
  const [selected, setSelected] = useState<ImageModelProfile | null>(null)
  const [creating, setCreating] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const form = useForm<ImageModelFormValues>({
    resolver: zodResolver(imageModelFormSchema),
    defaultValues: createImageModelFormValues(),
  })
  const candidates = useMemo(() => {
    const configured = new Set(modelsQuery.data?.map((model) => model.model))
    return (candidatesQuery.data ?? []).filter(
      (candidate) => !configured.has(candidate)
    )
  }, [candidatesQuery.data, modelsQuery.data])

  useEffect(() => {
    if (selected) form.reset(createImageModelFormValues(selected))
  }, [form, selected])

  const startCreate = (): void => {
    setSelected(null)
    setCreating(true)
    form.reset(createImageModelFormValues())
  }

  const submit = form.handleSubmit(async (values) => {
    try {
      const saved = await saveMutation.mutateAsync({
        id: selected?.id,
        values: parseImageModelForm(values, selected ?? undefined),
      })
      setSelected(saved)
      setCreating(false)
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
      form.reset(createImageModelFormValues())
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
          {t('imageStudio.admin.models')}
        </span>
        <Button
          size='icon-sm'
          variant='ghost'
          onClick={startCreate}
          aria-label={t('imageStudio.admin.addModel')}
        >
          <Plus aria-hidden='true' />
        </Button>
      </div>
      <div className='min-h-0 flex-1 overflow-y-auto p-2'>
        {modelsQuery.isLoading && (
          <LoaderCircle className='mx-auto mt-8 animate-spin' />
        )}
        {modelsQuery.data?.map((model) => (
          <button
            key={model.id}
            type='button'
            className={cn(
              'hover:bg-muted flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left',
              selected?.id === model.id && 'bg-muted'
            )}
            onClick={() => {
              setCreating(false)
              setSelected(model)
            }}
          >
            <span
              className={cn(
                'size-2 rounded-full',
                model.enabled ? 'bg-success' : 'bg-muted-foreground/40'
              )}
            />
            <span className='truncate text-sm font-medium'>
              {model.display_name}
            </span>
          </button>
        ))}
      </div>
    </div>
  )

  const editor =
    !selected && !creating ? (
      <div className='text-muted-foreground flex h-full items-center justify-center p-8 text-sm'>
        {t('imageStudio.admin.selectModelToEdit')}
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
                ? t('imageStudio.admin.editModel')
                : t('imageStudio.admin.addModel')}
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
              <Button type='submit' size='sm' disabled={saveMutation.isPending}>
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
                name='model'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('imageStudio.admin.model')}</FormLabel>
                    <FormControl>
                      {selected ? (
                        <Input {...field} readOnly />
                      ) : (
                        <NativeSelect
                          value={field.value}
                          onChange={(event) =>
                            form.reset(
                              createImageModelFormValues(
                                undefined,
                                event.target.value
                              )
                            )
                          }
                        >
                          <NativeSelectOption value=''>
                            {t('imageStudio.admin.selectCandidate')}
                          </NativeSelectOption>
                          {candidates.map((candidate) => (
                            <NativeSelectOption
                              key={candidate}
                              value={candidate}
                            >
                              {candidate}
                            </NativeSelectOption>
                          ))}
                        </NativeSelect>
                      )}
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <TextField
                form={form}
                name='display_name'
                label={t('imageStudio.admin.displayName')}
              />
              <TextField
                form={form}
                name='provider_label'
                label={t('imageStudio.admin.provider')}
              />
              <NumberField
                form={form}
                name='sort_order'
                label={t('imageStudio.admin.sortOrder')}
              />
            </div>
            <FormField
              control={form.control}
              name='description'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Description')}</FormLabel>
                  <FormControl>
                    <Textarea {...field} rows={3} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='enabled'
              render={({ field }) => (
                <FormItem className='flex items-center justify-between rounded-md border px-3 py-2'>
                  <FormLabel>{t('Enabled')}</FormLabel>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </FormItem>
              )}
            />
            <ImageParameterSpecEditor />
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
              {t('imageStudio.admin.deleteModel')}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t('imageStudio.admin.deleteModelDescription')}
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

function TextField(props: {
  form: UseFormReturn<ImageModelFormValues>
  name: 'display_name' | 'provider_label'
  label: string
}) {
  return (
    <FormField
      control={props.form.control}
      name={props.name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{props.label}</FormLabel>
          <FormControl>
            <Input {...field} />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

function NumberField(props: {
  form: UseFormReturn<ImageModelFormValues>
  name: 'sort_order'
  label: string
}) {
  return (
    <FormField
      control={props.form.control}
      name={props.name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{props.label}</FormLabel>
          <FormControl>
            <Input
              type='number'
              value={field.value}
              onChange={(event) => field.onChange(Number(event.target.value))}
            />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}
