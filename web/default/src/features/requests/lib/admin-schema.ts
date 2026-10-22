import { z } from 'zod'

export const requestReviewSchema = z.object({
  feedback: z.string().trim().min(1).max(2000),
})

export type RequestReviewValues = z.infer<typeof requestReviewSchema>
