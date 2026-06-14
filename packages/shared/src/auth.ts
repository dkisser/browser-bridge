export interface AuthProvider {
  id: string;
  validateToken(token: string): Promise<AuthResult>;
  refreshToken(token: AuthToken): Promise<AuthToken>;
}

export interface AuthToken {
  accessToken: string;
  refreshToken: string;
  expiresAt: number;
  userId: string;
}

export interface AuthResult {
  valid: boolean;
  userId: string;
  permissions: string[];
}

export class NoopAuthProvider implements AuthProvider {
  id = 'noop';

  async validateToken(_token: string): Promise<AuthResult> {
    return { valid: true, userId: 'local', permissions: ['*'] };
  }

  async refreshToken(token: AuthToken): Promise<AuthToken> {
    return token;
  }
}

export class ApiKeyAuthProvider implements AuthProvider {
  id = 'api-key';
  private validKeys: Map<string, string>;

  constructor(keys: Record<string, string>) {
    this.validKeys = new Map(Object.entries(keys));
  }

  async validateToken(token: string): Promise<AuthResult> {
    const userId = this.validKeys.get(token);
    if (!userId) {
      return { valid: false, userId: '', permissions: [] };
    }
    return { valid: true, userId, permissions: ['*'] };
  }

  async refreshToken(token: AuthToken): Promise<AuthToken> {
    return token;
  }
}
