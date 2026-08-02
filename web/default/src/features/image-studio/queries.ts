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
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'

import { privateUserQueryKey } from '@/lib/private-query-cache'
import { useAuthStore } from '@/stores/auth-store'

import {
  createImageGeneration,
  createImageToken,
  deleteAdminImageModel,
  deleteAdminImageSample,
  deleteImageGeneration,
  getAdminImageModelCandidates,
  getAdminImageModels,
  getAdminImageSamples,
  getImageGenerations,
  getImageModels,
  getImageSample,
  getImageSamples,
  getImageTokenCapability,
  quoteImageGeneration,
  saveAdminImageModel,
  saveAdminImageSample,
  uploadAdminImageSampleAsset,
} from './api'
import type {
  CreateImageRequest,
  ImageGenerationStatus,
  ImageQuoteRequest,
} from './types'

export const imageStudioQueryKeys = {
  privateAll: (userId: number) => privateUserQueryKey(userId, 'image-studio'),
  token: (userId: number) =>
    privateUserQueryKey(userId, 'image-studio', 'token'),
  models: (userId: number, tokenId: number) =>
    privateUserQueryKey(userId, 'image-studio', 'models', tokenId),
  samples: (userId: number, tokenId: number) =>
    privateUserQueryKey(userId, 'image-studio', 'samples', tokenId),
  sample: (userId: number, tokenId: number, id: number) =>
    privateUserQueryKey(userId, 'image-studio', 'sample', tokenId, id),
  quote: (userId: number, request: ImageQuoteRequest | null) =>
    privateUserQueryKey(userId, 'image-studio', 'quote', request),
  generations: (
    userId: number,
    filters: { model?: string; status?: ImageGenerationStatus }
  ) => privateUserQueryKey(userId, 'image-studio', 'generations', filters),
  adminModels: (userId: number) =>
    privateUserQueryKey(userId, 'image-studio', 'admin-models'),
  adminCandidates: (userId: number) =>
    privateUserQueryKey(userId, 'image-studio', 'admin-candidates'),
  adminSamples: (userId: number) =>
    privateUserQueryKey(userId, 'image-studio', 'admin-samples'),
}

const useImageStudioUserId = (): number =>
  useAuthStore((state) => state.auth.user?.id ?? 0)

export const useImageTokenCapability = () => {
  const userId = useImageStudioUserId()
  return useQuery({
    queryKey: imageStudioQueryKeys.token(userId),
    queryFn: getImageTokenCapability,
    enabled: userId > 0,
  })
}

export const useCreateImageToken = () => {
  const queryClient = useQueryClient()
  const userId = useImageStudioUserId()
  return useMutation({
    mutationFn: createImageToken,
    onSuccess: (capability) => {
      queryClient.setQueryData(imageStudioQueryKeys.token(userId), capability)
    },
  })
}

export const useImageModels = (tokenId: number | null) => {
  const userId = useImageStudioUserId()
  return useQuery({
    queryKey: imageStudioQueryKeys.models(userId, tokenId ?? 0),
    queryFn: () => getImageModels(tokenId ?? 0),
    enabled: userId > 0 && Boolean(tokenId),
  })
}

export const useImageSamples = (tokenId: number | null) => {
  const userId = useImageStudioUserId()
  return useInfiniteQuery({
    queryKey: imageStudioQueryKeys.samples(userId, tokenId ?? 0),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => getImageSamples(tokenId ?? 0, pageParam),
    getNextPageParam: (lastPage) => lastPage.next_cursor,
    enabled: userId > 0 && Boolean(tokenId),
  })
}

export const useImageSample = (
  sampleId: number | undefined,
  tokenId: number | null
) => {
  const userId = useImageStudioUserId()
  return useQuery({
    queryKey: imageStudioQueryKeys.sample(userId, tokenId ?? 0, sampleId ?? 0),
    queryFn: () => getImageSample(sampleId ?? 0, tokenId ?? 0),
    enabled: userId > 0 && Boolean(sampleId) && Boolean(tokenId),
  })
}

