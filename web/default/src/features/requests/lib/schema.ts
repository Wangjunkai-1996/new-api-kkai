import { z } from 'zod'

export const requestSchema = z.object({
  title: z.string().trim().min(1).max(80),
  description: z.string().trim().min(1).max(2000),
})

export type RequestInput = z.infer<typeof requestSchema>
