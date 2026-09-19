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
  useIsMutating,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'
import { isAxiosError } from 'axios'
import { Loader2, RotateCw } from 'lucide-react'
import { useMemo, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { GroupBadge } from '@/components/group-badge'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { getUserGroups } from '@/lib/api'
import { toUserGroupOption } from '@/lib/group-display'

import { updateApiKeyGroup } from '../api'
import type { ApiKey } from '../types'
import {
  ApiKeyGroupCombobox,
  type ApiKeyGroupOption,
} from './api-key-group-combobox'
import { useApiKeys } from './api-keys-provider'

type ApiKeyGroupCellProps = {
  apiKey: ApiKey
}

export function ApiKeyGroupCell(props: ApiKeyGroupCellProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { triggerRefresh } = useApiKeys()
  const group = props.apiKey.group ?? ''
  const pending =
    useIsMutating({ mutationKey: ['token-group', props.apiKey.id] }) > 0
  const groupsQuery = useQuery({
    queryKey: ['user-groups'],
    queryFn: getUserGroups,
    staleTime: 60_000,
    retry: false,
  })
  const groupsData = groupsQuery.data
  const groupsFailed = groupsQuery.isError || groupsData?.success === false

  const availableGroups = useMemo(() => {
    if (!groupsData?.success || !groupsData.data) return []
    return Object.entries(groupsData.data).map(([key, info]) =>
      toUserGroupOption(key, info)
    )
  }, [groupsData])

  const options = useMemo<ApiKeyGroupOption[]>(() => {
    if (
      !groupsData?.success ||
      !groupsData.data ||
      !group ||
      availableGroups.some((option) => option.value === group)
    ) {
      return availableGroups
    }

    return [
      {
        value: group,
        label: group,
        desc: t('Unavailable'),
        disabled: true,
      },
      ...availableGroups,
    ]
  }, [availableGroups, groupsData, group, t])

  const selectedOption = options.find((option) => option.value === group)
  const selectedInfo = groupsData?.data?.[group]
  const isAutoGroup = selectedOption?.isAuto || group === 'auto'
  const ratio =
    !isAutoGroup && typeof selectedInfo?.ratio === 'number'
      ? selectedInfo.ratio
      : undefined

  const mutation = useMutation({
    mutationKey: ['token-group', props.apiKey.id],
    mutationFn: async (nextGroup: string) => {
      const result = await updateApiKeyGroup(props.apiKey.id, nextGroup)
      if (!result.success || !result.data) {
        throw new Error(result.message || t('Failed to update token group'))
      }
      return result.data
    },
    onMutate: () => queryClient.cancelQueries({ queryKey: ['keys'] }),
    onSuccess: (updatedToken) => {
      queryClient.setQueriesData<{ items: ApiKey[]; total: number }>(
        { queryKey: ['keys'] },
        (data) =>
          data && {
            ...data,
            items: data.items.map((item) =>
              item.id === updatedToken.id
                ? {
                    ...item,
                    group: updatedToken.group,
                    cross_group_retry: updatedToken.cross_group_retry,
                  }
                : item
            ),
          }
      )
      toast.success(t('Token group updated'))
      triggerRefresh()
    },
    onError: (error) => {
      const serverMessage = isAxiosError<{ message?: string }>(error)
        ? error.response?.data?.message
        : undefined
      toast.error(
        serverMessage || error.message || t('Failed to update token group')
      )
      void groupsQuery.refetch()
    },
  })

  const handleChange = (nextGroup: string) => {
    if (nextGroup === group || pending) return
    mutation.mutate(nextGroup)
  }

  let groupStatusContent: ReactNode
  if (groupsQuery.isPending) {
    groupStatusContent = (
      <span
        role='status'
        className='text-muted-foreground flex items-center justify-center gap-2 px-3 py-6 text-xs'
      >
        <Loader2 className='size-4 animate-spin' />
        {t('Loading...')}
      </span>
    )
  } else if (groupsFailed) {
    groupStatusContent = (
      <div
        role='alert'
        className='flex flex-col items-center gap-3 px-3 py-5 text-center text-xs'
      >
        <span className='text-muted-foreground'>
          {groupsData?.message || t('Request failed')}
        </span>
        <Button
          size='sm'
          variant='outline'
          disabled={groupsQuery.isFetching}
          onClick={() => {
            void groupsQuery.refetch()
          }}
        >
          <RotateCw className='size-3.5' />
          {t('Retry')}
        </Button>
      </div>
    )
  }

  return (
    <div className='max-w-full'>
      <ApiKeyGroupCombobox
        options={options}
        value={group}
        onValueChange={handleChange}
        compact
        pending={pending}
        disabled={pending}
        onOpen={() => {
          if (groupsQuery.isStale && !groupsQuery.isFetching) {
            void groupsQuery.refetch()
          }
        }}
        statusContent={groupStatusContent}
        triggerAriaLabel={`${t('Group')}: ${props.apiKey.name}`}
        trigger={
          <span className='flex min-w-0 flex-1 items-center gap-1.5 overflow-hidden'>
            <GroupBadge
              group={group}
              displayName={selectedOption?.label}
              isAutoGroup={isAutoGroup}
              ratio={ratio}
              className='max-w-[10rem]'
            />
            {isAutoGroup && props.apiKey.cross_group_retry && (
              <StatusBadge
                label={t('Cross-group')}
                variant='info'
                copyable={false}
                className='hidden shrink-0 text-[10px] xl:inline-flex'
              />
            )}
          </span>
        }
      />
    </div>
  )
}
