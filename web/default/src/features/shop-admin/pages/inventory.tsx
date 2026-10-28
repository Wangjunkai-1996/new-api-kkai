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
import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import { queryString, shopPath, shopRequest } from '@/features/shop/api'
import {
  Pagination,
  ShopErrorNotice,
  ShopHeading,
  ShopLoading,
  ShopState,
} from '@/features/shop/components/common'
import {
  usePrivateResult,
  useShopAction,
  useShopIdentity,
  useShopQuery,
} from '@/features/shop/hooks/use-shop'
import {
  beginRecovery,
  finishRecovery,
  readRecovery,
  type RecoveryMarker,
} from '@/features/shop/recovery'
import {
  importPreviewSchema,
  inventorySchema,
  pageSchema,
  productSchema,
  recoverySchema,
} from '@/features/shop/schemas'

import { AdminActionDialog } from '../components/action-dialog'
import { AdminPrivateReveal } from '../components/private-reveal'

const rowsSchema = z
  .array(
    z.object({
      identity: z
        .string()
        .trim()
        .min(1)
        .max(4096)
        .refine(
          (value) =>
            !['\r', '\n', '\0'].some((character) => value.includes(character))
        ),
      secret_fields: z
        .array(
          z.object({
            key: z.string().min(1).max(191),
            value: z.string().min(1).max(8000),
          })
        )
        .min(1)
        .max(20),
    })
  )
  .min(1)
  .max(200)
const commitSchema = z.object({
  batch_id: z.string(),
  inserted_count: z.number().int(),
})

