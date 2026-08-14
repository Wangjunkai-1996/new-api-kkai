/*
Copyright (C) 2025 QuantumNous

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

export const DEFAULT_TOP_UP_LINK = 'https://catfk.com/shop/R8OHTQ73';

// Reject non-navigable schemes and relative URLs from backend configuration.
export function isSafeHttpCheckoutUrl(value) {
  const trimmed = (value || '').trim();
  if (!trimmed) {
    return false;
  }

  try {
    const url = new URL(trimmed);
    return url.protocol === 'http:' || url.protocol === 'https:';
  } catch {
    return false;
  }
}

export function resolveTopUpLink(value) {
  const candidate = (value || '').trim();
  if (!isSafeHttpCheckoutUrl(candidate)) {
    return DEFAULT_TOP_UP_LINK;
  }

  return new URL(candidate).toString();
}
