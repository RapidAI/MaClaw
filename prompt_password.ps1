param(
    [string]$Prompt = "Password",
    [string]$OutputPath = ""
)

$secure = Read-Host $Prompt -AsSecureString
$bstr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
try {
    $plain = [Runtime.InteropServices.Marshal]::PtrToStringAuto($bstr)
    if ([string]::IsNullOrEmpty($OutputPath)) {
        $plain
    } else {
        [System.IO.File]::WriteAllText($OutputPath, $plain, [System.Text.Encoding]::ASCII)
    }
}
finally {
    [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($bstr)
}
