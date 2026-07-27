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
import { useQueries } from '@tanstack/react-query'
import AwsS3, { type AwsS3Options } from '@uppy/aws-s3'
import { Uppy } from '@uppy/core'
import { useUppyEvent, useUppyState } from '@uppy/react'
import { isAxiosError } from 'axios'
import { FileImage, Film, LoaderCircle, Plus, Trash2 } from 'lucide-react'
import { type Ref, useEffect, useId, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Progress } from '@/components/ui/progress'
import { useStatus } from '@/hooks/use-status'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'
import { useVideoStudioDraftStore } from '@/stores/video-studio-draft-store'

import {
  deleteVideoAsset,
  getVideoAssetContentUrl,
  getVideoUpload,
} from '../api'
import { videoStudioQueryKeys } from '../queries'
import type { VideoAsset, VideoUploadPurpose } from '../types'
import { isVideoAssetInspectionPending } from '../video-domain'
import { destroyVideoUploadClientPreservingResumes } from '../video-upload-lifecycle'
import {
  createVideoUploadProtocol,
  type PreparedVideoUploadMeta,
  type VideoUploadMeta,
  VideoUploadProtocolError,
  type VideoUploadResponseBody,
} from '../video-upload-protocol'
import {
  getVideoUploadMaxBytes,
  shouldDeleteVideoAssetOnRemove,
} from '../video-upload-resume'
import { prepareVideoUploadSelection } from '../video-upload-selection'

type VideoAssetUploaderProps = {
  assets: VideoAsset[]
  onAssetsChange: (assets: VideoAsset[]) => void
  purpose: VideoUploadPurpose
  maxFiles: number
  accept: string[]
  label: string
  assetLabels?: string[]
  compact?: boolean
  adminUpload?: boolean
  inputRef?: Ref<HTMLInputElement>
}

