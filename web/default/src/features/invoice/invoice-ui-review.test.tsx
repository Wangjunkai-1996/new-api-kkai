import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import * as invoiceApi from './api'
import { InvoiceDetail } from './components/detail'
import { InvoiceFileButton } from './components/files'
import { DraftContext, freshInvoiceDraft } from './lib/draft'
import { InvoiceRecordRow, InvoiceRecords } from './records'
import type { InvoiceAttachment, InvoiceRequest } from './types'
import { InvoiceWorkspace } from './workspace'

const location = vi.hoisted(() => ({
  pathname: '/invoices/new',
  search: {} as { tab?: string },
}))

vi.mock('@tanstack/react-router', async (original) => ({
  ...(await original<typeof import('@tanstack/react-router')>()),
  useNavigate: () => vi.fn(),
  useLocation: (options: { select: (value: typeof location) => unknown }) =>
    options.select(location),
  useBlocker: () => {},
  Outlet: () => null,
  Link: (props: {
    children: React.ReactNode
    to: string
    role?: React.AriaRole
    ref?: React.Ref<HTMLAnchorElement>
  }) => (
    <a href={props.to} role={props.role} ref={props.ref}>
      {props.children}
    </a>
  ),
}))

const file: InvoiceAttachment = {
  id: 'file_1',
  file_name: 'invoice.png',
  mime_type: 'image/png',
  size: 100,
}
const invoice: InvoiceRequest = {
  public_id: 'request_1',
  display_no: 'INV-TEST-1',
  status: 'pending_review',
  version: 1,
  profile: {
    kind: 'company',
    title: 'Example Company',
    tax_id: 'example-tax-id',
    email: 'invoice@example.test',
  },
  seller_id: 'seller',
  seller_name: 'Example Seller',
  invoice_type: 'ordinary',
  amount_minor: '12000',
  currency: 'CNY',
  currency_exponent: 2,
  items_count: 1,
  source_summary: ['topup'],
  created_at: '2026-10-22T00:00:00Z',
  updated_at: '2026-10-22T00:00:00Z',
  current_delivery_version: 0,
  allowed_actions: [],
  username: 'invoice-customer',
  user_id: 42,
}

beforeEach(() => {
  Object.defineProperty(HTMLElement.prototype, 'getAnimations', {
    configurable: true,
    value: () => [],
  })
  vi.stubGlobal(
    'URL',
    class extends URL {
      static createObjectURL = vi.fn().mockReturnValue('blob:invoice-preview')
      static revokeObjectURL = vi.fn()
    }
  )
})
afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

test('a decoded image failure offers retry and an authorized download in the preview', async () => {
  const fetch = vi
    .spyOn(invoiceApi, 'fetchInvoiceFile')
    .mockResolvedValue(new Blob(['image'], { type: 'image/png' }))
  const download = vi
    .spyOn(HTMLAnchorElement.prototype, 'click')
    .mockImplementation(() => {})
  const user = userEvent.setup()
  render(<InvoiceFileButton requestId='request_1' file={file} />)
  await user.click(screen.getByRole('button', { name: 'Preview' }))
  const dialog = await screen.findByRole('dialog')
  fireEvent.error(within(dialog).getByRole('img'))
  expect(within(dialog).getByRole('alert')).toHaveTextContent(
    'Failed to load image'
  )
  expect(within(dialog).queryByRole('img')).not.toBeInTheDocument()
  await user.click(within(dialog).getByRole('button', { name: 'Retry' }))
  expect(await within(dialog).findByRole('img')).toBeInTheDocument()
  expect(within(dialog).queryByRole('alert')).not.toBeInTheDocument()
  await user.click(within(dialog).getByRole('button', { name: 'Download' }))
  expect(fetch).toHaveBeenLastCalledWith('request_1', 'file_1', false, true)
  expect(download).toHaveBeenCalledOnce()
})

test('PDF previews retain independent reload and download controls', async () => {
  const fetch = vi
    .spyOn(invoiceApi, 'fetchInvoiceFile')
    .mockResolvedValue(new Blob(['pdf'], { type: 'application/pdf' }))
  const user = userEvent.setup()
  render(
    <InvoiceFileButton
      requestId='request_1'
      file={{ ...file, file_name: 'invoice.pdf', mime_type: 'application/pdf' }}
      admin
    />
  )
  await user.click(screen.getByRole('button', { name: 'Preview' }))
  const dialog = await screen.findByRole('dialog')
  expect(within(dialog).getByTitle('Invoice preview')).toHaveAttribute(
    'sandbox',
    ''
  )
  expect(within(dialog).getByRole('button', { name: 'Download' })).toBeEnabled()
  await user.click(within(dialog).getByRole('button', { name: 'Refresh' }))
  expect(fetch).toHaveBeenCalledTimes(2)
  expect(fetch).toHaveBeenLastCalledWith('request_1', 'file_1', true, false)
})

