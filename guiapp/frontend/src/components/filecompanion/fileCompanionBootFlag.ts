export function readFileCompanionBootFlag(): boolean {
    const flag = (window as unknown as { __MACLAW_FILE_COMPANION__?: boolean }).__MACLAW_FILE_COMPANION__;
    return flag === true;
}
