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
import { useEffect } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

const createRateLimitDialogSchema = (t: (key: string) => string) =>
  z
    .object({
      targetType: z.enum(['group', 'id', 'username']),
      targetValue: z.string().trim().min(1, t('This field is required')),
      maxRequests: z
        .number()
        .int(t('Value must be an integer'))
        .min(0, t('Value must be at least 0'))
        .max(2147483647, t('Value must not exceed 2,147,483,647')),
      maxSuccess: z
        .number()
        .int(t('Value must be an integer'))
        .min(1, t('Value must be at least 1'))
        .max(2147483647, t('Value must not exceed 2,147,483,647')),
    })
    .superRefine((values, context) => {
      if (
        values.targetType === 'id' &&
        !/^[1-9]\d*$/.test(values.targetValue)
      ) {
        context.addIssue({
          code: 'custom',
          path: ['targetValue'],
          message: t('User ID must be a positive integer'),
        })
      }
    })

type RateLimitDialogFormValues = z.infer<
  ReturnType<typeof createRateLimitDialogSchema>
>

export type RateLimitEntryData = {
  target: string
  maxRequests: number
  maxSuccess: number
}

type RateLimitDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSave: (data: RateLimitEntryData) => void
  editData?: RateLimitEntryData | null
  scope?: 'group' | 'user'
}

export function RateLimitDialog({
  open,
  onOpenChange,
  onSave,
  editData,
  scope = 'group',
}: RateLimitDialogProps) {
  const { t } = useTranslation()
  const isEditMode = !!editData
  const rateLimitDialogSchema = createRateLimitDialogSchema(t)
  const formId =
    scope === 'user' ? 'user-rate-limit-form' : 'group-rate-limit-form'

  const form = useForm<RateLimitDialogFormValues>({
    resolver: zodResolver(rateLimitDialogSchema),
    defaultValues: {
      targetType: scope === 'user' ? 'id' : 'group',
      targetValue: '',
      maxRequests: 0,
      maxSuccess: 1,
    },
  })
  const targetType = useWatch({
    control: form.control,
    name: 'targetType',
  })

  useEffect(() => {
    if (editData) {
      const separatorIndex = editData.target.indexOf(':')
      const targetPrefix = editData.target.slice(0, separatorIndex)
      let normalizedTargetType: RateLimitDialogFormValues['targetType'] =
        'group'
      if (
        scope === 'user' &&
        (targetPrefix === 'id' || targetPrefix === 'username')
      ) {
        normalizedTargetType = targetPrefix
      }
      form.reset({
        targetType: normalizedTargetType,
        targetValue:
          normalizedTargetType === 'group'
            ? editData.target
            : editData.target.slice(separatorIndex + 1),
        maxRequests: editData.maxRequests,
        maxSuccess: editData.maxSuccess,
      })
    } else {
      form.reset({
        targetType: scope === 'user' ? 'id' : 'group',
        targetValue: '',
        maxRequests: 0,
        maxSuccess: 1,
      })
    }
  }, [editData, form, open, scope])

  const handleSubmit = (values: RateLimitDialogFormValues) => {
    const targetValue = values.targetValue.trim()
    onSave({
      target:
        values.targetType === 'group'
          ? targetValue
          : `${values.targetType}:${targetValue}`,
      maxRequests: values.maxRequests,
      maxSuccess: values.maxSuccess,
    })
    form.reset()
    onOpenChange(false)
  }

  let dialogTitle: string
  let dialogDescription: string
  if (scope === 'user') {
    dialogTitle = isEditMode
      ? t('Edit user rate limit')
      : t('Add user rate limit')
    dialogDescription = t('Configure rate limiting rules for a specific user.')
  } else {
    dialogTitle = isEditMode
      ? t('Edit group rate limit')
      : t('Add group rate limit')
    dialogDescription = t(
      'Configure rate limiting rules for a specific user group.'
    )
  }

  let targetLabel = t('Group Name')
  let targetPlaceholder = t('e.g., default, vip, premium')
  let targetDescription = isEditMode
    ? t('Group name cannot be changed when editing.')
    : t('Unique identifier for this group.')
  if (scope === 'user') {
    targetLabel = targetType === 'id' ? t('User ID') : t('Username')
    targetPlaceholder =
      targetType === 'id' ? t('Enter user ID') : t('Enter username')
    if (isEditMode) {
      targetDescription = t('User identifier cannot be changed when editing.')
    } else if (targetType === 'id') {
      targetDescription = t('User ID remains valid if the username changes.')
    } else {
      targetDescription = t(
        'Username matching is exact; update this rule if the username changes.'
      )
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={dialogTitle}
      description={dialogDescription}
      contentClassName='sm:max-w-[500px]'
      contentHeight='auto'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button type='submit' form={formId}>
            {isEditMode ? t('Update') : t('Add')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <form
          id={formId}
          onSubmit={form.handleSubmit(handleSubmit)}
          className='space-y-4'
        >
          {scope === 'user' && (
            <FormField
              control={form.control}
              name='targetType'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Match user by')}</FormLabel>
                  <Select
                    items={[
                      { value: 'id', label: t('User ID') },
                      { value: 'username', label: t('Username') },
                    ]}
                    value={field.value}
                    onValueChange={(value) => value && field.onChange(value)}
                    disabled={isEditMode}
                  >
                    <FormControl>
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent alignItemWithTrigger={false}>
                      <SelectGroup>
                        <SelectItem value='id'>{t('User ID')}</SelectItem>
                        <SelectItem value='username'>
                          {t('Username')}
                        </SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  <FormMessage />
                </FormItem>
              )}
            />
          )}

          <FormField
            control={form.control}
            name='targetValue'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{targetLabel}</FormLabel>
                <FormControl>
                  <Input
                    placeholder={targetPlaceholder}
                    inputMode={
                      scope === 'user' && targetType === 'id'
                        ? 'numeric'
                        : undefined
                    }
                    {...field}
                    disabled={isEditMode}
                  />
                </FormControl>
                <FormDescription>{targetDescription}</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='maxRequests'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Max Requests (including failures)')}</FormLabel>
                <FormControl>
                  <div className='flex items-center gap-2'>
                    <Input
                      type='number'
                      min={0}
                      max={2147483647}
                      step={1}
                      {...field}
                      onChange={(e) =>
                        field.onChange(Number.parseInt(e.target.value) || 0)
                      }
                    />
                    <span className='text-muted-foreground text-sm'>
                      {t('times')}
                    </span>
                  </div>
                </FormControl>
                <FormDescription>
                  {t('Total requests allowed per period. 0 = unlimited.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='maxSuccess'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Max Successful Requests')}</FormLabel>
                <FormControl>
                  <div className='flex items-center gap-2'>
                    <Input
                      type='number'
                      min={1}
                      max={2147483647}
                      step={1}
                      {...field}
                      onChange={(e) =>
                        field.onChange(Number.parseInt(e.target.value) || 1)
                      }
                    />
                    <span className='text-muted-foreground text-sm'>
                      {t('times')}
                    </span>
                  </div>
                </FormControl>
                <FormDescription>
                  {t('Only successful requests count toward this limit.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </form>
      </Form>
    </Dialog>
  )
}
