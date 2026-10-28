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
import { Link, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import heroMobile from '@/assets/shop/hero-mobile.webp'
import hero from '@/assets/shop/hero.webp'
import guide from '@/assets/shop/service-guide.webp'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'

import { queryString, shopPath } from '../api'
import { productImage } from '../assets'
import {
  MoneyText,
  Pagination,
  ShopEmpty,
  ShopErrorNotice,
  ShopHeading,
  ShopLoading,
} from '../components/common'
import { useShopQuery } from '../hooks/use-shop'
import {
  categorySchema,
  configSchema,
  pageSchema,
  productSchema,
  type Product,
} from '../schemas'

export function CatalogPage(props: {
  q?: string
  category_id?: string
  page: number
}) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [search, setSearch] = useState(props.q ?? '')
  const products = useShopQuery(
    `/api/shop/products${queryString({ q: props.q, category_id: props.category_id, page: props.page })}`,
    pageSchema(productSchema),
    { public: true }
  )
  const categories = useShopQuery(
    '/api/shop/categories',
    z.array(categorySchema),
    { public: true }
  )
  const config = useShopQuery('/api/shop/config', configSchema, {
    public: true,
  })
  const change = (value: { q?: string; category_id?: string; page?: number }) =>
    void navigate({
      to: '/shop',
      search: { q: props.q, category_id: props.category_id, page: 1, ...value },
    })
  return (
    <>
      <section className='shop-hero'>
        <div>
          <p className='shop-eyebrow'>KK SHOP · DIGITAL GOODS</p>
          <h1>{t('Good things, delivered digitally.')}</h1>
          <p className='shop-muted'>
            {t(
              'Thoughtfully selected digital products, with clear terms and support in one place.'
            )}
          </p>
        </div>
        <picture>
          <source media='(max-width:540px)' srcSet={heroMobile} />
          <img
            src={hero}
            alt=''
            width={1536}
            height={1024}
            fetchPriority='high'
          />
        </picture>
      </section>
      {config.data && !config.data.new_orders_enabled && (
        <Alert>
          <AlertDescription>
            {t(
              'New purchases are paused. Your existing orders remain available.'
            )}
          </AlertDescription>
        </Alert>
      )}
      <ShopHeading
        title={t('Explore the collection')}
        description={t('Digital delivery. No shipping address required.')}
      />
      <form
        className='shop-filters'
        onSubmit={(event) => {
          event.preventDefault()
          change({ q: search })
        }}
      >
        <FieldGroup className='flex-row flex-wrap items-end'>
          <Field className='w-auto flex-1'>
            <FieldLabel htmlFor='shop-search'>
              {t('Search products')}
            </FieldLabel>
            <Input
              id='shop-search'
              value={search}
              maxLength={100}
              onChange={(event) => setSearch(event.target.value)}
            />
          </Field>
          <Field className='w-auto'>
            <FieldLabel htmlFor='shop-category'>{t('Category')}</FieldLabel>
            <NativeSelect
              id='shop-category'
              value={props.category_id ?? ''}
              onChange={(event) =>
                change({ category_id: event.target.value || undefined })
              }
            >
              <NativeSelectOption value=''>
                {t('All categories')}
              </NativeSelectOption>
              {categories.data?.map((category) => (
                <NativeSelectOption key={category.id} value={category.id}>
                  {category.name}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </Field>
          <Button type='submit'>{t('Search')}</Button>
        </FieldGroup>
      </form>
      <ShopErrorNotice
        error={products.error ?? categories.error ?? config.error}
        retry={() => {
          void products.refetch()
          void categories.refetch()
          void config.refetch()
        }}
      />
      {products.isPending && <ShopLoading />}
      {products.data?.items.length === 0 && <ShopEmpty />}
      <div className='shop-grid'>
        {products.data?.items.map((product) => (
          <ProductCard key={product.id} product={product} />
        ))}
      </div>
      {products.data && (
        <Pagination
          page={props.page}
          total={products.data.total}
          pageSize={products.data.page_size}
          onPage={(page) => change({ page })}
        />
      )}
      <section className='shop-panel'>
        <div className='shop-columns'>
          <div className='shop-stack'>
            <h2>{t('From choosing to receiving')}</h2>
            <ol className='shop-stack'>
              <li>{t('1. Read the product terms and confirm your order.')}</li>
              <li>{t('2. Pay through an available payment method.')}</li>
              <li>
                {t('3. View delivery details and support in your order.')}
              </li>
            </ol>
          </div>
          <img
            src={guide}
            width={1536}
            height={1024}
            alt=''
            loading='lazy'
            className='max-h-48 w-full object-contain'
          />
        </div>
      </section>
    </>
  )
}

function ProductCard(props: { product: Product }) {
  const { t } = useTranslation()
  const product = props.product
  const image = productImage(product.cover?.url)
  return (
    <Link
      className='shop-card'
      to='/shop/products/$productId'
      params={{ productId: product.id }}
    >
      <div className='shop-card-art'>
        {image ? (
          <img
            src={image}
            width={product.cover?.width ?? 1024}
            height={product.cover?.height ?? 1024}
            alt={product.cover?.alt ?? ''}
            loading='lazy'
          />
        ) : (
          <span>{t('Digital product')}</span>
        )}
      </div>
      <div className='shop-card-content'>
        <Badge variant='outline'>
          {product.delivery_type === 'automatic'
            ? t('Automatic delivery')
            : t('Manual delivery')}
        </Badge>
        <h2>{product.name}</h2>
        <p className='shop-muted line-clamp-2 text-sm'>{product.summary}</p>
        <div className='shop-actions justify-between'>
          <MoneyText money={product.price} />
          <span className='text-sm'>
            {product.availability === 'available'
              ? t('View product')
              : t('Unavailable')}
          </span>
        </div>
      </div>
    </Link>
  )
}

export function ProductPage(props: { productId: string }) {
  const { t } = useTranslation()
  const product = useShopQuery(
    `/api/shop/products/${shopPath(props.productId)}`,
    productSchema,
    { public: true }
  )
  const config = useShopQuery('/api/shop/config', configSchema, {
    public: true,
  })
  if (product.isPending) return <ShopLoading />
  if (!product.data) {
    return (
      <ShopErrorNotice
        error={product.error}
        retry={() => void product.refetch()}
      />
    )
  }
  const data = product.data
  const image = productImage(data.cover?.url)
  return (
    <>
      <ShopHeading
        eyebrow={t('The collection')}
        title={data.name}
        description={data.summary}
      />
      <div className='shop-columns'>
        <div className='shop-stack'>
          {image && (
            <img
              className='shop-product-art'
              src={image}
              width={1024}
              height={1024}
              alt={data.cover?.alt ?? data.name}
            />
          )}
          <ProductTerms product={data} />
        </div>
        <aside className='shop-panel shop-receipt'>
          <Badge variant='secondary'>
            {data.delivery_type === 'automatic'
              ? t('Automatic delivery')
              : t('Manual delivery')}
          </Badge>
          <h2>{data.name}</h2>
          <p>{data.specification}</p>
          <MoneyText money={data.price} className='shop-money text-3xl' />
          <p className='shop-muted'>
            {t('One digital item per order. No delivery fee.')}
          </p>
          {data.can_purchase && config.data?.new_orders_enabled ? (
            <Button
              nativeButton={false}
              render={
                <Link
                  to='/shop/checkout/$productId'
                  params={{ productId: data.id }}
                />
              }
            >
              {t('Buy now')}
            </Button>
          ) : (
            <Alert>
              <AlertDescription>
                {t('This product is currently unavailable.')}
              </AlertDescription>
            </Alert>
          )}
          <ShopErrorNotice error={config.error} />
          <Link to='/shop/orders' search={{ page: 1 }}>
            {t('View my orders')}
          </Link>
        </aside>
      </div>
    </>
  )
}

export function ProductTerms(props: { product: Partial<Product> }) {
  const { t } = useTranslation()
  const product = props.product
  return (
    <section className='shop-panel'>
      <h2>{t('Before you purchase')}</h2>
      {product.usage_conditions && (
        <p className='whitespace-pre-wrap'>{product.usage_conditions}</p>
      )}
      {[
        [t('Delivery terms'), product.delivery_policy?.public_terms],
        [t('Viewing period'), product.view_policy?.public_terms],
        [t('Product validity'), product.entitlement_policy?.public_terms],
        [t('Warranty'), product.warranty_policy?.public_terms],
        [t('Refund policy'), product.refund_policy?.public_terms],
      ].map(
        ([label, value]) =>
          value && (
            <div key={label}>
              <h3 className='font-medium'>{label}</h3>
              <p className='shop-muted whitespace-pre-wrap'>{value}</p>
            </div>
          )
      )}
    </section>
  )
}