test('only administrator queue rows show the applicant identity', () => {
  const row = render(
    <InvoiceRecordRow invoice={invoice} selected={false} onSelect={() => {}} />
  )
  expect(screen.queryByText('User: invoice-customer')).not.toBeInTheDocument()
  row.rerender(
    <InvoiceRecordRow
      invoice={invoice}
      selected={false}
      onSelect={() => {}}
      admin
    />
  )
  expect(screen.getByText('User: invoice-customer')).toHaveAttribute(
    'title',
    'invoice-customer'
  )
})

test('invoice feedback shows its category and submission time', () => {
  render(
    <InvoiceDetail
      admin
      invoice={{
        ...invoice,
        status: 'disputed',
        active_dispute: {
          id: 'dispute_1',
          delivery_version: 1,
          round: 1,
          category: 'attachment',
          description: 'The delivered file does not open.',
          status: 'open',
          created_at: '2026-10-22T03:04:00Z',
        },
      }}
    />
  )
  expect(screen.getByText(/Attachment cannot be opened/)).toHaveTextContent(
    '10/22/2026'
  )
  expect(screen.getByText('The delivered file does not open.')).toBeVisible()
})

test('invoice record pagination honors the requested page size', async () => {
  vi.spyOn(invoiceApi, 'getInvoices').mockResolvedValue({
    items: [invoice],
    total: 30,
    p: 1,
    page_size: 50,
  })
  vi.spyOn(invoiceApi, 'getInvoice').mockResolvedValue(invoice)
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const view = render(
    <QueryClientProvider client={client}>
      <DraftContext.Provider
        value={{ draft: freshInvoiceDraft(), setDraft: vi.fn() }}
      >
        <InvoiceRecords filters={{ p: 1, page_size: 50 }} />
      </DraftContext.Provider>
    </QueryClientProvider>
  )
  expect(
    await screen.findByRole('button', { name: /INV-TEST-1/ })
  ).toBeVisible()
  expect(screen.queryByRole('navigation', { name: 'Pagination' })).toBeNull()
  view.unmount()
  client.clear()
})

test('the current application page has no duplicate apply button and other invoice pages retain the entry', async () => {
  vi.spyOn(invoiceApi, 'getInvoicePolicy').mockResolvedValue({
    enabled: true,
    policy_version: 'v1',
    seller_id: 'seller',
    seller_name: 'Example Seller',
    currencies: [{ currency: 'CNY', exponent: 2 }],
    invoice_types: [],
    timezone: 'Asia/Shanghai',
    limits: { max_orders: 50, max_file_bytes: 1024 },
  })
  vi.spyOn(invoiceApi, 'getInvoiceSummary').mockResolvedValue({
    eligible_count: 0,
    totals: [],
    processing_count: 0,
    awaiting_confirmation_count: 0,
    unread_count: 0,
  })
  vi.spyOn(invoiceApi, 'getInvoiceNotifications').mockResolvedValue({
    items: [],
    total: 0,
    p: 1,
    page_size: 20,
    unread_count: 0,
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  location.pathname = '/invoices/new'
  location.search = {}
  const view = render(
    <QueryClientProvider client={client}>
      <InvoiceWorkspace />
    </QueryClientProvider>
  )
  expect(
    screen.getByRole('link', { name: 'Apply for invoice' })
  ).toHaveAttribute('href', '/invoices/new')
  expect(
    screen.queryByRole('button', { name: 'Apply for invoice' })
  ).not.toBeInTheDocument()
  location.pathname = '/invoices'
  location.search = { tab: 'records' }
  view.rerender(
    <QueryClientProvider client={client}>
      <InvoiceWorkspace />
    </QueryClientProvider>
  )
  expect(
    screen.getByRole('button', { name: 'Apply for invoice' })
  ).toHaveAttribute('href', '/invoices/new')
  view.unmount()
  client.clear()
})
