const ENV_BASE_URL = import.meta.env.VITE_API_BASE_URL ?? ''

function getBaseUrl(): string {
  return localStorage.getItem('api_base_url') || ENV_BASE_URL
}

function getApiKey(): string {
  return localStorage.getItem('api_key') || ''
}

export async function apiFetch(path: string, init?: RequestInit): Promise<Response> {
  const headers = new Headers(init?.headers || {})
  const key = getApiKey()
  if (key && !headers.has('Authorization') && !headers.has('x-api-key')) {
    headers.set('Authorization', `Bearer ${key}`)
  }
  return fetch(`${getBaseUrl()}${path}`, { ...init, headers })
}
