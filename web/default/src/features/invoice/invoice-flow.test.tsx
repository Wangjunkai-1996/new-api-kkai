import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import * as invoiceApi from './api'
import { InvoiceAdminActions } from './components/admin-actions'
import { InvoiceUserActions } from './components/user-actions'
import { DraftContext, type InvoiceDraft, freshInvoiceDraft } from './lib/draft'
import { NewInvoicePage } from './new'
import type {
  InvoiceOrder,
  InvoicePolicy,
  InvoiceProfile,
  InvoiceRequest,
} from './types'

const navigate = vi.hoisted(() => vi.fn())
vi.mock('@tanstack/react-router', async (original) => ({
  ...(await original<typeof import('@tanstack/react-router')>()),
  useNavigate: () => navigate,
}))
vi.mock('./components/upload', () => ({
  InvoiceUpload: ({ onChange }: { onChange: (file: object) => void }) => (
    <button
      type='button'
      onClick={() =>
        onChange({
          id: 'attachment_1',
          file_name: 'invoice.pdf',
          mime_type: 'application/pdf',
          size: 100,
        })
      }
    >
      Choose test attachment
    </button>
  ),
}))

const profile: InvoiceProfile = {
  kind: 'company',
  title: 'Example Company',
  tax_id: '91310000123456789X',
  email: 'invoice@example.com',
}
const order: InvoiceOrder = {
  source_type: 'membership',
  order_id: 'member_1',
  order_no: 'MEM2026102201',
  paid_at: '2026-10-22T00:00:00Z',
  payment_label: 'Card',
  amount_minor: '12000',
  currency: 'CNY',
  currency_exponent: 2,
  eligible: true,
  reason_codes: [],
  merge_group_id: 'same_seller_cny',
  allowed_invoice_types: ['ordinary'],
}
const policy: InvoicePolicy = {
  enabled: true,
  policy_version: 'v1',
  seller_id: 'seller',
  seller_name: 'Example Seller',
  currencies: [{ currency: 'CNY', exponent: 2 }],
  invoice_types: [{ id: 'ordinary', label: 'Ordinary', required_fields: [] }],
  timezone: 'Asia/Shanghai',
  limits: { max_orders: 50, max_file_bytes: 20 * 1024 * 1024 },
}
const invoice: InvoiceRequest = {
  public_id: 'inv_1',
  display_no: 'INV2026102201',
  status: 'delivered',
  version: 8,
  profile,
  seller_id: 'seller',
  seller_name: 'Example Seller',
  invoice_type: 'ordinary',
  amount_minor: '12000',
  currency: 'CNY',
  currency_exponent: 2,
  items_count: 1,
  source_summary: ['membership'],
  created_at: '2026-10-22T00:00:00Z',
  updated_at: '2026-10-22T00:00:00Z',
  current_delivery_version: 2,
  allowed_actions: ['confirm', 'dispute'],
}
const clients: QueryClient[] = []

beforeEach(() => {
  useAuthStore
    .getState()
    .auth.setUser({ id: 42, username: 'customer', role: 1 })
  vi.spyOn(invoiceApi, 'getInvoiceProfiles').mockResolvedValue({
    items: [],
    total: 0,
    p: 1,
    page_size: 100,
  })
  vi.spyOn(invoiceApi, 'getInvoiceOrders').mockResolvedValue({
    items: [order],
    total: 1,
    p: 1,
    page_size: 20,
  })
  vi.spyOn(invoiceApi, 'quoteInvoice').mockResolvedValue({
    items: [order],
    eligible: true,
    amount_minor: '12000',
    currency: 'CNY',
    merge_group_id: 'same_seller_cny',
    policy_version: 'v1',
  })
  Object.defineProperty(HTMLElement.prototype, 'getAnimations', {
    configurable: true,
    value: () => [],
  })
})
afterEach(() => {
  clients.splice(0).forEach((client) => client.clear())
  useAuthStore.getState().auth.reset()
  vi.restoreAllMocks()
  vi.clearAllMocks()
})
function renderWithClient(element: React.ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  return render(
    <QueryClientProvider client={client}>{element}</QueryClientProvider>
  )
}
function DraftHarness(props: { step: number; required?: string[] }) {
  const [draft, setDraft] = useState<InvoiceDraft>(() => ({
    ...freshInvoiceDraft(),
    step: props.step,
    orders: [order],
    profile: { ...profile },
    invoiceType: 'ordinary',
    consent: props.step === 3,
  }))
  const rules = {
    ...policy,
    invoice_types: [
      { ...policy.invoice_types[0], required_fields: props.required || [] },
    ],
  }
  return (
    <DraftContext.Provider value={{ draft, setDraft, policy: rules }}>
      <NewInvoicePage />
    </DraftContext.Provider>
  )
}

