/**
 * PKCE (Proof Key for Code Exchange) Utilities
 *
 * Implements RFC 7636 for OAuth 2.1 PKCE validation.
 * Only S256 method is supported (plain is deprecated in OAuth 2.1).
 */

/**
 * Verify a code_verifier against a code_challenge using S256 method.
 *
 * S256: BASE64URL(SHA256(code_verifier)) === code_challenge
 */
export async function verifyPkceChallenge(
  codeVerifier: string,
  codeChallenge: string
): Promise<boolean> {
  if (!codeVerifier || !codeChallenge) {
    return false;
  }

  // Validate code_verifier format (43-128 chars, unreserved chars only)
  if (codeVerifier.length < 43 || codeVerifier.length > 128) {
    return false;
  }

  // Only unreserved characters allowed: [A-Z] / [a-z] / [0-9] / "-" / "." / "_" / "~"
  const validPattern = /^[A-Za-z0-9\-._~]+$/;
  if (!validPattern.test(codeVerifier)) {
    return false;
  }

  try {
    // Compute SHA-256 hash
    const encoder = new TextEncoder();
    const data = encoder.encode(codeVerifier);
    const hashBuffer = await crypto.subtle.digest("SHA-256", data);

    // Convert to base64url (RFC 4648 Section 5)
    const hashArray = new Uint8Array(hashBuffer);
    const base64 = btoa(String.fromCharCode(...hashArray));
    const base64url = base64
      .replace(/\+/g, "-")
      .replace(/\//g, "_")
      .replace(/=+$/, "");

    // Compare with constant-time comparison
    return constantTimeCompare(base64url, codeChallenge);
  } catch {
    return false;
  }
}

/**
 * Validate code_challenge format.
 * Must be base64url encoded (43 chars for SHA-256).
 */
export function isValidCodeChallenge(codeChallenge: string): boolean {
  if (!codeChallenge) return false;

  // SHA-256 base64url encoded is 43 characters
  if (codeChallenge.length !== 43) return false;

  // Only base64url characters allowed
  const base64urlPattern = /^[A-Za-z0-9\-_]+$/;
  return base64urlPattern.test(codeChallenge);
}

/**
 * Constant-time string comparison to prevent timing attacks.
 */
function constantTimeCompare(a: string, b: string): boolean {
  if (a.length !== b.length) {
    return false;
  }

  let result = 0;
  for (let i = 0; i < a.length; i++) {
    result |= a.charCodeAt(i) ^ b.charCodeAt(i);
  }

  return result === 0;
}

/**
 * Generate a secure code_verifier for testing purposes.
 */
export function generateCodeVerifier(): string {
  const length = 64; // Between 43-128
  const array = new Uint8Array(length);
  crypto.getRandomValues(array);

  // Use base64url alphabet for unreserved chars
  const chars =
    "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~";
  return Array.from(array)
    .map((b) => chars[b % chars.length])
    .join("");
}

/**
 * Generate code_challenge from code_verifier using S256.
 */
export async function generateCodeChallenge(
  codeVerifier: string
): Promise<string> {
  const encoder = new TextEncoder();
  const data = encoder.encode(codeVerifier);
  const hashBuffer = await crypto.subtle.digest("SHA-256", data);
  const hashArray = new Uint8Array(hashBuffer);
  const base64 = btoa(String.fromCharCode(...hashArray));
  return base64.replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}
