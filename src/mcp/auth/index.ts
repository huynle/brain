/**
 * OAuth 2.1 Authentication for MCP Server
 *
 * Provides OAuth 2.1 authentication with:
 * - Dynamic Client Registration (RFC 7591)
 * - PKCE (RFC 7636)
 * - Authorization Server Metadata (RFC 8414)
 * - Bearer Token Authentication
 */

// Routes
export { createOAuthRoutes } from "./routes";

// Middleware
export {
  bearerAuth,
  optionalBearerAuth,
  requireScope,
  conditionalAuth,
} from "./middleware";

// Stores
export {
  clientStore,
  authCodeStore,
  accessTokenStore,
  refreshTokenStore,
  cleanupExpired,
} from "./stores";

// PKCE utilities
export {
  verifyPkceChallenge,
  isValidCodeChallenge,
  generateCodeVerifier,
  generateCodeChallenge,
} from "./pkce";

// Types
export type {
  OAuthClient,
  ClientRegistrationRequest,
  ClientRegistrationResponse,
  AuthorizationCode,
  AccessToken,
  RefreshToken,
  TokenRequest,
  TokenResponse,
  TokenErrorResponse,
  OAuthServerMetadata,
  ProtectedResourceMetadata,
  OAuthConfig,
  OAuthErrorCode,
} from "./types";

export { OAuthError } from "./types";