test('saved title updates carry the server version after creation and every update', async () => {
  const user = userEvent.setup()
  const save = vi
    .spyOn(invoiceApi, 'saveInvoiceProfile')
    .mockResolvedValueOnce({ ...profile, id: 'profile_1', version: 1 })
    .mockResolvedValueOnce({ ...profile, id: 'profile_1', version: 2 })
    .mockResolvedValueOnce({ ...profile, id: 'profile_1', version: 3 })
  renderWithClient(<DraftHarness step={2} />)
  await user.click(screen.getByRole('button', { name: 'Save as new title' }))
  await user.click(
    await screen.findByRole('button', { name: 'Update saved title' })
  )
  await waitFor(() => expect(save).toHaveBeenCalledTimes(2))
  expect(save.mock.calls[1][0]).toMatchObject({ id: 'profile_1', version: 1 })
  await waitFor(() =>
    expect(
      screen.getByRole('button', { name: 'Update saved title' })
    ).toBeEnabled()
  )
  await user.click(screen.getByRole('button', { name: 'Update saved title' }))
  await waitFor(() => expect(save).toHaveBeenCalledTimes(3))
  expect(save.mock.calls[2][0]).toMatchObject({ id: 'profile_1', version: 2 })
})

test('policy required fields block review and identify the missing field', async () => {
  const user = userEvent.setup()
  renderWithClient(<DraftHarness step={2} required={['address']} />)
  await user.click(
    screen.getAllByRole('button', { name: 'Next: review request' })[0]
  )
  expect(screen.getByRole('textbox', { name: /Address/ })).toHaveAttribute(
    'aria-invalid',
    'true'
  )
  expect(invoiceApi.quoteInvoice).not.toHaveBeenCalled()
  expect(
    screen.queryByRole('button', { name: 'Submit invoice request' })
  ).not.toBeInTheDocument()
})

test('final submission rechecks changed payments before creating a request', async () => {
  const user = userEvent.setup()
  const create = vi.spyOn(invoiceApi, 'createInvoice')
  vi.mocked(invoiceApi.quoteInvoice).mockResolvedValueOnce({
    items: [{ ...order, eligible: false, reason_codes: ['ORDER_CLAIMED'] }],
    eligible: false,
    amount_minor: '12000',
    currency: 'CNY',
    merge_group_id: 'same_seller_cny',
    policy_version: 'v1',
  })
  renderWithClient(<DraftHarness step={3} />)
  await user.click(
    screen.getAllByRole('button', { name: 'Submit invoice request' })[0]
  )
  expect(await screen.findByRole('alert')).toHaveTextContent(
    'Payment eligibility has changed'
  )
  expect(create).not.toHaveBeenCalled()
  expect(
    screen.getAllByRole('button', { name: 'Next: invoice information' })[0]
  ).toBeDisabled()
})

