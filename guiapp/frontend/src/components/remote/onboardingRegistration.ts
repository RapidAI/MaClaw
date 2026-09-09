export type RegistrationTarget = { hubURL: string; hubID: string; tenantID: string };

export type RegistrationVerificationResetters = {
    invalidateSMSRequest: () => void; invalidateEmailRequest: () => void;
    setSMSCodeSending: (value: boolean) => void; setEmailCodeSending: (value: boolean) => void;
    setSMSCountdown: (value: number) => void; setEmailCountdown: (value: number) => void;
    setSMSCode: (value: string) => void; setSMSTargetPhone: (value: string) => void;
    setEmailCode: (value: string) => void; setEmailTarget: (value: string) => void;
    setEmailError: (value: string) => void; setVerifiedCode: (value: string) => void;
};

export function extractDailySMSLimit(message: string): number | null {
    const limit = Number(message.match(/max\s+(\d+)\s+per\s+day/i)?.[1]);
    return Number.isFinite(limit) && limit > 0 ? limit : null;
}

export function resetVerificationForInvitationChange(args: RegistrationVerificationResetters & {
    nextCode: string; verifiedCode: string; baseTarget: RegistrationTarget; fallbackHubURL: string;
    setHubURL: (value: string) => void; setHubID: (value: string) => void; setTenantID: (value: string) => void;
}) {
    if (args.nextCode.trim() === args.verifiedCode) return;
    resetRegistrationVerification(args);
    args.setHubURL(args.baseTarget.hubURL || args.fallbackHubURL);
    args.setHubID(args.baseTarget.hubID); args.setTenantID(args.baseTarget.tenantID);
}

export function resetRegistrationVerification(args: RegistrationVerificationResetters) {
    args.invalidateSMSRequest(); args.invalidateEmailRequest();
    args.setSMSCodeSending(false); args.setEmailCodeSending(false);
    args.setSMSCountdown(0); args.setEmailCountdown(0);
    args.setSMSCode(""); args.setSMSTargetPhone(""); args.setEmailCode(""); args.setEmailTarget(""); args.setEmailError(""); args.setVerifiedCode("");
}
