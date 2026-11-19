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
  ArrowRight02Icon,
  Key01Icon,
  PauseCircleIcon,
  Search01Icon,
  ShoppingBag01Icon,
  Ticket01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { Link, useNavigate } from '@tanstack/react-router'
import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import storefrontArt from '@/assets/shop/storefront-vouchers.webp'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'

import { queryString, shopPath } from '../api'
import { productImage } from '../assets'
import {
  MoneyText,
  Pagination,
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
  const searchInput = useRef<HTMLInputElement>(null)
  useEffect(() => {
    if (searchInput.current) searchInput.current.value = props.q ?? ''
  }, [props.q])
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
  const catalog = products.data
  const filtered = !!(props.q || props.category_id)
  const change = (value: { q?: string; category_id?: string; page?: number }) =>
    void navigate({
      to: '/shop',
      search: { q: props.q, category_id: props.category_id, page: 1, ...value },
      resetScroll: false,
    })
  const clearFilters = () => {
    if (searchInput.current) searchInput.current.value = ''
    change({ q: undefined, category_id: undefined })
  }
  return (
    <>
      <header className='shop-hero'>
        <div className='shop-hero-art' aria-hidden='true'>
          <img
            src={storefrontArt}
            width={1920}
            height={704}
            alt=''
            fetchPriority='high'
          />
        </div>
        <div className='shop-hero-copy'>
          <p className='shop-wordmark' lang='en'>
            KK SHOP
          </p>
          <h1>{t('Digital goods store')}</h1>
          <p className='shop-hero-description'>
            {t(
              'Shop accounts and redemption codes. Find delivery details in your orders.'
            )}
          </p>
          <Link
            to='/shop/orders'
            search={{ page: 1 }}
            className='shop-orders-link'
          >
            {t('My orders')}
            <HugeiconsIcon
              icon={ArrowRight02Icon}
              size={17}
              aria-hidden='true'
            />
          </Link>
        </div>
      </header>
      <section
        className='shop-collection'
        aria-labelledby='shop-collection-title'
      >
        <div className='shop-collection-toolbar'>
          <div className='shop-collection-heading'>
            <h2 id='shop-collection-title'>{t('Product catalog')}</h2>
            {config.data?.new_orders_enabled === false && (
              <p className='shop-purchase-notice' role='status'>
                <HugeiconsIcon
                  icon={PauseCircleIcon}
                  size={17}
                  aria-hidden='true'
                />
                {t(
                  'New purchases are paused. Your existing orders remain available.'
                )}
              </p>
            )}
          </div>
          {(filtered || !!catalog?.total) && (
            <form
              className='shop-catalog-filters'
              role='search'
              aria-label={t('Search products')}
              onSubmit={(event) => {
                event.preventDefault()
                change({ q: searchInput.current?.value.trim() || undefined })
              }}
            >
              <div className='shop-search-field'>
                <Input
                  type='search'
                  name='q'
                  ref={searchInput}
                  defaultValue={props.q}
                  maxLength={100}
                  aria-label={t('Search products')}
                  placeholder={t('Search products')}
                />
                <Button
                  type='submit'
                  variant='ghost'
                  size='icon'
                  aria-label={t('Search')}
                >
                  <HugeiconsIcon icon={Search01Icon} aria-hidden='true' />
                </Button>
              </div>
              {(!!categories.data?.length || !!props.category_id) && (
                <NativeSelect
                  name='category_id'
                  aria-label={t('Category')}
                  value={props.category_id ?? ''}
                  onChange={(event) =>
                    change({
                      category_id: event.target.value || undefined,
                      q: searchInput.current?.value.trim() || undefined,
                    })
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
              )}
              {filtered && !!catalog?.items.length && (
                <Button type='button' variant='ghost' onClick={clearFilters}>
                  {t('Clear filters')}
                </Button>
              )}
            </form>
          )}
        </div>
        <ShopErrorNotice
          error={products.error ?? categories.error ?? config.error}
          retry={() => {
            void products.refetch()
            void categories.refetch()
            void config.refetch()
          }}
        />
        {products.isPending && <ShopLoading />}
        {catalog?.items.length === 0 && !products.isError && (
          <CatalogEmpty
            filtered={filtered}
            page={props.page}
            onReset={props.page > 1 ? () => change({ page: 1 }) : clearFilters}
          />
        )}
        {catalog && catalog.items.length > 0 && (
          <>
            <div className='shop-grid'>
              {catalog.items.map((product) => (
                <ProductCard key={product.id} product={product} />
              ))}
            </div>
            {(props.page > 1 || catalog.total > catalog.page_size) && (
              <Pagination
                page={props.page}
                total={catalog.total}
                pageSize={catalog.page_size}
                onPage={(page) => change({ page })}
              />
            )}
          </>
        )}
      </section>
    </>
  )
}

function CatalogEmpty(props: {
  filtered: boolean
  page: number
  onReset: () => void
}) {
  const { t } = useTranslation()
  let title = t('No products yet')
  let description = t(
    'When products are listed, you can find their prices and delivery details here.'
  )
  let action: string | undefined
  if (props.page > 1) {
    title = t('No products on this page')
    description = t('Return to the first page to see the current selection.')
    action = t('Go to first page')
  } else if (props.filtered) {
    title = t('No products match your search')
    description = t('Try a different keyword or category.')
    action = t('Clear filters')
  }
  return (
    <div className='shop-shelf-empty'>
      <span className='shop-shelf-icon' aria-hidden='true'>
        <HugeiconsIcon
          icon={props.filtered ? Search01Icon : ShoppingBag01Icon}
          size={32}
          strokeWidth={1.45}
        />
      </span>
      <div>
        <h3>{title}</h3>
        <p>{description}</p>
        {action && (
          <Button variant='outline' onClick={props.onReset}>
            {action}
          </Button>
        )}
      </div>
    </div>
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
      {image && (
        <div className='shop-card-art'>
          <img
            src={image}
            width={product.cover?.width ?? 1024}
            height={product.cover?.height ?? 1024}
            alt={product.cover?.alt ?? ''}
            loading='lazy'
          />
        </div>
      )}
      <div className='shop-card-content'>
        {!image && (
          <ProductPlaceholder productType={product.product_type} compact />
        )}
        <div className='shop-card-title'>
          <h3>{product.name}</h3>
          <MoneyText money={product.price} />
        </div>
        {(product.specification || product.summary) && (
          <p className='shop-muted line-clamp-2 text-sm'>
            {product.specification || product.summary}
          </p>
        )}
        <div className='shop-card-footer'>
          {product.delivery_type && (
            <Badge variant='secondary'>
              {product.delivery_type === 'automatic'
                ? t('Automatic delivery')
                : t('Manual delivery')}
            </Badge>
          )}
          <span>
            {product.availability === 'available'
              ? t('View product')
              : t('Currently unavailable')}
            <HugeiconsIcon
              icon={ArrowRight02Icon}
              size={16}
              aria-hidden='true'
            />
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
          {image ? (
            <img
              className='shop-product-art'
              src={image}
              width={1024}
              height={1024}
              alt={data.cover?.alt ?? data.name}
            />
          ) : (
            <div className='shop-product-art'>
              <ProductPlaceholder productType={data.product_type} />
            </div>
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

function ProductPlaceholder(props: {
  productType?: string
  compact?: boolean
}) {
  const { t } = useTranslation()
  let icon = ShoppingBag01Icon
  let label = t('Digital product')
  if (props.productType === 'account') {
    icon = Key01Icon
    label = t('Account')
  } else if (props.productType === 'card') {
    icon = Ticket01Icon
    label = t('Card code')
  }
  return (
    <div
      className={props.compact ? 'shop-card-kind' : 'shop-product-placeholder'}
      data-kind={props.productType}
    >
      <HugeiconsIcon icon={icon} strokeWidth={1.5} aria-hidden='true' />
      <span>{label}</span>
    </div>
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
