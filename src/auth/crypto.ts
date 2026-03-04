/**
 * Cryptographic utilities for token generation.
 *
 * Shared across API token auth and OAuth flows.
 */

/**
 * Generate a cryptographically secure random hex string.
 * @param length Number of random bytes (output will be 2x this in hex chars)
 */
export function generateSecureToken(length: number = 32): string {
  const array = new Uint8Array(length);
  crypto.getRandomValues(array);
  return Array.from(array)
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}
