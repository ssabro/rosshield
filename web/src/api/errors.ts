// rosshield API 에러 정규화.
// - openapi-fetch가 반환하는 `{ data, error, response }`를 ApiError로 변환.
// - status 기반 helper로 UI 분기(redirect to login 등)를 단순화.

export class ApiError extends Error {
  // tsconfig `erasableSyntaxOnly` 때문에 parameter properties(`public status`)
  // 사용 불가 — 명시적 필드 선언 + 생성자에서 대입.
  public readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }

  isClientError(): boolean {
    return this.status >= 400 && this.status < 500
  }

  isServerError(): boolean {
    return this.status >= 500
  }

  isUnauthorized(): boolean {
    return this.status === 401
  }
}

/**
 * 서버가 반환한 임의 에러 객체에서 message를 추출.
 * - 서버 합의 형식: `{ error: "message" }`
 * - fallback: HTTP status text
 */
export function extractErrorMessage(
  error: unknown,
  fallback: string,
): string {
  if (error && typeof error === 'object' && 'error' in error) {
    const msg = (error as { error: unknown }).error
    if (typeof msg === 'string' && msg.length > 0) {
      return msg
    }
  }
  return fallback
}
