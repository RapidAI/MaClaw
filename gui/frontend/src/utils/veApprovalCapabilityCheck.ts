/**
 * VE Approval Capability Check Utility
 *
 * Provides validation for assigning VEs as approvers in the workflow designer.
 * When a user selects a VE as an approver in the approval node configuration panel,
 * this utility checks whether that VE has the approval capability enabled.
 *
 * Requirement 2.6: WHEN the user assigns a VE as an approver and that VE does not
 * have approval capability enabled, THE Workflow_Designer SHALL prevent the assignment
 * and display an error message indicating that the selected VE lacks approval capability.
 */

import { CheckVEApprovalCapabilityStatus, ValidateVEApproverAssignment } from '../../wailsjs/go/main/App';

/**
 * Result of a VE approval capability check.
 */
export interface VEApprovalCapabilityResult {
  veId: string;
  hasCapability: boolean;
  enabled: boolean;
  error?: string;
}

/**
 * Validation result for a VE approver assignment.
 */
export interface VEApproverValidationResult {
  valid: boolean;
  errorMessage?: string;
}

/**
 * Checks whether a VE has approval capability enabled.
 * Used by the approval node configuration panel to validate VE selection.
 *
 * @param veId - The VE machine ID to check
 * @returns Promise resolving to the capability status
 */
export async function checkVEApprovalCapability(veId: string): Promise<VEApprovalCapabilityResult> {
  if (!veId || veId.trim() === '') {
    return {
      veId: veId || '',
      hasCapability: false,
      enabled: false,
      error: 'VE ID is required',
    };
  }

  try {
    const status = await CheckVEApprovalCapabilityStatus(veId);
    return {
      veId: status.ve_id || veId,
      hasCapability: status.has_capability || false,
      enabled: status.enabled || false,
      error: status.error || undefined,
    };
  } catch (err) {
    return {
      veId,
      hasCapability: false,
      enabled: false,
      error: err instanceof Error ? err.message : String(err),
    };
  }
}

/**
 * Validates that a VE can be assigned as an approver in the workflow designer.
 * Returns a validation result indicating whether the assignment is valid.
 * If invalid, includes an error message suitable for display in the UI.
 *
 * @param veId - The VE machine ID to validate
 * @returns Promise resolving to the validation result
 */
export async function validateVEApproverAssignment(veId: string): Promise<VEApproverValidationResult> {
  if (!veId || veId.trim() === '') {
    return {
      valid: false,
      errorMessage: 'Please select a VE to assign as approver.',
    };
  }

  try {
    await ValidateVEApproverAssignment(veId);
    return { valid: true };
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    return {
      valid: false,
      errorMessage: formatApprovalCapabilityError(message),
    };
  }
}

/**
 * Formats the error message from the backend into a user-friendly message
 * for display in the workflow designer's approval node configuration panel.
 */
function formatApprovalCapabilityError(backendError: string): string {
  if (backendError.includes('does not have approval capability enabled')) {
    return 'This VE does not have approval capability enabled. Please enable it in the VE\'s Approval Settings before assigning as approver.';
  }
  if (backendError.includes('not found')) {
    return 'The selected VE could not be found. It may be offline or not registered.';
  }
  if (backendError.includes('hub URL not configured')) {
    return 'Cannot verify VE capability: Hub connection is not configured.';
  }
  if (backendError.includes('hub client not available')) {
    return 'Cannot verify VE capability: Hub is not connected.';
  }
  // Fallback: return the backend error as-is.
  return `Cannot assign VE as approver: ${backendError}`;
}