test('unknown create outcome retries with the same idempotency key and preserved input', async () => {
  const user = userEvent.setup()
  const create = vi
    .spyOn(invoiceApi, 'createInvoice')
    .mockRejectedValueOnce(new Error('Network disconnected'))
    .mockResolvedValueOnce(invoice)
  renderWithClient(<DraftHarness step={3} />)
  await user.click(
    screen.getAllByRole('button', { name: 'Submit invoice request' })[0]
  )
  await screen.findByRole('alert')
  await user.click(
    screen.getAllByRole('button', { name: 'Submit invoice request' })[0]
  )
  await waitFor(() => expect(create).toHaveBeenCalledTimes(2))
  expect(create.mock.calls[0][1]).toBe(create.mock.calls[1][1])
  expect(create.mock.calls[1][0]).toMatchObject({
    items: [{ source_type: 'membership', order_id: 'member_1' }],
    profile,
  })
  expect(invoiceApi.quoteInvoice).toHaveBeenCalledTimes(2)
  await waitFor(() =>
    expect(navigate).toHaveBeenCalledWith({
      to: '/invoices/$invoiceId',
      params: { invoiceId: 'inv_1' },
    })
  )
})

test('confirmation submits the displayed request and current delivery versions', async () => {
  const user = userEvent.setup()
  const action = vi
    .spyOn(invoiceApi, 'invoiceAction')
    .mockResolvedValue({ ...invoice, status: 'confirmed' })
  renderWithClient(<InvoiceUserActions invoice={invoice} />)
  await user.click(screen.getByRole('button', { name: 'Confirm correct' }))
  await user.click(
    within(screen.getByRole('dialog')).getByRole('button', {
      name: 'Confirm correct',
    })
  )
  await waitFor(() =>
    expect(action).toHaveBeenCalledWith(
      'inv_1',
      'confirm',
      { version: 8, delivery_version: 2 },
      expect.any(String)
    )
  )
})

test('feedback keeps the current delivery and submits category, description and evidence', async () => {
  const user = userEvent.setup()
  const action = vi
    .spyOn(invoiceApi, 'invoiceAction')
    .mockResolvedValue({ ...invoice, status: 'disputed' })
  renderWithClient(<InvoiceUserActions invoice={invoice} />)
  await user.click(
    screen.getByRole('button', { name: 'Invoice has a problem' })
  )
  const dialog = within(screen.getByRole('dialog'))
  await user.selectOptions(
    dialog.getByRole('combobox', { name: 'Problem type' }),
    'amount'
  )
  await user.type(
    dialog.getByRole('textbox', { name: 'Description' }),
    'The total is incorrect.'
  )
  await user.click(
    dialog.getByRole('button', { name: 'Choose test attachment' })
  )
  await user.click(dialog.getByRole('button', { name: 'Submit feedback' }))
  await waitFor(() =>
    expect(action).toHaveBeenCalledWith(
      'inv_1',
      'disputes',
      {
        version: 8,
        delivery_version: 2,
        category: 'amount',
        description: 'The total is incorrect.',
        evidence_attachment_id: 'attachment_1',
      },
      expect.any(String)
    )
  )
})

test('upload selection does not deliver until the admin explicitly confirms complete metadata', async () => {
  const user = userEvent.setup()
  const action = vi
    .spyOn(invoiceApi, 'invoiceAction')
    .mockResolvedValue(invoice)
  renderWithClient(
    <InvoiceAdminActions
      invoice={{
        ...invoice,
        status: 'approved_for_issue',
        allowed_actions: ['deliver'],
      }}
    />
  )
  await user.click(
    screen.getByRole('button', { name: 'Upload and deliver invoice' })
  )
  const dialog = within(screen.getByRole('dialog'))
  const deliver = dialog.getByRole('button', { name: 'Deliver invoice' })
  expect(deliver).toBeDisabled()
  await user.click(
    dialog.getByRole('button', { name: 'Choose test attachment' })
  )
  expect(action).not.toHaveBeenCalled()
  expect(deliver).toBeDisabled()
  await user.type(
    dialog.getByRole('textbox', { name: 'Invoice number' }),
    'FP-2026-1'
  )
  await user.click(deliver)
  await waitFor(() =>
    expect(action).toHaveBeenCalledWith(
      'inv_1',
      'deliveries',
      expect.objectContaining({
        version: 8,
        attachment_id: 'attachment_1',
        invoice_no: 'FP-2026-1',
        amount_minor: '12000',
        currency: 'CNY',
      }),
      expect.any(String),
      true
    )
  )
})

