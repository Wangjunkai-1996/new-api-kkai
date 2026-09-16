export const VIDEO_SAMPLE_CATEGORIES = [
  'people',
  'animals',
  'nature',
  'animation',
  'product',
  'architecture',
  'food',
  'effects',
  'other',
] as const

export type VideoSampleCategory = (typeof VIDEO_SAMPLE_CATEGORIES)[number]

export const VIDEO_SAMPLE_CATEGORY_LABEL_KEYS: Record<
  VideoSampleCategory,
  string
> = {
  people: 'videoStudio.category.people',
  animals: 'videoStudio.category.animals',
  nature: 'videoStudio.category.nature',
  animation: 'videoStudio.category.animation',
  product: 'videoStudio.category.product',
  architecture: 'videoStudio.category.architecture',
  food: 'videoStudio.category.food',
  effects: 'videoStudio.category.effects',
  other: 'videoStudio.category.other',
}
