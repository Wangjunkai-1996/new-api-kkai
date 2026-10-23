import {
  QueryClient,
  QueryClientProvider,
  useQuery,
} from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { toast } from 'sonner'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import * as invoiceApi from './api'
import { InvoiceAdminActions } from './components/admin-actions'
import { InvoiceProfiles } from './components/profiles'
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
  amount_minor: '22000',
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
  amount_minor: '22000',
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
    amount_minor: '22000',
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
function DraftHarness(props: {
  step: number
  required?: string[]
  orders?: InvoiceOrder[]
  minimum?: string
}) {
  const [draft, setDraft] = useState<InvoiceDraft>(() => ({
    ...freshInvoiceDraft(),
    step: props.step,
    orders: props.orders || [order],
    profile: { ...profile },
    invoiceType: 'ordinary',
    consent: props.step === 3,
  }))
  const rules = {
    ...policy,
    minimum_amount_minor: props.minimum,
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

test.each([
  { amount: '33000', currency: 'CNY', expected: 'CN¥330.00' },
  {
    amount: '33000',
    currency: 'USD',
    actualCurrency: 'CNY',
    expected: '$330.00',
  },
  { amount: '33000', currency: '', actualCurrency: 'CNY', expected: '330.00' },
  { amount: null, currency: '', expected: 'Order amount unavailable' },
])(
  'unavailable order amount $amount / $currency stays visible without becoming invoiceable',
  async (scenario) => {
    const user = userEvent.setup()
    vi.mocked(invoiceApi.getInvoiceOrders).mockImplementation(
      async (filters) => ({
        items:
          filters.eligibility === 'unavailable'
            ? [
                {
                  ...order,
                  order_no: 'HISTORICAL-1',
                  eligible: false,
                  amount_minor: null,
                  currency: scenario.actualCurrency || '',
                  order_amount_minor: scenario.amount,
                  order_currency: scenario.currency,
                  reason_codes: ['PAYMENT_UNVERIFIED'],
                },
              ]
            : [],
        total: filters.eligibility === 'unavailable' ? 1 : 0,
        p: 1,
        page_size: 20,
      })
    )
    renderWithClient(<DraftHarness step={1} orders={[]} />)
    expect(await screen.findByText('No invoiceable orders yet.')).toBeVisible()
    expect(
      screen.getByText('Select at least one invoiceable order to continue.')
    ).toBeVisible()
    expect(
      screen.getAllByRole('button', { name: 'Next: invoice information' })[0]
    ).toBeDisabled()
    await user.click(
      screen.getByRole('button', {
        name: 'View unavailable orders and reasons',
      })
    )
    expect(await screen.findByText('HISTORICAL-1')).toBeVisible()
    expect(screen.getByText(scenario.expected)).toBeVisible()
    if (scenario.amount != null) {
      expect(screen.getByText('Order amount')).toBeVisible()
    }
    if (!scenario.currency) {
      expect(screen.getByText('Unknown currency')).toBeVisible()
    } else {
      expect(
        screen.getByText(scenario.currency, { selector: 'span' })
      ).toBeVisible()
    }
    if (
      scenario.actualCurrency &&
      scenario.actualCurrency !== scenario.currency
    ) {
      expect(
        screen.queryByText(scenario.actualCurrency, { selector: 'span' })
      ).not.toBeInTheDocument()
    }
    expect(
      screen.getByText('Payment evidence needs verification')
    ).toBeVisible()
    expect(
      screen.getByText(
        'These orders cannot be selected. Review the reason shown for each order.'
      )
    ).toBeVisible()
    expect(screen.queryByRole('checkbox')).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Verify payment' })
    ).not.toBeInTheDocument()
    const next = screen.getAllByRole('button', {
      name: 'Next: invoice information',
    })[0]
    expect(next).toBeDisabled()
    await user.click(next)
    expect(
      screen.getByRole('button', { name: 'Why are some orders unavailable?' })
    ).toHaveAttribute('aria-expanded', 'true')
    expect(
      screen.queryByRole('button', {
        name: 'View unavailable orders and reasons',
      })
    ).not.toBeInTheDocument()
    expect(invoiceApi.quoteInvoice).not.toHaveBeenCalled()
  }
)

test('payment verification refreshes unavailable orders and summary once and preserves the invoice minimum', async () => {
  const user = userEvent.setup()
  const payment = {
    ...order,
    source_type: 'topup',
    amount_minor: '12000',
  }
  let verified = false
  let finishVerification!: (value: { verified: boolean }) => void
  const verify = vi
    .spyOn(invoiceApi, 'verifyInvoicePayment')
    .mockImplementation(
      () =>
        new Promise((resolve) => {
          finishVerification = resolve
        })
    )
  const success = vi
    .spyOn(toast, 'success')
    .mockReturnValue('verification-toast')
  vi.mocked(invoiceApi.getInvoiceOrders).mockImplementation(async (filters) => {
    let items: InvoiceOrder[] = verified ? [payment] : []
    if (filters.eligibility === 'unavailable') {
      items = verified
        ? []
        : [
            {
              ...payment,
              amount_minor: null,
              eligible: false,
              allowed_actions: ['verify_payment'],
              reason_codes: ['PAYMENT_UNVERIFIED'],
            },
          ]
    }
    return { items, total: items.length, p: 1, page_size: 20 }
  })
  const summary = vi
    .spyOn(invoiceApi, 'getInvoiceSummary')
    .mockImplementation(async () => ({
      eligible_count: Number(verified),
      totals: [],
      processing_count: 0,
      awaiting_confirmation_count: 0,
      unread_count: 0,
    }))
  function SummaryObserver() {
    const query = useQuery({
      queryKey: [...invoiceApi.invoiceKey(42), 'summary'],
      queryFn: invoiceApi.getInvoiceSummary,
    })
    return (
      <output aria-label='Verified payment count'>
        {query.data?.eligible_count}
      </output>
    )
  }
  renderWithClient(
    <>
      <DraftHarness step={1} orders={[]} />
      <SummaryObserver />
    </>
  )
  await user.click(
    await screen.findByRole('button', {
      name: 'View unavailable orders and reasons',
    })
  )
  const button = await screen.findByRole('button', { name: 'Verify payment' })
  await user.dblClick(button)
  expect(verify).toHaveBeenCalledExactlyOnceWith(
    { source_type: 'topup', order_id: order.order_id },
    expect.any(String)
  )
  expect(button).toBeDisabled()
  verified = true
  finishVerification({ verified: true })
  const selectable = await screen.findByRole('checkbox', {
    name: `Select order ${order.order_no}`,
  })
  expect(
    screen.queryByRole('button', { name: 'Verify payment' })
  ).not.toBeInTheDocument()
  await waitFor(() =>
    expect(screen.getByLabelText('Verified payment count')).toHaveTextContent(
      '1'
    )
  )
  expect(summary).toHaveBeenCalledTimes(2)
  expect(success).toHaveBeenCalledWith('Payment verified')
  await user.click(selectable)
  expect(
    screen.getAllByRole('button', { name: 'Next: invoice information' })[0]
  ).toBeDisabled()
  expect(invoiceApi.quoteInvoice).not.toHaveBeenCalled()
})

test.each([
  {
    code: 'PAYMENT_VERIFICATION_UNAVAILABLE',
    message:
      'Payment verification is temporarily unavailable. Try again later.',
  },
  {
    code: 'PAYMENT_VERIFICATION_FAILED',
    message:
      'This payment could not be verified. Check its payment status and try again, or contact support.',
  },
  {
    code: 'PAYMENT_EXCEPTION',
    message: 'This payment needs manual review. Contact support.',
  },
  {
    code: 'NOT_FOUND',
    message:
      'The requested record is no longer available. Refresh and try again.',
  },
])(
  'payment verification $code preserves an unavailable order and safely allows retry',
  async (scenario) => {
    const user = userEvent.setup()
    const verify = vi
      .spyOn(invoiceApi, 'verifyInvoicePayment')
      .mockRejectedValue({
        code: 'ERR_BAD_REQUEST',
        response: {
          status: 422,
          data: { code: scenario.code, message: 'sensitive provider response' },
        },
      })
    vi.mocked(invoiceApi.getInvoiceOrders).mockResolvedValue({
      items: [
        {
          ...order,
          source_type: 'topup',
          eligible: false,
          amount_minor: null,
          reason_codes: ['PAYMENT_UNVERIFIED'],
          allowed_actions: ['verify_payment'],
        },
      ],
      total: 1,
      p: 1,
      page_size: 20,
    })
    renderWithClient(<DraftHarness step={1} orders={[]} />)
    const button = await screen.findByRole('button', { name: 'Verify payment' })
    await user.click(button)
    expect(await screen.findByRole('alert')).toHaveTextContent(scenario.message)
    expect(
      screen.queryByText('sensitive provider response')
    ).not.toBeInTheDocument()
    expect(button).toBeEnabled()
    expect(screen.queryByRole('checkbox')).not.toBeInTheDocument()
    await user.click(button)
    await waitFor(() => expect(verify).toHaveBeenCalledTimes(2))
  }
)

test('empty order results distinguish filtered results and reset to the unfiltered explanation', async () => {
  const user = userEvent.setup()
  vi.mocked(invoiceApi.getInvoiceOrders).mockResolvedValue({
    items: [],
    total: 0,
    p: 1,
    page_size: 20,
  })
  renderWithClient(<DraftHarness step={1} orders={[]} />)
  await screen.findByText('No invoiceable orders yet.')
  await user.selectOptions(
    screen.getByRole('combobox', { name: 'Order source' }),
    'topup'
  )
  expect(
    await screen.findByText('No orders match these filters.')
  ).toBeVisible()
  await user.click(screen.getByRole('button', { name: 'Reset filters' }))
  expect(await screen.findByText('No invoiceable orders yet.')).toBeVisible()
  expect(
    screen.queryByText('No orders match these filters.')
  ).not.toBeInTheDocument()
})

test('selecting an invoiceable payment enables the next step and removes the empty selection explanation', async () => {
  const user = userEvent.setup()
  renderWithClient(<DraftHarness step={1} orders={[]} />)
  await user.click(
    await screen.findByRole('checkbox', {
      name: `Select order ${order.order_no}`,
    })
  )
  expect(
    screen.getAllByRole('button', { name: 'Next: invoice information' })[0]
  ).toBeEnabled()
  expect(
    screen.queryByText('Select at least one invoiceable order to continue.')
  ).not.toBeInTheDocument()
})

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

test('a pending submission prevents a saved title read from changing the original draft', async () => {
  const user = userEvent.setup()
  const savedProfile = { ...profile, id: 'profile_1', title: 'Saved Company' }
  vi.mocked(invoiceApi.getInvoiceProfiles).mockResolvedValue({
    items: [savedProfile],
    total: 1,
    p: 1,
    page_size: 100,
  })
  let finishProfile!: (value: InvoiceProfile) => void
  vi.spyOn(invoiceApi, 'getInvoiceProfile').mockImplementation(
    () =>
      new Promise((resolve) => {
        finishProfile = resolve
      })
  )
  function ProfilesHarness() {
    const [draft, setDraft] = useState<InvoiceDraft>(() => ({
      ...freshInvoiceDraft(),
      orders: [order],
      profile,
      step: 3,
    }))
    return (
      <DraftContext.Provider value={{ draft, setDraft, policy }}>
        <button
          type='button'
          onClick={() =>
            setDraft((previous) => ({
              ...previous,
              pendingCreate: {
                key: previous.key,
                input: {
                  items: [
                    {
                      source_type: order.source_type,
                      order_id: order.order_id,
                    },
                  ],
                  profile,
                  invoice_type: 'ordinary',
                  policy_version: 'v1',
                  note: '',
                  user_confirmed: true,
                },
              },
            }))
          }
        >
          Simulate pending submission
        </button>
        <output aria-label='Draft title'>{draft.profile.title}</output>
        <InvoiceProfiles policy={policy} />
      </DraftContext.Provider>
    )
  }
  renderWithClient(<ProfilesHarness />)
  const useTitle = await screen.findByRole('button', { name: 'Use this title' })
  await user.click(useTitle)
  await user.click(
    screen.getByRole('button', { name: 'Simulate pending submission' })
  )
  finishProfile(savedProfile)
  await waitFor(() =>
    expect(navigate).toHaveBeenCalledWith({ to: '/invoices/new' })
  )
  expect(screen.getByLabelText('Draft title')).toHaveTextContent(profile.title)
  expect(useTitle).toBeDisabled()
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
    amount_minor: '22000',
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

test('a committed create with a lost response replays the exact input and key without quoting claimed orders', async () => {
  const user = userEvent.setup()
  const create = vi
    .spyOn(invoiceApi, 'createInvoice')
    .mockRejectedValueOnce(new Error('Server committed; response disconnected'))
    .mockResolvedValueOnce(invoice)
  vi.mocked(invoiceApi.quoteInvoice)
    .mockResolvedValueOnce({
      items: [order],
      eligible: true,
      amount_minor: '22000',
      currency: 'CNY',
      merge_group_id: 'same_seller_cny',
      policy_version: 'v1',
    })
    .mockResolvedValueOnce({
      items: [
        {
          ...order,
          eligible: false,
          reason_codes: ['ORDER_CLAIMED'],
          claimed_request_public_id: invoice.public_id,
        },
      ],
      eligible: false,
      amount_minor: '0',
      currency: '',
      merge_group_id: '',
      policy_version: 'v1',
    })
  renderWithClient(<DraftHarness step={3} />)
  await user.click(
    screen.getAllByRole('button', { name: 'Submit invoice request' })[0]
  )
  await screen.findByRole('alert')
  expect(
    screen.getByRole('button', { name: 'Edit information' })
  ).toBeDisabled()
  await user.click(
    screen.getAllByRole('button', { name: 'Check submission result' })[0]
  )
  await waitFor(() => expect(create).toHaveBeenCalledTimes(2))
  expect(create.mock.calls[0][1]).toBe(create.mock.calls[1][1])
  expect(create.mock.calls[1][0]).toEqual(create.mock.calls[0][0])
  expect(invoiceApi.quoteInvoice).toHaveBeenCalledTimes(1)
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
        amount_minor: '22000',
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
    items: [{ ...order, amount_minor: '21900' }],
    eligible: true,
    amount_minor: '21900',
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

test('pending quotes freeze order selection, removal, step changes and invoice input', async () => {
  const user = userEvent.setup()
  let finishQuote!: (
    quote: Awaited<ReturnType<typeof invoiceApi.quoteInvoice>>
  ) => void
  let finishReview!: (
    quote: Awaited<ReturnType<typeof invoiceApi.quoteInvoice>>
  ) => void
  vi.mocked(invoiceApi.quoteInvoice)
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finishQuote = resolve
        })
    )
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finishReview = resolve
        })
    )
  renderWithClient(<DraftHarness step={1} />)
  const checkbox = await screen.findByRole('checkbox', {
    name: `Select order ${order.order_no}`,
  })
  await user.click(
    screen.getAllByRole('button', { name: 'Next: invoice information' })[0]
  )
  expect(checkbox).toHaveAttribute('aria-disabled', 'true')
  await user.click(checkbox)
  expect(checkbox).toBeChecked()
  await user.click(screen.getByText('View selected orders'))
  expect(screen.getByRole('button', { name: 'Remove' })).toBeDisabled()
  expect(screen.getByRole('button', { name: /1Select orders/ })).toBeDisabled()
  finishQuote({
    items: [order],
    eligible: true,
    amount_minor: '22000',
    currency: 'CNY',
    merge_group_id: 'same_seller_cny',
    policy_version: 'v1',
  })
  expect(
    await screen.findByRole('textbox', { name: /Invoice title/ })
  ).toBeEnabled()
  expect(screen.getAllByText('1 selected')).toHaveLength(2)
  expect(invoiceApi.getInvoiceOrders).toHaveBeenCalledTimes(1)
  expect(invoiceApi.getInvoiceProfiles).toHaveBeenCalledTimes(1)
  await user.click(
    screen.getAllByRole('button', { name: 'Next: review request' })[0]
  )
  expect(screen.getByRole('textbox', { name: /Invoice title/ })).toBeDisabled()
  expect(
    screen.getByRole('textbox', { name: 'Additional note' })
  ).toBeDisabled()
  expect(screen.getByRole('combobox', { name: 'Invoice type' })).toBeDisabled()
  expect(screen.getByRole('button', { name: 'Remove' })).toBeDisabled()
  expect(
    screen.getAllByRole('button', { name: 'Previous step' })[0]
  ).toBeDisabled()
  finishReview({
    items: [order],
    eligible: true,
    amount_minor: '22000',
    currency: 'CNY',
    merge_group_id: 'same_seller_cny',
    policy_version: 'v1',
  })
  expect(
    await screen.findByRole('button', { name: 'Edit information' })
  ).toBeEnabled()
})

