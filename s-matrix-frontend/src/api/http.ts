const TOKEN_KEY = 's_matrix_token'

export function getToken() { return localStorage.getItem(TOKEN_KEY) || '' }
export function setToken(token: string) { localStorage.setItem(TOKEN_KEY, token) }
export function clearToken() { localStorage.removeItem(TOKEN_KEY) }

export async function apiFetch(path: string, init: RequestInit = {}) {
  const headers = new Headers(init.headers || {})
  headers.set('Content-Type', headers.get('Content-Type') || 'application/json')
  const token = getToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)
  const res = await fetch(path, { ...init, headers })
  if (res.status === 401) {
    clearToken()
    if (location.pathname !== '/login') location.href = '/login'
  }
  return res
}

export async function login(username: string, password: string) {
  const res = await fetch('/api/v1/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ username, password }) })
  const text = await res.text()
  if (!res.ok) throw new Error(text)
  const data = JSON.parse(text)
  setToken(data.token)
  return data
}
