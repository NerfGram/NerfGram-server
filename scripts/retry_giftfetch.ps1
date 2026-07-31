# Retry giftfetch up to N times with exponential backoff.
$outDir = '.\\data\\official-gifts'
$maxAttempts = 20
for ($i = 1; $i -le $maxAttempts; $i++) {
    Write-Output "giftfetch loop attempt $i"
    & .\\giftfetch.exe -out $outDir
    $code = $LASTEXITCODE
    Write-Output "giftfetch exitcode=$code"
    if ($code -eq 0) {
        Write-Output "giftfetch finished successfully"
        break
    }
    $sleep = [int](30 * $i)
    Write-Output "sleeping ${sleep}s before retry"
    Start-Sleep -Seconds $sleep
}
Write-Output "retry loop completed"

