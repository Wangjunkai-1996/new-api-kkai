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
import * as z from 'zod'

export const IMAGE_PRICING_OPTION_KEY = 'ImagePricingPolicy'

const MANAGED_MODEL = 'gpt-image-2'
const INVALID_POLICY_MESSAGE = 'imageStudio.admin.pricing.invalidPolicy'
const INVALID_PRICE_MESSAGE = 'imageStudio.validation.pricePositive'

const imagePricingTierSchema = z
  .object({
    unit_price: z.number().finite().positive(),
    sizes: z.array(z.string()).min(1),
  })
  .passthrough()

const imagePricingModelSchema = z
  .object({
    default_size: z.string().min(1),
    tiers: z.record(z.string(), imagePricingTierSchema),
  })
  .passthrough()

const imagePricingPolicySchema = z
  .object({
    version: z.string().min(1),
    enabled: z.boolean(),
    models: z.record(z.string(), imagePricingModelSchema),
  })
  .passthrough()

const managedPriceSchema = z
  .number({ error: INVALID_PRICE_MESSAGE })
  .finite(INVALID_PRICE_MESSAGE)
  .positive(INVALID_PRICE_MESSAGE)

export const imagePricingFormSchema = z.object({
  enabled: z.boolean(),
  price1k: managedPriceSchema,
  price2k: managedPriceSchema,
  price4k: managedPriceSchema,
})

export type ImagePricingPolicy = z.infer<typeof imagePricingPolicySchema>
export type ImagePricingFormValues = z.infer<typeof imagePricingFormSchema>

type ManagedTiers = {
  '1k': z.infer<typeof imagePricingTierSchema>
  '2k': z.infer<typeof imagePricingTierSchema>
  '4k': z.infer<typeof imagePricingTierSchema>
}

type ManagedPricing = {
  model: z.infer<typeof imagePricingModelSchema>
  tiers: ManagedTiers
}

function getManagedPricing(policy: ImagePricingPolicy): ManagedPricing {
  const model = policy.models[MANAGED_MODEL]
  const tiers = model?.tiers
  if (!tiers?.['1k'] || !tiers['2k'] || !tiers['4k']) {
    throw new Error(INVALID_POLICY_MESSAGE)
  }

  return {
    model,
    tiers: {
      '1k': tiers['1k'],
      '2k': tiers['2k'],
      '4k': tiers['4k'],
    },
  }
}

export function parseImagePricingPolicy(raw: string): ImagePricingPolicy {
  let decoded: unknown
  try {
    decoded = JSON.parse(raw)
  } catch {
    throw new Error(INVALID_POLICY_MESSAGE)
  }

  const parsed = imagePricingPolicySchema.safeParse(decoded)
  if (!parsed.success) {
    throw new Error(INVALID_POLICY_MESSAGE)
  }

  getManagedPricing(parsed.data)
  return parsed.data
}

export function getImagePricingFormValues(
  policy: ImagePricingPolicy
): ImagePricingFormValues {
  const { tiers } = getManagedPricing(policy)
  return {
    enabled: policy.enabled,
    price1k: tiers['1k'].unit_price,
    price2k: tiers['2k'].unit_price,
    price4k: tiers['4k'].unit_price,
  }
}

export function updateImagePricingPolicy(
  policy: ImagePricingPolicy,
  values: ImagePricingFormValues,
  version = new Date().toISOString()
): ImagePricingPolicy {
  const validated = imagePricingFormSchema.parse(values)
  const { model, tiers } = getManagedPricing(policy)

  return {
    ...policy,
    version,
    enabled: validated.enabled,
    models: {
      ...policy.models,
      [MANAGED_MODEL]: {
        ...model,
        tiers: {
          ...model.tiers,
          '1k': { ...tiers['1k'], unit_price: validated.price1k },
          '2k': { ...tiers['2k'], unit_price: validated.price2k },
          '4k': { ...tiers['4k'], unit_price: validated.price4k },
        },
      },
    },
  }
}
