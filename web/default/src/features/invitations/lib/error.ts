const GENERIC_AXIOS_STATUS_MESSAGE = /^Request failed with status code \d+$/

export function getInvitationErrorMessage(
  error: unknown,
  fallback: string
): string {
  const responseMessage = (
    error as { response?: { data?: { message?: unknown } } }
  )?.response?.data?.message
  if (typeof responseMessage === 'string' && responseMessage.trim()) {
    return responseMessage
  }

  const errorMessage = (error as { message?: unknown })?.message
  if (
    typeof errorMessage === 'string' &&
    errorMessage.trim() &&
    !GENERIC_AXIOS_STATUS_MESSAGE.test(errorMessage)
  ) {
    return errorMessage
  }

  return fallback
}