export function AdminInventoryPage(props: { productId?: string } = {}) {
  const { t } = useTranslation()
  const identity = useShopIdentity()
  const [productId, setProductId] = useState(props.productId ?? '')
  const [state, setState] = useState('')
  const [page, setPage] = useState(1)
  const products = useShopQuery(
    '/api/admin/shop/products?page_size=100',
    pageSchema(productSchema)
  )
  const inventory = useShopQuery(
    `/api/admin/shop/inventory${queryString({
      product_id: productId,
      state,
      page,
    })}`,
    pageSchema(inventorySchema)
  )
  return (
    <>
      <ShopHeading
        title={t('Inventory')}
        description={t('Only masked identifiers appear in the inventory list.')}
      />
      <div className='shop-filters'>
        <Field>
          <FieldLabel htmlFor='inventory-product'>{t('Product')}</FieldLabel>
          <NativeSelect
            id='inventory-product'
            value={productId}
            onChange={(event) => {
              setProductId(event.target.value)
              setPage(1)
            }}
          >
            <NativeSelectOption value=''>{t('All')}</NativeSelectOption>
            {products.data?.items.map((product) => (
              <NativeSelectOption key={product.id} value={product.id}>
                {product.name}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </Field>
        <Field>
          <FieldLabel htmlFor='inventory-state'>{t('Status')}</FieldLabel>
          <NativeSelect
            id='inventory-state'
            value={state}
            onChange={(event) => {
              setState(event.target.value)
              setPage(1)
            }}
          >
            <NativeSelectOption value=''>{t('All')}</NativeSelectOption>
            {[
              ['available', t('Available')],
              ['reserved', t('Reserved')],
              ['delivered', t('Delivered')],
              ['quarantined', t('Quarantined')],
            ].map(([value, label]) => (
              <NativeSelectOption key={value} value={value}>
                {label}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </Field>
      </div>
      <ShopErrorNotice error={inventory.error ?? products.error} />
      {inventory.isPending && <ShopLoading />}
      <div className='shop-table-wrap'>
        <table className='shop-table'>
          <thead>
            <tr>
              <th>{t('Identifier')}</th>
              <th>{t('Status')}</th>
              <th>{t('Actions')}</th>
            </tr>
          </thead>
          <tbody>
            {inventory.data?.items.map((unit) => (
              <tr key={unit.id}>
                <td>
                  {unit.masked_identifier}
                  <p className='shop-muted'>{unit.id}</p>
                </td>
                <td>
                  <ShopState state={unit.state} />
                </td>
                <td>
                  <div className='shop-stack'>
                    {unit.allowed_actions.includes('quarantine') && (
                      <AdminActionDialog
                        label={t('Quarantine')}
                        description={t(
                          'Quarantined inventory cannot be allocated to a new order.'
                        )}
                        path={`/api/admin/shop/inventory/${shopPath(
                          unit.id
                        )}/quarantine`}
                        schema={inventorySchema}
                        fields={[{ name: 'reason', label: t('Reason') }]}
                        body={(values) => ({
                          expected_version: unit.version,
                          reason: values.reason,
                        })}
                      />
                    )}
                    {unit.allowed_actions.includes('reveal') && (
                      <AdminPrivateReveal
                        path={`/api/admin/shop/inventory/${shopPath(
                          unit.id
                        )}/reveal`}
                        target={unit.id}
                        version={unit.version}
                        scope='shop.inventory.reveal'
                      />
                    )}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {inventory.data && (
        <Pagination
          page={page}
          total={inventory.data.total}
          pageSize={inventory.data.page_size}
          onPage={setPage}
        />
      )}
      {productId && (
        <InventoryImport
          key={`${identity}:${productId}`}
          productId={productId}
        />
      )}
    </>
  )
}

function InventoryImport(props: { productId: string }) {
  const { t } = useTranslation()
  const identity = useShopIdentity()
  const queryClient = useQueryClient()
  const [text, setText] = useState('')
  const [advancedText, setAdvancedText] = useState('')
  const [invalid, setInvalid] = useState(false)
  const [marker, setMarker] = useState<RecoveryMarker | null>(() =>
    readRecovery('import-commit', props.productId)
  )
  const rows = useRef<z.infer<typeof rowsSchema> | null>(null)
  const preview = usePrivateResult<z.infer<typeof importPreviewSchema>>(
    props.productId
  )
  const recovery = usePrivateResult<z.infer<typeof recoverySchema>>(
    props.productId
  )
  const action = useShopAction()
  const product = useShopQuery(
    `/api/admin/shop/products/${shopPath(props.productId)}`,
    productSchema
  )
  useEffect(() => {
    const clear = () => {
      setText('')
      setAdvancedText('')
      rows.current = null
    }
    const hide = () => {
      if (document.hidden) clear()
    }
    document.addEventListener('visibilitychange', hide)
    window.addEventListener('pagehide', clear)
    return () => {
      rows.current = null
      document.removeEventListener('visibilitychange', hide)
      window.removeEventListener('pagehide', clear)
    }
  }, [identity, props.productId])
  const resolve = async () => {
    if (!marker) return
    const value = await recovery.read((signal) =>
      shopRequest(
        `/api/admin/shop/inventory/import-requests/${shopPath(
          marker.key
        )}?product_id=${shopPath(props.productId)}`,
        recoverySchema,
        { signal }
      )
    )
    if (value?.status === 'recorded') {
      finishRecovery('import-commit', props.productId)
      setMarker(null)
      setText('')
      setAdvancedText('')
      rows.current = null
      preview.clear()
      await queryClient.invalidateQueries({ queryKey: ['private-user'] })
    }
  }
  const deliveryFields =
    product.data?.delivery_fields.map((field) => field.key) ?? []
  let format: 'account' | 'card' | null = null
  if (product.data?.delivery_type === 'automatic') {
    if (
      product.data.product_type === 'account' &&
      deliveryFields.length === 2 &&
      deliveryFields.includes('account_identifier') &&
      deliveryFields.includes('password')
    ) {
      format = 'account'
    } else if (
      product.data.product_type === 'card' &&
      deliveryFields.length === 1 &&
      deliveryFields[0] === 'card_code'
    ) {
      format = 'card'
    }
  }

  const previewImport = async (advanced: boolean) => {
    setInvalid(false)
    try {
      const source = advanced ? advancedText : text
      if (new TextEncoder().encode(source).length > 1048576) throw new Error()
      let parsed: z.infer<typeof rowsSchema>
      if (advanced) {
        parsed = rowsSchema.parse(JSON.parse(source))
      } else {
        if (!format) throw new Error()
        const lines = source.replaceAll('\r\n', '\n').split('\n')
        while (lines.at(-1) === '') lines.pop()
        if (lines.some((line) => line.includes('\r') || line.includes('\0'))) {
          throw new Error()
        }
        parsed = rowsSchema.parse(
          lines.map((line) => {
            if (format === 'card') {
              if (!line.trim() || line.includes('\t')) throw new Error()
              const code = line.trim()
              return {
                identity: code,
                secret_fields: [{ key: 'card_code', value: code }],
              }
            }
            const pair = line.split('\t')
            if (pair.length !== 2 || !pair[0].trim() || !pair[1].trim()) {
              throw new Error()
            }
            return {
              identity: pair[0].trim(),
              secret_fields: [
                { key: 'account_identifier', value: pair[0].trim() },
                { key: 'password', value: pair[1] },
              ],
            }
          })
        )
      }
      if (new TextEncoder().encode(JSON.stringify(parsed)).length > 1048576) {
        throw new Error()
      }
      rows.current = parsed
      await preview.read((signal) =>
        shopRequest(
          '/api/admin/shop/inventory/import-preview',
          importPreviewSchema,
          {
            method: 'POST',
            body: {
              product_id: props.productId,
              product_version: product.data?.version,
              template_version: product.data?.template_version,
              rows: parsed,
            },
            signal,
          }
        )
      )
    } catch {
      rows.current = null
      setInvalid(true)
    }
  }
  return (
    <section className='shop-panel mt-6'>
      <h2>{t('Import inventory')}</h2>
      {product.data?.delivery_type === 'manual' && (
        <p className='shop-muted'>
          {t(
            'Manual delivery products do not use inventory. Deliver from the order.'
          )}
        </p>
      )}
      {format && (
        <>
          <p className='shop-muted'>
            {format === 'account'
              ? t(
                  'One account and password per line, separated by a tab. Maximum 200 lines.'
                )
              : t('One redemption code per line. Maximum 200 lines.')}
          </p>
          <Textarea
            aria-label={
              format === 'account'
                ? t('Accounts and passwords')
                : t('Redemption codes')
            }
            autoComplete='off'
            spellCheck={false}
            rows={8}
            value={text}
            onChange={(event) => {
              setText(event.target.value)
              setInvalid(false)
              rows.current = null
              preview.clear()
            }}
          />
        </>
      )}
      {product.data?.delivery_type === 'automatic' && (
        <details className='mt-4' open={!format}>
          <summary className='cursor-pointer font-semibold'>
            {t('Advanced JSON import')}
          </summary>
          <p className='shop-muted mt-3'>
            {t(
              'Paste a JSON array with identity and secret_fields. Identity is the stable unique account or card identifier. Maximum 200 rows and 1 MiB.'
            )}
          </p>
          <Textarea
            aria-label={t('Inventory data')}
            autoComplete='off'
            spellCheck={false}
            rows={8}
            value={advancedText}
            onChange={(event) => {
              setAdvancedText(event.target.value)
              setInvalid(false)
              rows.current = null
              preview.clear()
            }}
          />
          <Button
            className='mt-3'
            disabled={!product.data || preview.pending || action.pending}
            onClick={() => void previewImport(true)}
          >
            {t('Preview JSON import')}
          </Button>
        </details>
      )}
      {invalid && (
        <p role='alert'>
          {t(
            'Check the import format and size. Sensitive values are not shown in errors.'
          )}
        </p>
      )}
      <div className='shop-actions'>
        {format && (
          <Button
            disabled={!product.data || preview.pending || action.pending}
            onClick={() => void previewImport(false)}
          >
            {t('Preview import')}
          </Button>
        )}
        {marker && (
          <Button variant='outline' onClick={resolve}>
            {t('Recover import result')}
          </Button>
        )}
      </div>
      {preview.value && (
        <div>
          <p>
            {t(
              'Valid: {{valid}} · Duplicate: {{duplicate}} · Invalid: {{invalid}}',
              {
                valid: preview.value.valid_count,
                duplicate: preview.value.duplicate_count,
                invalid: preview.value.invalid_count,
              }
            )}
          </p>
          {preview.value.errors.map((error) => (
            <p key={error.row}>
              {t('Row {{row}}', { row: error.row })}:{' '}
              {error.code === 'DUPLICATE_IDENTITY'
                ? t('Duplicate')
                : t(
                    'Check the required fields and try again. Sensitive values are not shown in errors.'
                  )}
            </p>
          ))}
          <Button
            disabled={
              preview.value.invalid_count > 0 ||
              preview.value.duplicate_count > 0 ||
              action.pending ||
              (Boolean(marker) && recovery.value?.status !== 'not_observed')
            }
            onClick={async () => {
              if (!rows.current || !preview.value || !product.data) return
              const pending =
                marker ?? beginRecovery('import-commit', props.productId)
              setMarker(pending)
              const result = await action.run(
                '/api/admin/shop/inventory/import-commit',
                commitSchema,
                {
                  product_id: props.productId,
                  product_version: product.data.version,
                  template_version: product.data.template_version,
                  preview_digest: preview.value.preview_digest,
                  rows: rows.current,
                },
                { key: pending.key }
              )
              if (result) {
                finishRecovery('import-commit', props.productId)
                setMarker(null)
                setText('')
                setAdvancedText('')
                rows.current = null
                preview.clear()
              }
            }}
          >
            {t('Commit import')}
          </Button>
        </div>
      )}
      {marker && (
        <p role='status'>
          {t(
            'The import is unresolved. Recover its result first; if it is not yet recorded, re-enter the original data and retry with the same request.'
          )}
        </p>
      )}
      <ShopErrorNotice
        error={preview.error ?? recovery.error ?? action.error ?? product.error}
      />
    </section>
  )
}
