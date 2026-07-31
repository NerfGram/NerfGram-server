# Complete FluxGram asset import pipeline (emoji, reactions, star gifts).
$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot\..

$env:SESSION = "$env:TEMP\appearance.session"
$adminToken = 'changeme_admin_api_token'
$adminAPI = 'http://localhost:2399'

function Update-SetShortName {
    param([string]$SetDir, [string]$ShortName)
    $info = Join-Path $SetDir 'set_info.json'
    if (-not (Test-Path $info)) { return }
    $json = Get-Content $info -Raw | ConvertFrom-Json
    $json.result.set.short_name = $ShortName
    $json | ConvertTo-Json -Depth 100 | Set-Content $info -Encoding UTF8
}

Write-Output '=== reorganize sticker-seed ==='
$seed = '.\data\sticker-seed'
$emojiExport = Join-Path $seed 'telegram_emoji_export'
$defaultExport = Join-Path $seed 'telegram_default_stickers_export'
New-Item -ItemType Directory -Force -Path $emojiExport | Out-Null
New-Item -ItemType Directory -Force -Path $defaultExport | Out-Null

$moves = @(
    @{ From = 'RestrictedEmoji_7173162320003080'; To = Join-Path $emojiExport 'RestrictedEmoji_7173162320003080' },
    @{ From = 'gradientnye_ikonki_by_TgEmodziBot_4871560303222980605'; To = Join-Path $emojiExport 'NewsGradient_4871560303222980605'; Short = 'NewsGradient' },
    @{ From = 'tgiosicons_7489288727785635936'; To = Join-Path $emojiExport 'tgiosicons_7489288727785635936' },
    @{ From = 'NewsEmoji_1269403972611866648'; To = Join-Path $emojiExport 'NewsEmoji_1269403972611866648' },
    @{ From = 'DevelopersEmoji_by_TgEmodziBot_4871560303222980601'; To = Join-Path $emojiExport 'DevEssentials_4871560303222980601'; Short = 'DevEssentials' },
    @{ From = 'ApplicationEmojiUltra_by_TgEmodziBot_4871560303222980604'; To = Join-Path $emojiExport 'ApplicationEmoji_4871560303222980604'; Short = 'ApplicationEmoji' },
    @{ From = 'GiftsPremium_1258816259752003'; To = Join-Path $defaultExport 'DefaultSet_PremiumGifts' },
    @{ From = 'GiftsTons_1258816259752006'; To = Join-Path $defaultExport 'DefaultSet_TonGifts' },
    @{ From = 'StatusPack_773947703670341676'; To = Join-Path $defaultExport 'DefaultSet_EmojiChannelDefaultStatuses' }
)
foreach ($m in $moves) {
    $src = Join-Path $seed $m.From
    if (-not (Test-Path $src)) { continue }
    if (Test-Path $m.To) { Remove-Item $m.To -Recurse -Force }
    Move-Item $src $m.To
    if ($m.Short) { Update-SetShortName -SetDir $m.To -ShortName $m.Short }
}

Write-Output '=== build fetch tools ==='
go build -o stickerfetch.exe ./cmd/stickerfetch
go build -o giftfetch.exe ./cmd/giftfetch

Write-Output '=== fetch reactions ==='
& .\stickerfetch.exe .\data\sticker-seed reactions
if ($LASTEXITCODE -ne 0) { throw "stickerfetch reactions failed with exit $LASTEXITCODE" }

Write-Output '=== finish official-gifts manifest ==='
$manifest = '.\data\official-gifts\manifest.json'
if (-not (Test-Path $manifest)) {
    for ($i = 1; $i -le 10; $i++) {
        Write-Output "giftfetch attempt $i"
        & .\giftfetch.exe -out .\data\official-gifts -reuse-metadata
        if ($LASTEXITCODE -eq 0 -and (Test-Path $manifest)) { break }
        Start-Sleep -Seconds ([int](20 * $i))
    }
}
if (-not (Test-Path $manifest)) { throw 'manifest.json still missing after giftfetch retries' }

Write-Output '=== restart telesrv to seed media ==='
docker compose restart telesrv
Start-Sleep -Seconds 25

Write-Output '=== import star gifts via admin API ==='
$headers = @{ Authorization = "Bearer $adminToken" }
$gifts = Invoke-RestMethod -Uri "$adminAPI/v1/official-gifts" -Headers $headers -Method Get

$starNames = @('heart','bear','gift','flower','cake','flowers','rocket','win','diamond ring','diamond','bottle')
$nftNames = @('instant ramen','input key','plush pepe','star notepad')
$sort = 0
$imported = @()

foreach ($g in $gifts) {
    $title = [string]$g.title
    if ([string]::IsNullOrWhiteSpace($title)) { continue }
    $lower = $title.ToLowerInvariant()
    $wantCollectible = $false
    $match = $false
    foreach ($n in $starNames) {
        if ($lower -eq $n -or $lower -like "*$n*") { $match = $true; break }
    }
    if (-not $match) {
        foreach ($n in $nftNames) {
            if ($lower -eq $n -or $lower -like "*$n*") { $match = $true; $wantCollectible = $true; break }
        }
    }
    if (-not $match) { continue }

    $body = @{
        command_id = "import-$($g.source_gift_id)"
        reason = 'bulk import'
        confirm = $true
        source_gift_id = [string]$g.source_gift_id
        title = $title
        stars = [int64]$g.stars
        convert_stars = [int64]$g.convert_stars
        enabled = $true
        sort_order = $sort
        include_collectible = $wantCollectible
        upgrade_stars = [int64]$g.upgrade_stars
        supply_total = [int]$g.availability_total
    } | ConvertTo-Json
    try {
        $result = Invoke-RestMethod -Uri "$adminAPI/v1/official-gifts/import" -Headers $headers -Method Post -Body $body -ContentType 'application/json'
        $imported += "$title ($($g.source_gift_id))"
        $sort++
        Write-Output "imported: $title"
    } catch {
        Write-Output "import failed for $title : $($_.Exception.Message)"
    }
}

Write-Output '=== verify reactions seeded ==='
# telesrv has no shell; check via admin postgres indirectly by restarting logs
docker compose logs telesrv --tail 40

Write-Output '=== done ==='
Write-Output "imported gifts: $($imported.Count)"
$imported | ForEach-Object { Write-Output "  $_" }
