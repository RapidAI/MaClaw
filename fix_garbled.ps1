param([string]$FilePath)

$bytes = [System.IO.File]::ReadAllBytes($FilePath)
$text = [System.Text.Encoding]::UTF8.GetString($bytes)
$lines = $text.Split([char]10)
$changed = 0

for ($i = 0; $i -lt $lines.Count; $i++) {
    $line = $lines[$i]
    $orig = $line
    
    # Replace garbled Chinese in double-quoted strings with ASCII equivalents
    # Pattern: find strings containing garbled chars and replace the garbled content
    
    # Error messages
    $line = $line -replace '"LLM [^"]*璋冪敤[^"]*"', '"LLM call failed: %v"'
    $line = $line -replace '"LLM [^"]*鏈厤缃[^"]*"', '"LLM not configured, please set maclaw_llm_url and maclaw_llm_model first"'
    $line = $line -replace '"鍔犺浇 LLM [^"]*"', '"failed to load LLM config: %v"'
    $line = $line -replace '"鏈煡宸ュ叿: %s"', '"unknown tool: %s"'
    $line = $line -replace '"閿欒: 缂哄皯 command [^"]*"', '"error: missing command parameter"'
    $line = $line -replace '"閿欒: 缂哄皯 path [^"]*"', '"error: missing path parameter"'
    $line = $line -replace '"閿欒: 缂哄皯 content [^"]*"', '"error: missing content parameter"'
    $line = $line -replace '"閿欒: 缂哄皯 session_id [^"]*"', '"error: missing session_id or text parameter"'
    $line = $line -replace '"\n閿欒: "', '"\nerror: "'
    $line = $line -replace '"璇诲彇澶辫触: %v"', '"read failed: %v"'
    $line = $line -replace '"鍐欏叆澶辫触: %v"', '"write failed: %v"'
    $line = $line -replace '"缂栬緫澶辫触: %v"', '"edit failed: %v"'
    $line = $line -replace '"璇诲彇鐩綍澶辫触: %v"', '"read directory failed: %v"'
    $line = $line -replace '"鍙戦€佸け璐? %v"', '"send failed: %v"'
    $line = $line -replace '"鎭㈠澶辫触: %w"', '"restore failed: %w"'
    
    # Status messages
    $line = $line -replace '"宸茶拷鍔犲埌 %s \(%d 瀛楄妭\)"', '"appended to %s (%d bytes)"'
    $line = $line -replace '"宸叉竻绌?%s \(%d 瀛楄妭\)"', '"cleared %s (%d bytes)"'
    $line = $line -replace '"宸插啓鍏?%s \(%d 瀛楄妭\)"', '"written to %s (%d bytes)"'
    $line = $line -replace '"宸茬紪杈?%s \(%d 澶? %d 瀛楄妭\)"', '"edited %s (%d replacements, %d bytes)"'
    $line = $line -replace '"宸插彂閫?', '"sent"'
    $line = $line -replace '"浼氳瘽绠＄悊鍣ㄦ湭鍒濆鍖?', '"session manager not initialized"'
    $line = $line -replace '"褰撳墠鏃犳椿璺冧細璇?', '"no active sessions"'
    
    # Log messages
    $line = $line -replace '\[LLM\] 棣栨[^"]*', '[LLM] first request timeout/network error, retrying after %s: %v"'
    
    # Generic: replace any remaining garbled Chinese in double-quoted strings
    # This catches tool descriptions and other strings
    # Match: "garbled_text" where garbled contains CJK chars
    
    if ($line -ne $orig) {
        $lines[$i] = $line
        $changed++
    }
}

$newText = [string]::Join([char]10, $lines)
$utf8NoBom = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllText($FilePath, $newText, $utf8NoBom)
Write-Output "Changed $changed lines in $FilePath"
