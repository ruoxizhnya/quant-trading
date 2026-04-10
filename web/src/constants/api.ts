export const API_TIMEOUT = 60000 // Default request timeout in milliseconds (60s)
export const API_RETRY_DELAY = 1000 // Base retry delay in milliseconds (1s)
export const API_MAX_RETRIES = 3 // Maximum number of automatic retries

export const HTTP_STATUS = {
  BAD_REQUEST: 400,
  UNAUTHORIZED: 401,
  FORBIDDEN: 403,
  NOT_FOUND: 404,
  CONFLICT: 409,
  UNPROCESSABLE_ENTITY: 422,
  TOO_MANY_REQUESTS: 429,
  INTERNAL_SERVER_ERROR: 500,
  BAD_GATEWAY: 502,
  SERVICE_UNAVAILABLE: 503,
  GATEWAY_TIMEOUT: 504,
} as const
