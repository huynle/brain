/**
 * OAuth 2.1 Types for MCP Server Authentication
 *
 * Implements OAuth 2.1 with PKCE and Dynamic Client Registration (RFC 7591)
 * for Claude connector compatibility.
 */

// =============================================================================
// OAuth Client Types
// =============================================================================

export interface OAuthClient {
  client_id: string;
  client_secret: string;
  redirect_uris: string[];
  client_name?: string;
  client_uri?: string;
  logo_uri?: string;
  scope?: string;
  grant_types: string[];
  response_types: string[];
  token_endpoint_auth_method: "client_secret_post" | "client_secret_basic" | "none";
  created_at: number;
}

export interface ClientRegistrationRequest {
  redirect_uris: string[];
  client_name?: string;
  client_uri?: string;
  logo_uri?: string;
  scope?: string;
  grant_types?: string[];
  response_types?: string[];
  token_endpoint_auth_method?: "client_secret_post" | "client_secret_basic" | "none";
}

export interface ClientRegistrationResponse {
  client_id: string;
  client_secret: string;
  client_id_issued_at: number;
  client_secret_expires_at: number; // 0 means never expires
  redirect_uris: string[];
  client_name?: string;
  grant_types: string[];
  response_types: string[];
  token_endpoint_auth_method: string;
}

// =============================================================================
// Authorization Code Types
// =============================================================================

export interface AuthorizationCode {
  code: string;
  client_id: string;
  redirect_uri: string;
  scope: string;
  code_challenge: string;
  code_challenge_method: "S256";
  user_id?: string;
  expires_at: number;
  created_at: number;
}

export interface AuthorizeRequest {
  response_type: "code";
  client_id: string;
  redirect_uri: string;
  scope?: string;
  state?: string;
  code_challenge: string;
  code_challenge_method: "S256";
}

// =============================================================================
// Token Types
// =============================================================================

export interface AccessToken {
  token: string;
  token_type: "Bearer";
  client_id: string;
  scope: string;
  user_id?: string;
  expires_at: number;
  created_at: number;
}

export interface RefreshToken {
  token: string;
  client_id: string;
  scope: string;
  user_id?: string;
  expires_at: number;
  created_at: number;
}

export interface TokenRequest {
  grant_type: "authorization_code" | "refresh_token";
  code?: string;
  redirect_uri?: string;
  code_verifier?: string;
  refresh_token?: string;
  client_id?: string;
  client_secret?: string;
}

export interface TokenResponse {
  access_token: string;
  token_type: "Bearer";
  expires_in: number;
  refresh_token?: string;
  scope: string;
}

export interface TokenErrorResponse {
  error: string;
  error_description?: string;
}

// =============================================================================
// Discovery Types (RFC 8414)
// =============================================================================

export interface OAuthServerMetadata {
  issuer: string;
  authorization_endpoint: string;
  token_endpoint: string;
  registration_endpoint?: string;
  jwks_uri?: string;
  scopes_supported?: string[];
  response_types_supported: string[];
  grant_types_supported: string[];
  token_endpoint_auth_methods_supported: string[];
  code_challenge_methods_supported: string[];
  service_documentation?: string;
}

export interface ProtectedResourceMetadata {
  resource: string;
  authorization_servers: string[];
  bearer_methods_supported: string[];
  scopes_supported?: string[];
}

// =============================================================================
// OAuth Configuration
// =============================================================================

export interface OAuthConfig {
  enabled: boolean;
  issuer: string;
  accessTokenTtl: number; // seconds
  refreshTokenTtl: number; // seconds
  authCodeTtl: number; // seconds
  allowedScopes: string[];
}

// =============================================================================
// Error Types
// =============================================================================

export type OAuthErrorCode =
  | "invalid_request"
  | "invalid_client"
  | "invalid_grant"
  | "unauthorized_client"
  | "unsupported_grant_type"
  | "invalid_scope"
  | "access_denied"
  | "server_error";

export class OAuthError extends Error {
  constructor(
    public code: OAuthErrorCode,
    message: string,
    public statusCode: number = 400
  ) {
    super(message);
    this.name = "OAuthError";
  }

  toJSON(): TokenErrorResponse {
    return {
      error: this.code,
      error_description: this.message,
    };
  }
}