test('a changed quoted amount requires the user to review and confirm again', async () => {
  const user = userEvent.setup()
  const create = vi.spyOn(invoiceApi, 'createInvoice')
  vi.mocked(invoiceApi.quoteInvoice).mockResolvedValueOnce({
    items: [{ ...order, amount_minor: '11900' }],
    eligible: true,
    amount_minor: '11900',
    currency: 'CNY',
    merge_group_id: 'same_seller_cny',
    policy_version: 'v1',
  })
  renderWithClient(<DraftHarness step={3} />)
  await user.click(
    screen.getAllByRole('button', { name: 'Submit invoice request' })[0]
  )
  expect(await screen.findByRole('alert')).toHaveTextContent(
    'Payment eligibility has changed'
  )
  expect(create).not.toHaveBeenCalled()
  expect(
    screen.getAllByRole('button', { name: 'Submit invoice request' })[0]
  ).toBeDisabled()
})

test('a delivery changed while confirming cannot be silently confirmed with a newer version', async () => {
  const user = userEvent.setup()
  const action = vi.spyOn(invoiceApi, 'invoiceAction')
  function VersionHarness() {
    const [value, setValue] = useState(invoice)
    return (
      <>
        <button
          type='button'
          onClick={() =>
            setValue({ ...invoice, version: 9, current_delivery_version: 3 })
          }
        >
          Simulate new delivery
        </button>
        <InvoiceUserActions invoice={value} />
      </>
    )
  }
  renderWithClient(<VersionHarness />)
  await user.click(screen.getByRole('button', { name: 'Confirm correct' }))
  // Re-render through an external update while the dialog remains open.
  screen
    .getByRole('button', { name: 'Simulate new delivery', hidden: true })
    .click()
  await waitFor(() =>
    expect(
      within(screen.getByRole('dialog')).getByRole('button', {
        name: 'Confirm correct',
      })
    ).toBeDisabled()
  )
  expect(screen.getByRole('alert')).toHaveTextContent(
    'Review the latest version'
  )
  expect(action).not.toHaveBeenCalled()
})

test('admin rejection requires a visible reason and explicit confirmation that no invoice was issued', async () => {
  const user = userEvent.setup()
  const action = vi
    .spyOn(invoiceApi, 'invoiceAction')
    .mockResolvedValue({ ...invoice, status: 'rejected' })
  renderWithClient(
    <InvoiceAdminActions
      invoice={{
        ...invoice,
        status: 'pending_review',
        allowed_actions: ['reject'],
      }}
    />
  )
  await user.click(screen.getByRole('button', { name: 'Reject request' }))
  const dialog = within(screen.getByRole('dialog'))
  const reject = dialog.getByRole('button', { name: 'Reject request' })
  expect(reject).toBeDisabled()
  await user.type(
    dialog.getByRole('textbox', { name: 'Reason' }),
    'Please correct the title.'
  )
  expect(reject).toBeDisabled()
  await user.click(dialog.getByRole('checkbox'))
  await user.click(reject)
  await waitFor(() =>
    expect(action).toHaveBeenCalledWith(
      'inv_1',
      'reject',
      expect.objectContaining({
        version: 8,
        reason: 'Please correct the title.',
        not_issued_confirmed: true,
      }),
      expect.any(String),
      true
    )
  )
})

test('server profile validation returns to the preserved form with a field error', async () => {
  const user = userEvent.setup()
  vi.spyOn(invoiceApi, 'createInvoice').mockRejectedValueOnce({
    response: {
      status: 400,
      data: {
        code: 'VALIDATION_FAILED',
        field_errors: { 'profile.email': 'INVALID_EMAIL' },
      },
    },
  })
  renderWithClient(<DraftHarness step={3} />)
  await user.click(
    screen.getAllByRole('button', { name: 'Submit invoice request' })[0]
  )
  const email = await screen.findByRole('textbox', { name: /Invoice email/ })
  expect(email).toHaveAttribute('aria-invalid', 'true')
  expect(email).toHaveValue('invoice@example.com')
  expect(screen.getByRole('textbox', { name: /Invoice title/ })).toHaveValue(
    'Example Company'
  )
})