test.each([
  { amount: '19999', enabled: false },
  { amount: '20000', enabled: true },
  { amount: '20001', enabled: true },
  { amount: '24999', minimum: '25000', enabled: false },
  { amount: '25000', minimum: '25000', enabled: true },
  {
    amount: '900719925474099299',
    minimum: '900719925474099300',
    enabled: false,
  },
])(
  'minimum invoice amount gates the next step exactly: $amount / $minimum',
  (scenario) => {
    renderWithClient(
      <DraftHarness
        step={1}
        minimum={scenario.minimum}
        orders={[
          {
            ...order,
            amount_minor: scenario.amount,
          },
        ]}
      />
    )
    const button = screen.getAllByRole('button', {
      name: 'Next: invoice information',
    })[0]
    if (scenario.enabled) expect(button).toBeEnabled()
    else expect(button).toBeDisabled()
  }
)

test('small eligible payments can be combined to exactly reach the minimum while claimed orders stay unavailable', async () => {
  const user = userEvent.setup()
  const second = {
    ...order,
    order_id: 'payment_2',
    order_no: 'PAYMENT-2',
    amount_minor: '8000',
  }
  vi.mocked(invoiceApi.getInvoiceOrders).mockResolvedValue({
    items: [
      { ...order, amount_minor: '12000' },
      second,
      {
        ...order,
        order_id: 'claimed',
        order_no: 'CLAIMED',
        eligible: false,
        reason_codes: ['ORDER_CLAIMED'],
      },
    ],
    total: 3,
    p: 1,
    page_size: 20,
  })
  renderWithClient(<DraftHarness step={1} orders={[]} />)
  await user.click(
    await screen.findByRole('checkbox', {
      name: `Select order ${order.order_no}`,
    })
  )
  expect(
    screen.getAllByRole('button', { name: 'Next: invoice information' })[0]
  ).toBeDisabled()
  expect(
    screen.getByText('Select CN¥80.00 more in eligible payments to continue.')
  ).toBeVisible()
  expect(
    screen.queryByRole('checkbox', { name: 'Select order CLAIMED' })
  ).not.toBeInTheDocument()
  await user.click(
    screen.getByRole('checkbox', { name: 'Select order PAYMENT-2' })
  )
  expect(
    screen.getAllByRole('button', { name: 'Next: invoice information' })[0]
  ).toBeEnabled()
  expect(
    screen.queryByText(/more in eligible payments to continue/)
  ).not.toBeInTheDocument()
})

