/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { zodResolver } from '@hookform/resolvers/zod'
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import { FormProvider, useForm } from 'react-hook-form'
import { expect, test, vi } from 'vitest'

import {
  createVideoModelProfileFormValues,
  getVideoModelPreset,
  parseVideoModelProfileForm,
  videoModelProfileFormSchema,
  type VideoModelProfileFormValues,
} from '../schemas'
import type { VideoModelProfile, VideoModelProfileInput } from '../types'
import { VideoModelSpecEditor } from './video-model-spec-editor'

function Fixture(props: {
  profile: VideoModelProfile
  onSave: (value: VideoModelProfileInput) => void
}) {
  const form = useForm<VideoModelProfileFormValues>({
    resolver: zodResolver(videoModelProfileFormSchema),
    defaultValues: createVideoModelProfileFormValues(props.profile),
  })
  return (
    <FormProvider {...form}>
      <form
        onSubmit={form.handleSubmit((values) =>
          props.onSave(parseVideoModelProfileForm(values, props.profile))
        )}
      >
        <VideoModelSpecEditor />
        <button type='submit'>Save profile</button>
      </form>
    </FormProvider>
  )
}

test('upgrades legacy reference settings and saves independently edited media limits', async () => {
  const preset = getVideoModelPreset('sd_2.0_special_720p_with_video_ref')
  expect(preset).toBeDefined()
  if (!preset) throw new Error('Expected the known Seedance preset')
  const profile: VideoModelProfile = {
    ...preset,
    id: 1,
    specification_version: 3,
    specification: {
      ...preset.specification,
      version: 3,
      modes: ['image_to_video'],
      reference_policy: undefined,
    },
    default_parameters: {
      ...preset.default_parameters,
      duration: 8,
      generate_audio: false,
    },
    enabled: false,
    created_at: 1,
    updated_at: 1,
  }
  const onSave = vi.fn()
  render(<Fixture profile={profile} onSave={onSave} />)
  expect(
    screen.queryByRole('group', {
      name: 'videoStudio.admin.referenceType.video',
    })
  ).not.toBeInTheDocument()
  fireEvent.click(
    screen.getByRole('button', { name: 'Apply reference preset' })
  )
  const images = within(
    screen.getByRole('group', { name: 'videoStudio.admin.referenceType.image' })
  )
  const videos = within(
    screen.getByRole('group', { name: 'videoStudio.admin.referenceType.video' })
  )
  const audio = within(
    screen.getByRole('group', { name: 'videoStudio.admin.referenceType.audio' })
  )
  fireEvent.change(images.getByLabelText('Maximum files'), {
    target: { value: '7' },
  })
  fireEvent.change(videos.getByLabelText('Maximum files'), {
    target: { value: '2' },
  })
  fireEvent.change(videos.getByLabelText('Maximum total duration (seconds)'), {
    target: { value: '14' },
  })
  fireEvent.change(videos.getByLabelText('Maximum duration (seconds)'), {
    target: { value: '10' },
  })
  fireEvent.change(audio.getByLabelText('Maximum files'), {
    target: { value: '3' },
  })
  fireEvent.change(audio.getByLabelText('Maximum total duration (seconds)'), {
    target: { value: '13' },
  })
  fireEvent.change(audio.getByLabelText('Maximum duration (seconds)'), {
    target: { value: '11' },
  })
  fireEvent.click(screen.getByRole('button', { name: 'Save profile' }))
  await waitFor(() => expect(onSave).toHaveBeenCalledOnce())
  expect(onSave.mock.calls[0][0]).toMatchObject({
    enabled: false,
    default_parameters: { duration: 8, generate_audio: false },
    specification: {
      version: 4,
      modes: ['reference_to_video'],
      reference_policy: {
        max_total_count: null,
        image: { max_count: 7 },
        video: { max_count: 2, max_total_duration_seconds: 14 },
        audio: { max_count: 3, max_total_duration_seconds: 13 },
      },
    },
  })
})
