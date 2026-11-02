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
const CC_SWITCH_TOKEN_USAGE_SCRIPT = `({
  request: {
    url: "{{baseUrl}}/api/usage/token/",
    method: "GET",
    headers: {
      "Authorization": "Bearer {{apiKey}}",
      "User-Agent": "cc-switch/1.0"
    }
  },
  extractor: function(response) {
    if (!response || response.code === false || response.success === false) {
      return {
        isValid: false,
        invalidMessage: response?.message || response?.error?.message || "Query failed"
      };
    }

    const data = response.data || response;
    if (data.token_is_valid === false) {
      return {
        isValid: false,
        invalidMessage: data.token_invalid_reason || "Token unavailable"
      };
    }

    const quotaPerUnit = Number(data.quota_per_unit || 500000);
    const displayType = data.quota_display_type || "USD";
    const usdExchangeRate = Number(data.usd_exchange_rate || 1);
    const customExchangeRate = Number(data.custom_currency_exchange_rate || 1);
    const displayUnit = displayType === "CUSTOM"
      ? (data.custom_currency_symbol || "CUSTOM")
      : displayType;
    const convertQuota = function(quota) {
      const value = Number(quota || 0);
      if (displayType === "TOKENS") return value;
      if (displayType === "CNY") return value / quotaPerUnit * usdExchangeRate;
      if (displayType === "CUSTOM") return value / quotaPerUnit * customExchangeRate;
      return value / quotaPerUnit;
    };

    const userUsed = Number(data.user_total_used ?? data.total_used ?? 0);
    const userAvailable = Number(data.user_total_available ?? data.total_available ?? 0);
    const userGranted = Number(
      data.user_total_granted ?? data.total_granted ?? userUsed + userAvailable
    );
    const hasTokenAvailable =
      data.token_total_available != null || data.total_available != null;
    const tokenAvailable = Number(
      data.token_total_available ?? data.total_available ?? 0
    );
    const isUnlimitedToken =
      data.unlimited_quota === true || (hasTokenAvailable && tokenAvailable < 0);

    if (userAvailable <= 0) {
      return {
        isValid: false,
        invalidMessage: "User balance exhausted"
      };
    }

    if (hasTokenAvailable && !isUnlimitedToken && tokenAvailable <= 0) {
      return {
        isValid: false,
        invalidMessage: data.token_invalid_reason || "Token unavailable"
      };
    }

    return {
      planName: "User Balance",
      remaining: convertQuota(userAvailable),
      total: convertQuota(userGranted),
      used: convertQuota(userUsed),
      unit: displayUnit,
      isValid: true
    };
  }
})`

export function getCCSwitchServerAddress(): string {
  if (typeof window === 'undefined') return ''
  const url = new URL(window.location.href)
  // The browser's current host is the tenant API host; cached status can point elsewhere.
  if (
    url.protocol === 'http:' &&
    !['localhost', '127.0.0.1', '[::1]'].includes(url.hostname)
  ) {
    url.protocol = 'https:'
  }
  return url.origin
}

function appendPath(base: string, path: string): string {
  return `${base.replace(/\/+$/, '')}/${path.replace(/^\/+/, '')}`
}

export function getCCSwitchEndpoint(app: string): string {
  const serverAddress = getCCSwitchServerAddress()
  return app === 'codex' ? appendPath(serverAddress, 'v1') : serverAddress
}

// The URL builder is shared by the existing API key action and the usage guide.
export function buildCCSwitchURL(
  app: string,
  name: string,
  models: Record<string, string>,
  apiKey: string
): string {
  const serverAddress = getCCSwitchServerAddress()
  const endpoint = getCCSwitchEndpoint(app)
  const params = new URLSearchParams()
  params.set('resource', 'provider')
  params.set('app', app)
  params.set('name', name)
  params.set('endpoint', endpoint)
  params.set('apiKey', apiKey)
  for (const [key, value] of Object.entries(models)) {
    if (value) params.set(key, value)
  }
  params.set('homepage', serverAddress)
  params.set('enabled', 'true')
  params.set('usageEnabled', 'true')
  params.set('usageScript', btoa(CC_SWITCH_TOKEN_USAGE_SCRIPT))
  params.set('usageBaseUrl', serverAddress)
  params.set('usageAutoInterval', '30')
  return `ccswitch://v1/import?${params.toString()}`
}
