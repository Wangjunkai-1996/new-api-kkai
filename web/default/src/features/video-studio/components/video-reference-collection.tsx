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
  ArrowLeft,
  ArrowRight,
  FileAudio,
  FileImage,
  FileVideo,
  MoreHorizontal,
  Play,
  Plus,
  RefreshCw,
} from 'lucide-react'
import { useEffect, useId, useRef, useState, type SyntheticEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { toIntlLocale } from '@/i18n/languages'
import { signedStudioMediaUrl, useStudioMediaUrls } from '@/lib/studio-media'

import { getVideoAssetContentUrl } from '../api'
import type {
  VideoAsset,
  VideoComposerReference,
  VideoModelProfile,
  VideoReferenceRole,
} from '../types'
import {
  VideoAssetUploader,
  type VideoAssetUploaderHandle,
} from './video-asset-uploader'

type CollectionRole = 'reference_image' | 'reference_video' | 'reference_audio'

export type VideoReferenceCollectionProps = {
  references: VideoComposerReference[]
  onChange: (references: VideoComposerReference[]) => void
  profile: VideoModelProfile
  disabled?: boolean
  onBusyChange?: (busy: boolean) => void
  admin?: boolean
}

const MEDIA_TYPES = [
  {
    role: 'reference_image',
    policy: 'image',
    purpose: 'reference',
    Icon: FileImage,
  },
  {
    role: 'reference_video',
    policy: 'video',
    purpose: 'reference_video',
    Icon: FileVideo,
  },
  {
    role: 'reference_audio',
    policy: 'audio',
    purpose: 'reference_audio',
    Icon: FileAudio,
  },
] as const
const ROLE_ORDER: VideoReferenceRole[] = [
  'reference_image',
  'reference_video',
  'reference_audio',
  'first_frame',
  'last_frame',
  'reference',
]

// Keep draft order for excluded items too. Wire normalization assigns included positions.
function orderedReferences(
  references: VideoComposerReference[]
): VideoComposerReference[] {
  const positions = new Map<VideoReferenceRole, number>()
  return [...references]
    .sort(
      (left, right) =>
        ROLE_ORDER.indexOf(left.role) - ROLE_ORDER.indexOf(right.role) ||
        left.position - right.position
    )
    .map((reference) => {
      const position = positions.get(reference.role) ?? 0
      if (!reference.included) {
        return reference
      }
      positions.set(reference.role, position + 1)
      return { ...reference, position }
    })
}

function pauseOtherMedia(event: SyntheticEvent<HTMLMediaElement>) {
  document
    .querySelectorAll<HTMLMediaElement>('audio,video')
    .forEach((media) => {
      if (media !== event.currentTarget) media.pause()
    })
}

export function VideoReferenceCollection(props: VideoReferenceCollectionProps) {
  const { t, i18n } = useTranslation()
  const headingId = useId()
  const [addOpen, setAddOpen] = useState(false)
  const [target, setTarget] = useState<{
    role: CollectionRole
    replaceId?: string
  } | null>(null)
  const [uploads, setUploads] = useState<VideoAsset[]>([])
  const [uploadBusy, setUploadBusy] = useState(false)
  const uploaderRef = useRef<VideoAssetUploaderHandle>(null)
  const [previewId, setPreviewId] = useState<string | null>(null)
  const [mediaFailed, setMediaFailed] = useState(false)
  const onBusyChange = props.onBusyChange
  const hasUploadSession = target !== null
  useEffect(() => {
    onBusyChange?.(hasUploadSession)
  }, [hasUploadSession, onBusyChange])
  useEffect(() => () => onBusyChange?.(false), [onBusyChange])

  const policy = props.profile.specification.reference_policy
  const ordered = orderedReferences(props.references)
  const included = ordered.filter((reference) => reference.included)
  const excluded = ordered.filter((reference) => !reference.included)
  const mediaQuery = useStudioMediaUrls(
    props.references.flatMap((reference) => {
      if (!reference.asset || reference.asset.state !== 'ready') return []
      return [
        reference.asset.content_url ||
          getVideoAssetContentUrl(reference.asset.id),
        reference.asset.poster_url,
      ]
    })
  )
  const number = new Intl.NumberFormat(
    toIntlLocale(i18n.resolvedLanguage ?? i18n.language),
    {
      maximumFractionDigits: 2,
    }
  )
  const labels = {
    reference_image: t('videoStudio.referenceImage'),
    reference_video: t('videoStudio.referenceVideo'),
    reference_audio: t('Reference audio'),
    first_frame: t('videoStudio.firstFrame'),
    last_frame: t('videoStudio.lastFrame'),
    reference: t('videoStudio.reference'),
  }
  const selectedType = MEDIA_TYPES.find((type) => type.role === target?.role)
  const selectedPolicy = selectedType
    ? policy?.[selectedType.policy]
    : undefined
  let maxFiles = 0
  if (selectedPolicy && target) {
    const groupCount = included.filter(
      (reference) => reference.role === target.role
    ).length
    maxFiles = Math.max(
      0,
      Math.min(
        selectedPolicy.max_count - groupCount,
        (policy?.max_total_count ?? 0) - included.length
      )
    )
    if (target.replaceId) maxFiles = 1
  }
  const uploadBindings: VideoComposerReference[] = uploads.map(
    (asset, position) => ({
      client_id: `asset-${asset.id}`,
      asset_id: asset.id,
      role: target?.role ?? 'reference_image',
      position,
      included: true,
      asset,
    })
  )
  const preview = props.references.find(
    (reference) => reference.client_id === previewId
  )
  const previewAsset = preview?.asset
  const previewSource = signedStudioMediaUrl(
    previewAsset?.content_url ||
      (previewAsset ? getVideoAssetContentUrl(previewAsset.id) : undefined),
    mediaQuery.data?.urls,
    mediaQuery.data?.deniedPaths,
    mediaQuery.identityDenied
  )
  const readyToApply =
    uploads.length > 0 &&
    uploads.every((asset) => asset.state === 'ready') &&
    !uploadBusy
  let proposedReferences = ordered
  if (target?.replaceId && uploadBindings[0]) {
    proposedReferences = ordered.map((reference) =>
      reference.client_id === target.replaceId
        ? {
            ...reference,
            asset_id: uploadBindings[0].asset_id,
            asset: uploadBindings[0].asset,
          }
        : reference
    )
  } else if (target) {
    const nextPosition = ordered.filter(
      (reference) => reference.role === target.role
    ).length
    proposedReferences = [
      ...ordered,
      ...uploadBindings.map((reference, index) => ({
        ...reference,
        position: nextPosition + index,
      })),
    ]
  }
  // Combination rules are evaluated by the composer after every edit. This
  // lets users add an audio clip before its required visual reference and
  // keeps the incompatible material visible for repair.
  const busy = Boolean(props.disabled || target)

  const openUpload = (role: CollectionRole, replaceId?: string) => {
    setUploads([])
    setUploadBusy(false)
    setTarget({ role, replaceId })
    setAddOpen(true)
  }
  const closeUpload = async () => {
    if (
      uploadBusy &&
      uploaderRef.current &&
      !(await uploaderRef.current.cancel())
    ) {
      return
    }
    setAddOpen(false)
    setTarget(null)
    setUploads([])
  }
  const move = (reference: VideoComposerReference, direction: -1 | 1) => {
    const group = included.filter((item) => item.role === reference.role)
    const index = group.findIndex(
      (item) => item.client_id === reference.client_id
    )
    const neighbor = group[index + direction]
    if (!neighbor) return
    props.onChange(
      orderedReferences(
        ordered.map((item) => {
          if (item.client_id === reference.client_id) {
            return { ...item, position: neighbor.position }
          }
          if (item.client_id === neighbor.client_id) {
            return { ...item, position: reference.position }
          }
          return item
        })
      )
    )
  }

  return (
    <section
      className='video-studio-reference-collection'
      aria-labelledby={headingId}
    >
      <div className='video-studio-reference-heading flex items-center justify-between gap-2'>
        <h2 id={headingId}>{t('Reference media')}</h2>
        <Button
          type='button'
          size='sm'
          variant='ghost'
          disabled={busy || !policy?.max_total_count}
          onClick={() => setAddOpen(true)}
        >
          <Plus aria-hidden='true' />
          {t('Add references')}
        </Button>
      </div>
      <p className='video-studio-reference-description text-muted-foreground text-xs'>
        {t(
          'Use images for appearance, videos for motion, and audio for sound.'
        )}
      </p>
      <div className='video-studio-reference-summary flex flex-wrap gap-x-3 gap-y-1 text-xs'>
        {MEDIA_TYPES.map((type) => {
          const group = included.filter(
            (reference) => reference.role === type.role
          )
          const config = policy?.[type.policy]
          if (!config && group.length === 0) return null
          const duration = group.reduce(
            (total, reference) =>
              total + (reference.asset?.duration_seconds ?? 0),
            0
          )
          return (
            <span key={type.role}>
              {labels[type.role]} {group.length}/{config?.max_count ?? 0}
              {type.policy !== 'image' && (
                <>
                  {' '}
                  · {t('{{seconds}} s', { seconds: number.format(duration) })}
                </>
              )}
            </span>
          )
        })}
      </div>
      <ul className='video-studio-reference-grid' data-count={included.length}>
        {included.map((reference) => {
          const asset = reference.asset
          const group = included.filter((item) => item.role === reference.role)
          const groupIndex = group.findIndex(
            (item) => item.client_id === reference.client_id
          )
          const mediaType = MEDIA_TYPES.find(
            (type) => type.role === reference.role
          )
          const Icon = mediaType?.Icon ?? FileImage
          const name =
            asset?.original_filename ||
            t('Unavailable reference {{id}}', { id: reference.asset_id })
          const content =
            asset?.content_url ||
            (asset ? getVideoAssetContentUrl(asset.id) : undefined)
          const thumbnail = asset?.mime_type.startsWith('image/')
            ? content
            : asset?.poster_url
          const image = signedStudioMediaUrl(
            thumbnail,
            mediaQuery.data?.urls,
            mediaQuery.data?.deniedPaths,
            mediaQuery.identityDenied
          )
          const ready = asset?.state === 'ready'
          let message = ''
          if (
            !asset ||
            asset.state === 'deleted' ||
            asset.state === 'deleting'
          ) {
            message = t(
              'Reference unavailable. Replace or remove it to continue.'
            )
          } else if (asset.state === 'failed') {
            message = asset.failure_reason || t('videoStudio.inspectFailed')
          } else if (!ready) message = t('videoStudio.inspecting')
          return (
            <li
              key={reference.client_id}
              className='video-studio-reference-card'
            >
              <button
                type='button'
                className='video-studio-reference-thumbnail'
                disabled={!ready}
                onClick={() => {
                  setMediaFailed(false)
                  setPreviewId(reference.client_id)
                }}
                aria-label={t('Preview {{name}}', { name })}
              >
                {image && ready ? (
                  <img src={image} alt='' className='size-full object-cover' />
                ) : (
                  <Icon aria-hidden='true' />
                )}
                <span className='video-studio-reference-kind'>
                  {labels[reference.role]} {groupIndex + 1}
                </span>
                {ready && asset && asset.duration_seconds > 0 && (
                  <span className='video-studio-reference-duration'>
                    {t('{{seconds}} s', {
                      seconds: number.format(asset.duration_seconds),
                    })}
                  </span>
                )}
                {ready && (
                  <span className='video-studio-reference-preview'>
                    <Play aria-hidden='true' />
                  </span>
                )}
              </button>
              <div className='video-studio-reference-card-body'>
                <p
                  className='video-studio-reference-filename truncate'
                  title={name}
                >
                  {name}
                </p>
                {message && (
                  <p className='video-studio-reference-error text-destructive text-xs'>
                    {message}
                  </p>
                )}
                <div className='video-studio-reference-actions flex items-center justify-between gap-1'>
                  <div className='flex items-center'>
                    <Button
                      type='button'
                      size='icon-xs'
                      variant='ghost'
                      disabled={busy || groupIndex === 0}
                      onClick={() => move(reference, -1)}
                      aria-label={t('Move {{name}} earlier', { name })}
                    >
                      <ArrowLeft aria-hidden='true' />
                    </Button>
                    <Button
                      type='button'
                      size='icon-xs'
                      variant='ghost'
                      disabled={busy || groupIndex === group.length - 1}
                      onClick={() => move(reference, 1)}
                      aria-label={t('Move {{name}} later', { name })}
                    >
                      <ArrowRight aria-hidden='true' />
                    </Button>
                  </div>
                  <DropdownMenu>
                    <DropdownMenuTrigger
                      render={
                        <Button
                          type='button'
                          size='icon-xs'
                          variant='ghost'
                          disabled={busy}
                          aria-label={t('Options for {{name}}', { name })}
                        />
                      }
                    >
                      <MoreHorizontal aria-hidden='true' />
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align='end'>
                      <DropdownMenuGroup>
                        <DropdownMenuItem
                          disabled={!mediaType || !policy?.[mediaType.policy]}
                          onClick={() => {
                            if (mediaType) {
                              openUpload(mediaType.role, reference.client_id)
                            }
                          }}
                        >
                          {t('Replace reference')}
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          onClick={() =>
                            props.onChange(
                              ordered.map((item) =>
                                item.client_id === reference.client_id
                                  ? { ...item, included: false }
                                  : item
                              )
                            )
                          }
                        >
                          {t('Exclude from this generation')}
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          variant='destructive'
                          onClick={() =>
                            props.onChange(
                              orderedReferences(
                                ordered.filter(
                                  (item) =>
                                    item.client_id !== reference.client_id
                                )
                              )
                            )
                          }
                        >
                          {t('Remove from draft')}
                        </DropdownMenuItem>
                      </DropdownMenuGroup>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              </div>
            </li>
          )
        })}
        {included.length === 0 && (
          <li className='video-studio-reference-empty'>
            <Button
              type='button'
              variant='outline'
              className='w-full border-dashed'
              disabled={busy || !policy?.max_total_count}
              onClick={() => setAddOpen(true)}
            >
              <Plus aria-hidden='true' />
              {t('Add reference media')}
            </Button>
          </li>
        )}
      </ul>
      {excluded.length > 0 && (
        <details className='video-studio-unused-references'>
          <summary>
            {t('Not used in this generation ({{count}})', {
              count: excluded.length,
            })}
          </summary>
          <ul className='flex flex-col gap-2'>
            {excluded.map((reference) => {
              const name =
                reference.asset?.original_filename ||
                t('Unavailable reference {{id}}', { id: reference.asset_id })
              return (
                <li
                  key={reference.client_id}
                  className='flex min-w-0 items-center gap-2'
                >
                  <span className='min-w-0 flex-1 truncate' title={name}>
                    {name}
                  </span>
                  <Button
                    type='button'
                    size='sm'
                    variant='ghost'
                    disabled={busy}
                    onClick={() =>
                      props.onChange(
                        ordered.map((item) =>
                          item.client_id === reference.client_id
                            ? { ...item, included: true }
                            : item
                        )
                      )
                    }
                  >
                    {t('Restore reference')}
                  </Button>
                  <Button
                    type='button'
                    size='sm'
                    variant='ghost'
                    disabled={busy}
                    onClick={() =>
                      props.onChange(
                        orderedReferences(
                          ordered.filter(
                            (item) => item.client_id !== reference.client_id
                          )
                        )
                      )
                    }
                  >
                    {t('Remove from draft')}
                  </Button>
                </li>
              )
            })}
          </ul>
        </details>
      )}
      <Dialog
        open={addOpen}
        onOpenChange={(open) => {
          if (!open) closeUpload()
        }}
      >
        <DialogContent
          className='video-studio-reference-dialog sm:max-w-xl'
          showCloseButton={!uploadBusy}
        >
          <DialogHeader>
            <DialogTitle>
              {target?.replaceId
                ? t('Replace reference')
                : t('Add reference media')}
            </DialogTitle>
            <DialogDescription>
              {target?.replaceId
                ? t(
                    'The original reference stays in your draft until the replacement is ready.'
                  )
                : t(
                    'Choose a media type, upload files, then add the checked media to your draft.'
                  )}
            </DialogDescription>
          </DialogHeader>
          {!target && (
            <div className='video-studio-reference-type-grid grid grid-cols-3 gap-3'>
              {MEDIA_TYPES.map((type) => {
                const config = policy?.[type.policy]
                const count = included.filter(
                  (item) => item.role === type.role
                ).length
                const available = Boolean(
                  config &&
                  count < config.max_count &&
                  policy?.max_total_count &&
                  included.length < policy.max_total_count
                )
                return (
                  <Button
                    key={type.role}
                    type='button'
                    variant='outline'
                    className='h-auto flex-col gap-2 py-5'
                    disabled={!available || props.disabled}
                    onClick={() => openUpload(type.role)}
                  >
                    <type.Icon aria-hidden='true' />
                    {labels[type.role]}
                  </Button>
                )
              })}
            </div>
          )}
          {target && selectedType && selectedPolicy && (
            <>
              <p className='text-muted-foreground text-xs'>
                {t('Up to {{count}} files, {{size}} MB each.', {
                  count: maxFiles,
                  size: number.format(
                    selectedPolicy.max_size_bytes / 1_000_000
                  ),
                })}
              </p>
              <VideoAssetUploader
                key={`${target.role}:${target.replaceId ?? 'add'}`}
                cancelRef={uploaderRef}
                assets={uploads}
                onAssetsChange={setUploads}
                purpose={selectedType.purpose}
                maxFiles={maxFiles}
                maxSizeBytes={selectedPolicy.max_size_bytes}
                accept={selectedPolicy.allowed_mime_types}
                label={t('Choose files')}
                compact
                adminUpload={props.admin}
                disabled={props.disabled || maxFiles < 1}
                onBusyChange={setUploadBusy}
              />
            </>
          )}
          <DialogFooter>
            <Button type='button' variant='outline' onClick={closeUpload}>
              {t('videoStudio.cancel')}
            </Button>
            {target && (
              <Button
                type='button'
                disabled={
                  !readyToApply || props.disabled || maxFiles < uploads.length
                }
                onClick={() => {
                  if (!readyToApply) return
                  props.onChange(orderedReferences(proposedReferences))
                  closeUpload()
                }}
              >
                {target.replaceId ? t('Replace reference') : t('Add to draft')}
              </Button>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog
        open={preview !== undefined}
        onOpenChange={(open) => {
          if (!open) setPreviewId(null)
        }}
      >
        <DialogContent className='video-studio-reference-dialog video-studio-reference-preview-dialog sm:max-w-4xl'>
          <DialogHeader>
            <DialogTitle>
              {previewAsset?.original_filename || t('Reference media')}
            </DialogTitle>
            <DialogDescription>
              {preview ? labels[preview.role] : t('Reference media')}
            </DialogDescription>
          </DialogHeader>
          {previewSource &&
            !mediaFailed &&
            previewAsset?.mime_type.startsWith('image/') && (
              <img
                className='max-h-[65dvh] w-full object-contain'
                src={previewSource}
                alt={previewAsset.original_filename}
                onError={() => setMediaFailed(true)}
              />
            )}
          {previewSource &&
            !mediaFailed &&
            previewAsset?.mime_type.startsWith('video/') && (
              <video
                className='max-h-[65dvh] w-full'
                src={previewSource}
                controls
                playsInline
                preload='metadata'
                onPlay={pauseOtherMedia}
                onError={() => setMediaFailed(true)}
                aria-label={previewAsset.original_filename}
              />
            )}
          {previewSource &&
            !mediaFailed &&
            previewAsset?.mime_type.startsWith('audio/') && (
              <audio
                className='w-full'
                src={previewSource}
                controls
                preload='metadata'
                onPlay={pauseOtherMedia}
                onError={() => setMediaFailed(true)}
                aria-label={previewAsset.original_filename}
              />
            )}
          {(!previewSource || mediaFailed) && (
            <p className='text-muted-foreground text-sm' role='status'>
              {t('Media preview is unavailable. Refresh to try again.')}
            </p>
          )}
          <Button
            type='button'
            variant='outline'
            disabled={mediaQuery.isFetching}
            onClick={() => {
              void mediaQuery.refetch().then(() => setMediaFailed(false))
            }}
          >
            <RefreshCw aria-hidden='true' />
            {t('videoStudio.refresh')}
          </Button>
        </DialogContent>
      </Dialog>
    </section>
  )
}
