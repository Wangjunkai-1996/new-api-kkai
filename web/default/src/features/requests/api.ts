import { api } from '@/lib/api'
import { getCurrentHost } from '@/lib/host-scope'
import { requireServerSuccess } from '@/lib/server-error-message'

export type RequestStatus =
  | 'pending'
  | 'accepted'
  | 'scheduling'
  | 'developing'
  | 'completed'
  | 'rejected'

export interface RequestTicket {
  id: number
  tenant_id: number
  user_id: number
  title: string
  description: string
  status: RequestStatus
  version: number
  created_at: number
  updated_at: number
  latest_feedback: string
  unread_count: number
  username?: string
}

export interface RequestEvent {
  id: number
  ticket_id: number
  actor_id: number
  status: RequestStatus
  feedback: string
  created_at: number
  read_at: number
}

export interface RequestDetail {
  ticket: RequestTicket
  events: RequestEvent[]
}

export interface RequestPage {
  items: RequestTicket[]
  total: number
  page: number
  page_size: number
}

export interface RequestNotification {
  id: number
  ticket_id: number
  event_id: number
  title: string
  status: RequestStatus
  feedback: string
  created_at: number
  read_at: number
}

export interface RequestNotificationPage {
  items: RequestNotification[]
  total: number
  unread_count: number
}

export interface RequestListOptions {
  page?: number
  pageSize?: number
  status?: RequestStatus | 'active' | ''
}

export function requestQueryKey(userId: number | undefined, resource: string) {
  return ['requests', getCurrentHost(), userId, resource] as const
}

export async function getRequests(
  options: RequestListOptions = {}
): Promise<RequestPage> {
  const response = await api.get('/api/requests', {
    params: {
      p: options.page ?? 1,
      page_size: options.pageSize ?? 20,
      status: options.status || undefined,
    },
  })
  return requireServerSuccess(response.data).data
}

export async function getRequest(id: number): Promise<RequestDetail> {
  const response = await api.get(`/api/requests/${id}`)
  return requireServerSuccess(response.data).data
}

export async function submitRequest(input: {
  title: string
  description: string
}): Promise<RequestDetail> {
  const response = await api.post('/api/requests', input)
  return requireServerSuccess(response.data).data
}

export async function getAdminRequests(
  options: RequestListOptions = {}
): Promise<RequestPage> {
  const response = await api.get('/api/requests/admin', {
    params: {
      p: options.page ?? 1,
      page_size: options.pageSize ?? 20,
      status: options.status || undefined,
    },
  })
  return requireServerSuccess(response.data).data
}

export async function getAdminRequest(id: number): Promise<RequestDetail> {
  const response = await api.get(`/api/requests/admin/${id}`)
  return requireServerSuccess(response.data).data
}

export async function transitionRequest(
  id: number,
  input: { status: RequestStatus; feedback: string; version: number }
): Promise<RequestDetail> {
  const response = await api.patch(`/api/requests/admin/${id}/status`, input)
  return requireServerSuccess(response.data).data
}

export async function getRequestNotifications(): Promise<RequestNotificationPage> {
  const response = await api.get('/api/requests/notifications', {
    params: { p: 1, page_size: 20 },
  })
  return requireServerSuccess(response.data).data
}

export async function markRequestNotificationRead(id: number): Promise<void> {
  const response = await api.post(`/api/requests/notifications/${id}/read`)
  requireServerSuccess(response.data)
}

export async function markAllRequestNotificationsRead(): Promise<void> {
  const response = await api.post('/api/requests/notifications/read-all')
  requireServerSuccess(response.data)
}
