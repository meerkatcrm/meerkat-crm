import { apiJson } from './client';

export async function requestPasswordReset(email: string): Promise<string> {
  const data = await apiJson<{ message?: string } | undefined>('/password-reset/request', {
    method: 'POST',
    body: JSON.stringify({ email }),
    errorMessage: 'Unable to request password reset.',
  });
  return data?.message || 'If an account exists, password reset instructions were sent.';
}

export async function confirmPasswordReset(token: string, password: string): Promise<string> {
  const data = await apiJson<{ message?: string } | undefined>('/password-reset/confirm', {
    method: 'POST',
    body: JSON.stringify({ token, password }),
    errorMessage: 'Unable to reset password.',
  });
  return data?.message || 'Password reset successful.';
}

export async function changePassword(currentPassword: string, newPassword: string): Promise<string> {
  const data = await apiJson<{ message?: string } | undefined>('/users/change-password', {
    method: 'POST',
    body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
    errorMessage: 'Unable to change password.',
  });
  return data?.message || 'Password updated successfully.';
}