test('requests below the minimum cannot submit or quote even from the final step', async () => {
  const user = userEvent.setup()
  const create = vi.spyOn(invoiceApi, 'createInvoice')
  renderWithClient(
    <DraftHarness step={3} orders={[{ ...order, amount_minor: '19999' }]} />
  )
  const submit = screen.getAllByRole('button', {
    name: 'Submit invoice request',
  })[0]
  expect(submit).toBeDisabled()
  await user.click(submit)
  expect(create).not.toHaveBeenCalled()
  expect(invoiceApi.quoteInvoice).not.toHaveBeenCalled()
})

test('a definitive validation refusal allows corrections and creates a fresh submission key', async () => {
  const user = userEvent.setup()
  const create = vi
    .spyOn(invoiceApi, 'createInvoice')
    .mockRejectedValueOnce({
      response: {
        status: 400,
        data: {
          code: 'VALIDATION_FAILED',
          field_errors: { 'profile.email': 'INVALID_EMAIL' },
        },
      },
    })
    .mockResolvedValueOnce(invoice)
  renderWithClient(<DraftHarness step={3} />)
  await user.click(
    screen.getAllByRole('button', { name: 'Submit invoice request' })[0]
  )
  const email = await screen.findByRole('textbox', { name: /Invoice email/ })
  await user.clear(email)
  await user.type(email, 'corrected@example.com')
  await user.click(
    screen.getAllByRole('button', { name: 'Next: review request' })[0]
  )
  await user.click(screen.getByRole('checkbox'))
  await user.click(
    screen.getAllByRole('button', { name: 'Submit invoice request' })[0]
  )
  await waitFor(() => expect(create).toHaveBeenCalledTimes(2))
  expect(create.mock.calls[1][1]).not.toBe(create.mock.calls[0][1])
  expect(create.mock.calls[1][0].profile?.email).toBe('corrected@example.com')
})

test('a higher server minimum refreshes invoice policy and shows the business error behind an Axios code', async () => {
  const user = userEvent.setup()
  vi.spyOn(invoiceApi, 'createInvoice').mockRejectedValueOnce({
    code: 'ERR_BAD_REQUEST',
    response: {
      status: 422,
      data: { code: 'INVOICE_BELOW_MINIMUM' },
    },
  })
  renderWithClient(<DraftHarness step={3} />)
  const invalidate = vi.spyOn(clients[0], 'invalidateQueries')
  await user.click(
    screen.getAllByRole('button', { name: 'Submit invoice request' })[0]
  )
  expect(await screen.findByRole('alert')).toHaveTextContent(
    'Add eligible payments to reach the minimum invoice amount.'
  )
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ['invoices'] })
  expect(
    screen.queryByRole('button', { name: 'Check submission result' })
  ).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Edit information' })).toBeEnabled()
})
