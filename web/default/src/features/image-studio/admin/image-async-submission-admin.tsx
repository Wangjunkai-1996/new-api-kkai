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
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Switch } from '@/components/ui/switch'
import { useSystemOptions } from '@/features/system-settings/hooks/use-system-options'
import { useUpdateOption } from '@/features/system-settings/hooks/use-update-option'

const ASYNC_OPTION_KEY = 'image_studio.async_submission_enabled'

export function ImageAsyncSubmissionAdmin() {
  const { t } = useTranslation()
  const optionsQuery = useSystemOptions()
  const updateOption = useUpdateOption()
  const option = optionsQuery.data?.data.find(
    (candidate) => candidate.key === ASYNC_OPTION_KEY
  )

  if (optionsQuery.isLoading) {
    return <p className='text-muted-foreground text-sm'>{t('Loading...')}</p>
  }

  if (
    optionsQuery.isError ||
    !option ||
    !['true', 'false', '1', '0'].includes(option.value)
  ) {
    return (
      <Alert variant='destructive'>
        <AlertDescription>
          {t('Unable to load asynchronous submission settings.')}
        </AlertDescription>
        <Button
          type='button'
          size='sm'
          variant='outline'
          disabled={optionsQuery.isFetching}
          onClick={() => void optionsQuery.refetch()}
        >
          {t('Retry')}
        </Button>
      </Alert>
    )
  }

  const disabled = updateOption.isPending || optionsQuery.isFetching

  return (
    <FieldGroup className='rounded-lg border p-4'>
      <Field orientation='horizontal' data-disabled={disabled}>
        <FieldContent>
          <FieldLabel htmlFor='image-async-submission-enabled'>
            {t('Accept asynchronous image requests')}
          </FieldLabel>
          <FieldDescription id='image-async-submission-description'>
            {t(
              'Applies to everyone with Image Studio access. Turning this off stops new asynchronous submissions; accepted tasks continue.'
            )}
          </FieldDescription>
        </FieldContent>
        <Switch
          id='image-async-submission-enabled'
          aria-describedby='image-async-submission-description'
          checked={option.value === 'true' || option.value === '1'}
          disabled={disabled}
          onCheckedChange={(enabled) =>
            updateOption.mutate({
              key: ASYNC_OPTION_KEY,
              value: String(enabled),
            })
          }
        />
      </Field>
    </FieldGroup>
  )
}
