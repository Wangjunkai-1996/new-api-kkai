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
import product1 from '@/assets/shop/product-1.webp'
import product2 from '@/assets/shop/product-2.webp'
import product3 from '@/assets/shop/product-3.webp'
import product4 from '@/assets/shop/product-4.webp'
import product5 from '@/assets/shop/product-5.webp'
import product6 from '@/assets/shop/product-6.webp'

import { safeShopImage } from './lib'

export const shopArt = {
  'shop:product-1': product1,
  'shop:product-2': product2,
  'shop:product-3': product3,
  'shop:product-4': product4,
  'shop:product-5': product5,
  'shop:product-6': product6,
}

export function productImage(reference?: string) {
  if (reference?.startsWith('/shop-assets/product-')) {
    reference = `shop:${reference.split('/').pop()?.replace('.webp', '')}`
  }
  if (reference && reference in shopArt) {
    return shopArt[reference as keyof typeof shopArt]
  }
  return safeShopImage(reference)
}