export function VideoAssetUploader(props: VideoAssetUploaderProps) {
  const { t } = useTranslation()
  const { status } = useStatus()
  const userId = useAuthStore((state) => state.auth.user?.id ?? 0)
  const inputId = useId()
  const propsRef = useRef(props)
  propsRef.current = props
  const assetsRef = useRef(props.assets)
  assetsRef.current = props.assets
  const uploadResumes = useVideoStudioDraftStore((state) => state.uploadResumes)
  const saveUploadResume = useVideoStudioDraftStore(
    (state) => state.saveUploadResume
  )
  const removeUploadResume = useVideoStudioDraftStore(
    (state) => state.removeUploadResume
  )
  const [completionError, setCompletionError] = useState<string | null>(null)
  const [assetDeleteErrors, setAssetDeleteErrors] = useState<
    Record<number, string>
  >({})
  const [deletingAssetIds, setDeletingAssetIds] = useState<Set<number>>(
    new Set()
  )
  const uploadLimits =
    status?.video_studio?.upload_limits ??
    status?.data?.video_studio?.upload_limits
  const maxUploadBytes = getVideoUploadMaxBytes(
    props.purpose,
    Boolean(props.adminUpload),
    uploadLimits
  )
  const getUploadFailureMessage = (error: unknown) => {
    if (error instanceof VideoUploadProtocolError) {
      return t('videoStudio.uploadFailed')
    }
    if (isAxiosError<{ message?: string }>(error)) {
      return (
        error.response?.data?.message ||
        error.message ||
        t('videoStudio.uploadFailed')
      )
    }
    return error instanceof Error
      ? error.message
      : t('videoStudio.uploadFailed')
  }
  const [uploadProtocol] = useState(() =>
    createVideoUploadProtocol({
      admin: () => Boolean(propsRef.current.adminUpload),
    })
  )
  const [uppy] = useState(() => {
    const instance = new Uppy<VideoUploadMeta, VideoUploadResponseBody>({
      id: `video-studio-${props.purpose}-${inputId}`,
      autoProceed: false,
      allowMultipleUploadBatches: true,
      restrictions: {
        allowedFileTypes: props.accept,
        maxFileSize: maxUploadBytes,
        maxNumberOfFiles: props.maxFiles,
      },
    })

    const awsS3Options: AwsS3Options<VideoUploadMeta, VideoUploadResponseBody> =
      {
        allowedMetaFields: [],
        shouldUseMultipart: (file) => uploadProtocol.shouldUseMultipart(file),
        getChunkSize: (data) => uploadProtocol.getChunkSize(data),
        getUploadParameters: (file) => uploadProtocol.getUploadParameters(file),
        createMultipartUpload: (file) =>
          uploadProtocol.createMultipartUpload(file),
        signPart: (file, { partNumber, signal }) =>
          uploadProtocol.signPart(file, partNumber, signal),
        listParts: (file, { signal }) =>
          uploadProtocol.listParts(file, { signal }),
        completeMultipartUpload: (file, { parts, signal }) =>
          uploadProtocol.completeMultipartUpload(file, { parts, signal }),
        abortMultipartUpload: (file) => uploadProtocol.abortUpload(file),
      }
    instance.use(AwsS3, awsS3Options)
    return instance
  })

  useEffect(() => {
    uppy.setOptions({
      restrictions: {
        ...uppy.opts.restrictions,
        maxFileSize: maxUploadBytes,
      },
    })
  }, [maxUploadBytes, uppy])

  const files = useUppyState(uppy, (state) => Object.values(state.files))
  const totalProgress = useUppyState(uppy, (state) => state.totalProgress)
  const inspectionQueries = useQueries({
    queries: props.assets.map((asset) => ({
      queryKey: videoStudioQueryKeys.upload(
        userId,
        Boolean(props.adminUpload),
        asset.id
      ),
      queryFn: () => getVideoUpload(asset.id, props.adminUpload),
      enabled: userId > 0 && isVideoAssetInspectionPending(asset),
      refetchInterval: 2_000,
    })),
  })
  const inspectedAssets = props.assets.map(
    (asset, index) => inspectionQueries[index]?.data ?? asset
  )

  useEffect(() => {
    const changed = inspectedAssets.some((asset, index) => {
      const previous = props.assets[index]
      return (
        previous?.id !== asset.id ||
        previous.state !== asset.state ||
        previous.updated_at !== asset.updated_at
      )
    })
    if (changed) {
      assetsRef.current = inspectedAssets
      props.onAssetsChange(inspectedAssets)
    }
  }, [inspectedAssets, props])

  const commitUploadedAsset = (asset: VideoAsset, fileId: string) => {
    const latestProps = propsRef.current
    const nextAssets = [
      ...assetsRef.current.filter((current) => current.id !== asset.id),
      asset,
    ].slice(-latestProps.maxFiles)
    assetsRef.current = nextAssets
    latestProps.onAssetsChange(nextAssets)
    useVideoStudioDraftStore.getState().removeUploadResume(asset.id)
    uppy.removeFile(fileId)
    setCompletionError(null)
  }

  useUppyEvent(uppy, 'upload-success', (file, response) => {
    if (!file) return
    if (file.meta.uploadMode === 'multipart') {
      const completedAsset = response.body?.asset
      if (completedAsset) {
        commitUploadedAsset(completedAsset, file.id)
        return
      }
      const message = t('videoStudio.uploadFailed')
      setCompletionError(message)
      toast.error(message)
      if (file.meta.assetId) removeUploadResume(file.meta.assetId)
      uppy.removeFile(file.id)
      return
    }

    void uploadProtocol
      .completeSingleUpload(file)
      .then((asset) => commitUploadedAsset(asset, file.id))
      .catch((error: unknown) => {
        const message = getUploadFailureMessage(error)
        setCompletionError(message)
        toast.error(message)
        void uploadProtocol
          .abortUpload(file)
          .catch(() => undefined)
          .finally(() => uppy.removeFile(file.id))
      })
  })

  useUppyEvent(uppy, 'upload-error', (file, error) => {
    const message = getUploadFailureMessage(error)
    setCompletionError(message)
    toast.error(message)
    if (!file) return
    if (file.meta.uploadMode === 'multipart') return
    void uploadProtocol
      .abortUpload(file)
      .catch(() => undefined)
      .finally(() => queueMicrotask(() => uppy.removeFile(file.id)))
  })

  useEffect(() => () => destroyVideoUploadClientPreservingResumes(uppy), [uppy])

  const isUploading = files.some(
    (file) => !file.error && !file.progress.uploadComplete
  )
  const hasFailedUploads = files.some((file) => Boolean(file.error))
  const canAdd = props.assets.length + files.length < props.maxFiles

  const handleFiles = async (event: React.ChangeEvent<HTMLInputElement>) => {
    const remaining = Math.max(
      0,
      props.maxFiles - props.assets.length - files.length
    )
    const selectedFiles = [...(event.target.files ?? [])].slice(0, remaining)
    event.target.value = ''
    const preparedFileIds: Array<string | null> = []
    const claimedResumeAssetIds = new Set<number>()
    for (const file of selectedFiles) {
      let fileId: string | undefined
      let preparedMeta: PreparedVideoUploadMeta | undefined
      let createdReservation = false
      try {
        fileId = uppy.addFile({
          name: file.name,
          type: file.type,
          data: file,
          source: 'video-studio',
        })
        const currentProps = propsRef.current
        const selection = await prepareVideoUploadSelection({
          file,
          purpose: currentProps.purpose,
          admin: Boolean(currentProps.adminUpload),
          uploadResumes,
          claimedResumeAssetIds,
          protocol: uploadProtocol,
          removeUploadResume,
        })
        if (selection.kind === 'completed') {
          commitUploadedAsset(selection.asset, fileId)
          preparedFileIds.push(null)
          continue
        }
        preparedMeta = selection.meta
        createdReservation = selection.createdReservation
        uppy.setFileMeta(fileId, preparedMeta)
        if (selection.resumeRecord) saveUploadResume(selection.resumeRecord)
        claimedResumeAssetIds.add(preparedMeta.assetId)
        preparedFileIds.push(fileId)
      } catch (error) {
        if (fileId) uppy.removeFile(fileId)
        if (preparedMeta && createdReservation) {
          void uploadProtocol
            .abortUpload({ meta: preparedMeta })
            .catch(() => undefined)
        }
        const message = getUploadFailureMessage(error)
        setCompletionError(message)
        toast.error(message)
        preparedFileIds.push(null)
      }
    }
    if (preparedFileIds.some(Boolean)) {
      try {
        await uppy.upload()
      } catch {
        // Per-file upload errors are surfaced by Uppy's upload-error event.
      }
    }
  }

  const removeAsset = async (asset: VideoAsset) => {
    if (!shouldDeleteVideoAssetOnRemove(asset, Boolean(props.adminUpload))) {
      const nextAssets = inspectedAssets.filter(
        (current) => current.id !== asset.id
      )
      assetsRef.current = nextAssets
      props.onAssetsChange(nextAssets)
      return
    }

    setDeletingAssetIds((current) => new Set(current).add(asset.id))
    setAssetDeleteErrors((current) => {
      const next = { ...current }
      delete next[asset.id]
      return next
    })
    try {
      await deleteVideoAsset(asset.id)
      const nextAssets = assetsRef.current.filter(
        (current) => current.id !== asset.id
      )
      assetsRef.current = nextAssets
      propsRef.current.onAssetsChange(nextAssets)
    } catch (error) {
      let message = t('videoStudio.deleteFailed')
      if (isAxiosError<{ message?: string }>(error)) {
        message = error.response?.data?.message || error.message || message
      } else if (error instanceof Error) {
        message = error.message
      }
      setAssetDeleteErrors((current) => ({
        ...current,
        [asset.id]: message,
      }))
      toast.error(message)
    } finally {
      setDeletingAssetIds((current) => {
        const next = new Set(current)
        next.delete(asset.id)
        return next
      })
    }
  }

  const retryFailedUploads = () => {
    setCompletionError(null)
    void uppy.retryAll().catch((error: unknown) => {
      const message = getUploadFailureMessage(error)
      setCompletionError(message)
      toast.error(message)
    })
  }

  const discardFailedUploads = async () => {
    const failedFiles = files.filter((file) => Boolean(file.error))
    let discardFailed = false
    await Promise.all(
      failedFiles.map(async (file) => {
        try {
          await uploadProtocol.abortUpload(file)
        } catch (error) {
          const message = getUploadFailureMessage(error)
          setCompletionError(message)
          toast.error(message)
          discardFailed = true
          return
        }
        if (file.meta.assetId) removeUploadResume(file.meta.assetId)
        uppy.removeFile(file.id)
      })
    )
    if (!discardFailed) setCompletionError(null)
  }

  return (
    <div className='space-y-2'>
      <div className='grid grid-cols-2 gap-2'>
        {inspectedAssets.map((asset, index) => {
          const isVideo = asset.mime_type?.startsWith('video/')
          const assetUrl =
            asset.content_url || getVideoAssetContentUrl(asset.id)
          return (
            <div
              key={asset.id}
              className='group border-border bg-muted/30 relative aspect-video overflow-hidden rounded-lg border'
            >
              {isVideo ? (
                <video
                  className='size-full object-cover'
                  src={assetUrl}
                  muted
                  playsInline
                  preload='metadata'
                  aria-label={asset.original_filename || props.label}
                />
              ) : (
                <img
                  className='size-full object-cover'
                  src={assetUrl}
                  alt={asset.original_filename || props.label}
                />
              )}
              <span className='bg-background/80 absolute top-1 left-1 rounded px-1.5 py-0.5 text-[10px] font-medium backdrop-blur-sm'>
                {props.assetLabels?.[index] ??
                  (props.maxFiles > 1
                    ? t('videoStudio.frameNumber', { number: index + 1 })
                    : t('videoStudio.reference'))}
              </span>
              {isVideoAssetInspectionPending(asset) && (
                <span className='bg-background/85 absolute inset-x-1 bottom-1 flex items-center justify-center gap-1 rounded px-1.5 py-1 text-[10px] font-medium backdrop-blur-sm'>
                  <LoaderCircle
                    className='size-3 animate-spin motion-reduce:animate-none'
                    aria-hidden='true'
                  />
                  {t('videoStudio.inspecting')}
                </span>
              )}
              <Button
                type='button'
                size='icon-xs'
                variant='destructive'
                className='absolute top-1 right-1 opacity-0 transition-opacity group-focus-within:opacity-100 group-hover:opacity-100'
                onClick={() => void removeAsset(asset)}
                disabled={deletingAssetIds.has(asset.id)}
                aria-label={t('videoStudio.removeAsset')}
              >
                {deletingAssetIds.has(asset.id) ? (
                  <LoaderCircle
                    className='animate-spin motion-reduce:animate-none'
                    aria-hidden='true'
                  />
                ) : (
                  <Trash2 aria-hidden='true' />
                )}
              </Button>
            </div>
          )
        })}

        {canAdd && (
          <label
            htmlFor={inputId}
            className={cn(
              'border-border text-muted-foreground hover:border-foreground/30 hover:text-foreground focus-within:border-ring focus-within:ring-ring/50 flex cursor-pointer items-center justify-center gap-2 rounded-lg border border-dashed transition-colors focus-within:ring-3',
              props.compact ? 'min-h-20' : 'aspect-video'
            )}
          >
            <input
              ref={props.inputRef}
              id={inputId}
              type='file'
              className='sr-only'
              accept={props.accept.join(',')}
              multiple={props.maxFiles > 1}
              onChange={handleFiles}
            />
            {props.accept.some((type) => type.startsWith('video')) ? (
              <Film className='size-4' aria-hidden='true' />
            ) : (
              <FileImage className='size-4' aria-hidden='true' />
            )}
            <span className='text-xs font-medium'>{props.label}</span>
            <Plus className='size-3.5' aria-hidden='true' />
          </label>
        )}
      </div>

      {Object.entries(assetDeleteErrors).map(([assetId, message]) => {
        const asset = inspectedAssets.find(
          (candidate) => candidate.id === Number(assetId)
        )
        if (!asset) return null
        return (
          <div
            key={assetId}
            className='text-destructive flex items-center justify-between gap-2 text-xs'
            role='alert'
          >
            <span>{message}</span>
            <Button
              type='button'
              size='xs'
              variant='outline'
              onClick={() => void removeAsset(asset)}
              disabled={deletingAssetIds.has(asset.id)}
            >
              {t('videoStudio.retry')}
            </Button>
          </div>
        )
      })}

      {isUploading && (
        <div
          className='space-y-1'
          role='status'
          aria-label={t('videoStudio.uploading')}
        >
          <div className='text-muted-foreground flex items-center justify-between text-xs'>
            <span className='inline-flex items-center gap-1.5'>
              <LoaderCircle
                className='size-3.5 animate-spin motion-reduce:animate-none'
                aria-hidden='true'
              />
              {t('videoStudio.uploading')}
            </span>
            <span className='tabular-nums'>{totalProgress}%</span>
          </div>
          <Progress value={totalProgress} />
        </div>
      )}

      {completionError && (
        <div
          className='text-destructive flex items-center justify-between gap-2 text-xs'
          role='alert'
        >
          <span>{completionError}</span>
          {hasFailedUploads && (
            <span className='flex shrink-0 items-center gap-1'>
              <Button
                type='button'
                size='xs'
                variant='outline'
                onClick={retryFailedUploads}
              >
                {t('videoStudio.retry')}
              </Button>
              <Button
                type='button'
                size='icon-xs'
                variant='ghost'
                onClick={() => void discardFailedUploads()}
                aria-label={t('videoStudio.removeAsset')}
              >
                <Trash2 aria-hidden='true' />
              </Button>
            </span>
          )}
        </div>
      )}
      {inspectedAssets.some((asset) => asset.state === 'failed') && (
        <p className='text-destructive text-xs' role='alert'>
          {inspectedAssets.find((asset) => asset.state === 'failed')
            ?.failure_reason || t('videoStudio.inspectFailed')}
        </p>
      )}
    </div>
  )
}
