export const usernamePattern = /^[a-z0-9_-]{3,32}$/;

export const minPasswordLength = 8;

export function isValidUsername(value: string): boolean {
  return usernamePattern.test(value);
}

export function isValidPassword(value: string): boolean {
  return value.length >= minPasswordLength;
}