export const useImageQuote = (request: ImageQuoteRequest | null) => {
  const userId = useImageStudioUserId()
  return useQuery({
    queryKey: imageStudioQueryKeys.quote(userId, request),
    queryFn: () => quoteImageGeneration(request as ImageQuoteRequest),
    enabled: userId > 0 && request !== null,
    retry: false,
  })
}

export const useCreateImageGeneration = () => {
  const queryClient = useQueryClient()
  const userId = useImageStudioUserId()
  return useMutation({
    mutationFn: (input: {
      request: CreateImageRequest
      idempotencyKey: string
    }) => createImageGeneration(input.request, input.idempotencyKey),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: privateUserQueryKey(userId, 'image-studio', 'generations'),
      })
    },
  })
}

export const useImageGenerations = (
  filters: { model?: string; status?: ImageGenerationStatus } = {}
) => {
  const userId = useImageStudioUserId()
  return useInfiniteQuery({
    queryKey: imageStudioQueryKeys.generations(userId, filters),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      getImageGenerations({ ...filters, cursor: pageParam }),
    getNextPageParam: (lastPage) => lastPage.next_cursor,
    enabled: userId > 0,
  })
}

export const useDeleteImageGeneration = () => {
  const queryClient = useQueryClient()
  const userId = useImageStudioUserId()
  return useMutation({
    mutationFn: deleteImageGeneration,
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: privateUserQueryKey(userId, 'image-studio', 'generations'),
      })
    },
  })
}

export const useAdminImageModels = () => {
  const userId = useImageStudioUserId()
  return useQuery({
    queryKey: imageStudioQueryKeys.adminModels(userId),
    queryFn: getAdminImageModels,
    enabled: userId > 0,
  })
}

export const useAdminImageModelCandidates = () => {
  const userId = useImageStudioUserId()
  return useQuery({
    queryKey: imageStudioQueryKeys.adminCandidates(userId),
    queryFn: getAdminImageModelCandidates,
    enabled: userId > 0,
  })
}

export const useSaveAdminImageModel = () => {
  const queryClient = useQueryClient()
  const userId = useImageStudioUserId()
  return useMutation({
    mutationFn: saveAdminImageModel,
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: imageStudioQueryKeys.adminModels(userId),
      })
      void queryClient.invalidateQueries({
        queryKey: imageStudioQueryKeys.adminCandidates(userId),
      })
    },
  })
}

export const useDeleteAdminImageModel = () => {
  const queryClient = useQueryClient()
  const userId = useImageStudioUserId()
  return useMutation({
    mutationFn: deleteAdminImageModel,
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: imageStudioQueryKeys.adminModels(userId),
      })
    },
  })
}

export const useAdminImageSamples = () => {
  const userId = useImageStudioUserId()
  return useInfiniteQuery({
    queryKey: imageStudioQueryKeys.adminSamples(userId),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => getAdminImageSamples(pageParam),
    getNextPageParam: (lastPage) => lastPage.next_cursor,
    enabled: userId > 0,
  })
}

export const useUploadAdminImageAsset = () =>
  useMutation({ mutationFn: uploadAdminImageSampleAsset })

export const useSaveAdminImageSample = () => {
  const queryClient = useQueryClient()
  const userId = useImageStudioUserId()
  return useMutation({
    mutationFn: saveAdminImageSample,
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: imageStudioQueryKeys.adminSamples(userId),
      })
      void queryClient.invalidateQueries({
        queryKey: imageStudioQueryKeys.adminModels(userId),
      })
    },
  })
}

export const useDeleteAdminImageSample = () => {
  const queryClient = useQueryClient()
  const userId = useImageStudioUserId()
  return useMutation({
    mutationFn: deleteAdminImageSample,
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: imageStudioQueryKeys.adminSamples(userId),
      })
      void queryClient.invalidateQueries({
        queryKey: imageStudioQueryKeys.adminModels(userId),
      })
    },
  })
}
