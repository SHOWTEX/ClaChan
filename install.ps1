#requires -Version 5.1
<#
Clachan installer, updater, and uninstaller for Windows.

Install or update from a normal (non-elevated) PowerShell window:

  powershell -NoProfile -ExecutionPolicy Bypass -Command "irm https://github.com/SHOWTEX/ClaChan/releases/latest/download/install.ps1 | iex"

For a fork or a local test, pass -Repo owner/repo. Clachan installs per user, so
no administrator rights are needed unless you also want to open the firewall.
#>
[CmdletBinding()]
param(
    [switch]$Update,
    [switch]$Uninstall,
    [switch]$Purge,
    [switch]$NoStart,
    [switch]$NoTask,
    [switch]$NoShortcut,
    [switch]$OpenFirewall,
    [switch]$Yes,
    [switch]$Help,
    [string]$Repo = "",
    [string]$Release = "latest",
    [string]$Port = "",
    [string]$InstallDir = ""
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$DefaultRepo = "SHOWTEX/ClaChan"
$AppName = "Clachan"

function Show-Usage {
    @"
Clachan installer for Windows

Usage:
  powershell -NoProfile -ExecutionPolicy Bypass -File install.ps1 [options]

Options:
  -Update           Update an existing installation.
  -Uninstall        Stop and remove the service and binary.
  -Purge            With -Uninstall, also delete config and data.
  -NoStart          Install or update without starting Clachan.
  -NoTask           Do not register the start-at-login task.
  -NoShortcut       Do not create Start Menu entries or an uninstall entry.
  -OpenFirewall     Allow the port through Windows Firewall (needs admin).
  -Yes              Answer yes to confirmation prompts.
  -Repo OWNER/REPO  GitHub repository to download from.
  -Release VERSION  Release tag to install (default: latest).
  -Port PORT        Port for a new configuration (default: 8080).
  -InstallDir PATH  Install directory (default: %LOCALAPPDATA%\Clachan).
  -Help             Show this help.

Environment:
  CLACHAN_REPO, CLACHAN_RELEASE, CLACHAN_API_KEY, CLACHAN_PORT,
  CLACHAN_INSTALL_DIR, CLACHAN_NO_PROMPT, CLACHAN_BASE_URL
"@
}

function Fail {
    param([string]$Message)
    Write-Host ""
    Write-Host "Error: $Message" -ForegroundColor Red
    throw $Message
}

function Write-Step {
    param([string]$Message)
    Write-Host "==> $Message" -ForegroundColor Cyan
}

function Write-TextFile {
    param([string]$Path, [string]$Text)
    $utf8 = New-Object System.Text.UTF8Encoding($false)
    [IO.File]::WriteAllText($Path, $Text, $utf8)
}

function Read-ApiKey {
    if ($env:CLACHAN_API_KEY) {
        return $env:CLACHAN_API_KEY
    }
    if ($env:CLACHAN_NO_PROMPT -or -not [Environment]::UserInteractive) {
        return ""
    }

    $secure = Read-Host -AsSecureString "OpenRouter API key (leave blank to set later)"
    if ($null -eq $secure -or $secure.Length -eq 0) {
        return ""
    }

    $bstr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
    try {
        return [Runtime.InteropServices.Marshal]::PtrToStringBSTR($bstr)
    }
    finally {
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($bstr)
    }
}

function Get-EnvValue {
    param([string]$Path, [string]$Key)
    if (-not (Test-Path -LiteralPath $Path)) {
        return ""
    }
    $prefix = $Key + "="
    $line = @(Get-Content -LiteralPath $Path) |
        Where-Object { $_.StartsWith($prefix, [StringComparison]::Ordinal) } |
        Select-Object -Last 1
    if ($null -eq $line) {
        return ""
    }
    return $line.Substring($prefix.Length).Trim().Trim('"').Trim("'")
}

function Set-EnvValue {
    param([string]$Path, [string]$Key, [string]$Value)
    $prefix = $Key + "="
    $lines = @()
    if (Test-Path -LiteralPath $Path) {
        $lines = @(Get-Content -LiteralPath $Path)
    }

    $found = $false
    $updated = foreach ($line in $lines) {
        if ($line.StartsWith($prefix, [StringComparison]::Ordinal)) {
            $found = $true
            $prefix + $Value
        }
        else {
            $line
        }
    }
    if (-not $found) {
        $updated = @($updated) + @($prefix + $Value)
    }
    $text = (@($updated) -join "`r`n") + "`r`n"
    Write-TextFile -Path $Path -Text $text
}

function Get-Port {
    param([string]$Value)
    if (-not $Value) {
        return 8080
    }
    $number = 0
    if (-not [int]::TryParse($Value, [ref]$number) -or $number -lt 1 -or $number -gt 65535) {
        Fail "invalid port: $Value"
    }
    return $number
}

function Get-InstalledPort {
    param([string]$Path)
    $addr = Get-EnvValue -Path $Path -Key "CLACHAN_ADDR"
    if ($addr -match ':(\d+)\s*$') {
        return [int]$Matches[1]
    }
    return 8080
}

function Stop-ClachanApp {
    param([string]$ExePath)
    $full = [IO.Path]::GetFullPath($ExePath)
    Get-CimInstance Win32_Process -Filter "Name = 'clachan.exe'" -ErrorAction SilentlyContinue |
        Where-Object {
            $_.ExecutablePath -and ([IO.Path]::GetFullPath($_.ExecutablePath) -ieq $full)
        } |
        ForEach-Object {
            Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue
        }
    Start-Sleep -Milliseconds 300
}

function Wait-Health {
    param([int]$PortNumber)
    $url = "http://127.0.0.1:$PortNumber/healthz"
    for ($i = 0; $i -lt 40; $i++) {
        try {
            $response = Invoke-WebRequest -UseBasicParsing -Uri $url -TimeoutSec 1
            if ($response.StatusCode -eq 200) {
                return $true
            }
        }
        catch {
            # The server is still starting.
        }
        Start-Sleep -Milliseconds 250
    }
    return $false
}

function Start-ClachanHidden {
    param([string]$ScriptPath)
    Start-Process -FilePath "powershell.exe" `
        -ArgumentList "-NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -File `"$ScriptPath`"" `
        -WindowStyle Hidden
}

function Test-Administrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($identity)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

if ($Help) {
    Show-Usage
    return
}

if (-not $Repo) {
    if ($env:CLACHAN_REPO) {
        $Repo = $env:CLACHAN_REPO
    }
    else {
        $Repo = $DefaultRepo
    }
}
if (-not $InstallDir) {
    if ($env:CLACHAN_INSTALL_DIR) {
        $InstallDir = $env:CLACHAN_INSTALL_DIR
    }
    else {
        $localAppData = $env:LOCALAPPDATA
        if (-not $localAppData) {
            $localAppData = Join-Path $env:USERPROFILE "AppData\Local"
        }
        $InstallDir = Join-Path $localAppData "Clachan"
    }
}
$InstallDir = [IO.Path]::GetFullPath($InstallDir)
$Exe = Join-Path $InstallDir "clachan.exe"
$EnvFile = Join-Path $InstallDir ".env"
$DataDir = Join-Path $InstallDir "data"
$LogDir = Join-Path $InstallDir "logs"
$InstallScript = Join-Path $InstallDir "install.ps1"
$RunScript = Join-Path $InstallDir "run-clachan.ps1"
$OpenScript = Join-Path $InstallDir "open-clachan.ps1"
$StopScript = Join-Path $InstallDir "stop-clachan.ps1"
$ShortcutDir = Join-Path $env:APPDATA "Microsoft\Windows\Start Menu\Programs\Clachan"
$RegistryKey = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\Clachan"
$IdentityName = [Security.Principal.WindowsIdentity]::GetCurrent().Name
$SafeIdentity = $IdentityName -replace '[^A-Za-z0-9_.-]', '_'
$TaskName = "Clachan-$SafeIdentity"
$isAdmin = Test-Administrator

if ($isAdmin) {
    Write-Host "Warning: this is an elevated shell. Clachan installs per user, so it will install into the administrator profile." -ForegroundColor Yellow
}

if ($Uninstall) {
    try { Set-Location $env:TEMP } catch { Set-Location $env:USERPROFILE }
    $present = (Test-Path -LiteralPath $Exe) -or
        (Test-Path -LiteralPath $EnvFile) -or
        (Test-Path -LiteralPath $DataDir) -or
        (Test-Path -LiteralPath $InstallScript)
    if (-not $present) {
        Write-Host "Clachan is not installed in $InstallDir."
        return
    }
    if (-not $Yes) {
        $prompt = if ($Purge) {
            "Remove Clachan, its configuration, and its data? [y/N] "
        }
        else {
            "Remove Clachan but keep its configuration and data? [y/N] "
        }
        $answer = Read-Host $prompt
        if ($answer -notmatch '^(?i:y|yes)$') {
            return
        }
    }

    Write-Step "Stopping Clachan"
    if (Test-Path -LiteralPath $Exe) {
        Stop-ClachanApp -ExePath $Exe
    }
    if (Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue) {
        Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false -ErrorAction SilentlyContinue
    }
    if (Test-Path -LiteralPath $ShortcutDir) {
        Remove-Item -LiteralPath $ShortcutDir -Recurse -Force -ErrorAction SilentlyContinue
    }
    if (Test-Path -LiteralPath $RegistryKey) {
        Remove-Item -LiteralPath $RegistryKey -Recurse -Force -ErrorAction SilentlyContinue
    }
    if ($isAdmin) {
        Get-NetFirewallRule -ErrorAction SilentlyContinue |
            Where-Object { $_.DisplayName -like "Clachan*" } |
            Remove-NetFirewallRule -ErrorAction SilentlyContinue
    }

    foreach ($file in @($Exe, $RunScript, $OpenScript, $StopScript)) {
        Remove-Item -LiteralPath $file -Force -ErrorAction SilentlyContinue
    }
    Remove-Item -LiteralPath $LogDir -Recurse -Force -ErrorAction SilentlyContinue

    if ($Purge) {
        Remove-Item -LiteralPath $InstallDir -Recurse -Force -ErrorAction SilentlyContinue
        Write-Host "Removed Clachan, its configuration, and its data." -ForegroundColor Green
    }
    else {
        Write-Host "Removed the Clachan program." -ForegroundColor Green
        Write-Host "Kept:"
        Write-Host "  $EnvFile"
        Write-Host "  $DataDir"
        if (Test-Path -LiteralPath $InstallScript) {
            Write-Host ""
            Write-Host "To delete those as well, run:"
            Write-Host "  powershell -NoProfile -ExecutionPolicy Bypass -File `"$InstallScript`" -Uninstall -Purge -Yes"
        }
    }
    return
}

if ($Repo -notmatch '^[^/]+/[^/]+$') {
    Fail "repository must look like OWNER/REPO; use -Repo OWNER/REPO"
}
$ProjectUrl = "https://github.com/$Repo"
if ($Port) {
    $null = Get-Port -Value $Port
}

$arch = $env:PROCESSOR_ARCHITEW6432
if (-not $arch) {
    $arch = $env:PROCESSOR_ARCHITECTURE
}
switch ($arch.ToUpperInvariant()) {
    "AMD64" { $goarch = "amd64" }
    "ARM64" { $goarch = "arm64" }
    default { Fail "unsupported architecture: $arch (supported: x64, arm64)" }
}
$Asset = "clachan-windows-$goarch.exe"

if ($env:CLACHAN_BASE_URL) {
    $Base = $env:CLACHAN_BASE_URL.TrimEnd('/')
}
elseif ($Release -eq "latest") {
    $Base = "https://github.com/$Repo/releases/latest/download"
}
else {
    $tag = $Release
    if (-not $tag.StartsWith("v")) {
        $tag = "v$tag"
    }
    $Base = "https://github.com/$Repo/releases/download/$tag"
}
$LatestBase = "https://github.com/$Repo/releases/latest/download"

$installed = Test-Path -LiteralPath $Exe
if ($Update -and -not $installed) {
    Fail "Clachan is not installed in $InstallDir"
}
if ($installed -and -not $Update) {
    Write-Step "Existing installation found; updating"
    $Update = $true
}

$tempDir = Join-Path ([IO.Path]::GetTempPath()) ("clachan-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
try {
    $download = Join-Path $tempDir $Asset
    $checksumsPath = Join-Path $tempDir "checksums.txt"
    Write-Step "Downloading $Asset"
    Invoke-WebRequest -UseBasicParsing -Uri "$Base/$Asset" -OutFile $download -Headers @{ "User-Agent" = "clachan-installer" }
    Invoke-WebRequest -UseBasicParsing -Uri "$Base/checksums.txt" -OutFile $checksumsPath -Headers @{ "User-Agent" = "clachan-installer" }

    $checksums = @(Get-Content -LiteralPath $checksumsPath)
    $line = $checksums | Where-Object { $_ -match ('\s' + [regex]::Escape($Asset) + '$') } | Select-Object -First 1
    if (-not $line) {
        Fail "no checksum found for $Asset"
    }
    $expected = ($line -split '\s+')[0].ToLowerInvariant()
    $actual = (Get-FileHash -LiteralPath $download -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -ne $expected) {
        Fail "checksum mismatch for $Asset"
    }
    Unblock-File -LiteralPath $download -ErrorAction SilentlyContinue

    $versionText = ((& $download --version) | Out-String).Trim()
    if ($LASTEXITCODE -ne 0) {
        Fail "downloaded binary failed to run"
    }
    $appVersion = ($versionText -split '\s+')[-1].TrimStart('v')

    New-Item -ItemType Directory -Path $InstallDir, $DataDir, $LogDir -Force | Out-Null

    if (-not (Test-Path -LiteralPath $EnvFile)) {
        Write-Step "Creating $EnvFile"
        $configPort = if ($Port) { Get-Port -Value $Port } else { 8080 }
        $config = @(
            "# Clachan configuration. Edit this file, then restart Clachan.",
            "OPENROUTER_API_KEY=",
            "CLACHAN_ADDR=:$configPort",
            "CLACHAN_DB=data/clachan.db",
            "OPENROUTER_REFERER=$ProjectUrl",
            "OPENROUTER_TITLE=Clachan"
        ) -join "`r`n"
        Write-TextFile -Path $EnvFile -Text ($config + "`r`n")
    }

    if ((Get-EnvValue -Path $EnvFile -Key "OPENROUTER_REFERER") -eq "http://clachan.local") {
        Set-EnvValue -Path $EnvFile -Key "OPENROUTER_REFERER" -Value $ProjectUrl
    }

    if (-not (Get-EnvValue -Path $EnvFile -Key "OPENROUTER_API_KEY")) {
        $apiKey = Read-ApiKey
        if ($apiKey) {
            Set-EnvValue -Path $EnvFile -Key "OPENROUTER_API_KEY" -Value $apiKey
        }
    }

    $runBody = @'
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$exe = Join-Path $root "clachan.exe"
$logDir = Join-Path $root "logs"
New-Item -ItemType Directory -Path $logDir -Force | Out-Null
$running = Get-CimInstance Win32_Process -Filter "Name = 'clachan.exe'" -ErrorAction SilentlyContinue |
    Where-Object { $_.ExecutablePath -and ([IO.Path]::GetFullPath($_.ExecutablePath) -ieq [IO.Path]::GetFullPath($exe)) }
if ($running) { return }
$log = Join-Path $logDir "clachan.log"
$err = Join-Path $logDir "clachan-error.log"
if (Test-Path -LiteralPath $log) { Move-Item -LiteralPath $log -Destination "$log.1" -Force }
if (Test-Path -LiteralPath $err) { Move-Item -LiteralPath $err -Destination "$err.1" -Force }
Start-Process -FilePath $exe -WorkingDirectory $root -WindowStyle Hidden -RedirectStandardOutput $log -RedirectStandardError $err
'@
    Write-TextFile -Path $RunScript -Text $runBody

    $openBody = @'
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
& (Join-Path $root "run-clachan.ps1")
$envFile = Join-Path $root ".env"
$port = 8080
$line = @(Get-Content -LiteralPath $envFile -ErrorAction SilentlyContinue) |
    Where-Object { $_.StartsWith("CLACHAN_ADDR=", [StringComparison]::Ordinal) } |
    Select-Object -Last 1
if ($line -match ':(\d+)\s*$') { $port = [int]$Matches[1] }
$url = "http://127.0.0.1:$port/"
for ($i = 0; $i -lt 40; $i++) {
    try {
        $response = Invoke-WebRequest -UseBasicParsing -Uri ($url + "healthz") -TimeoutSec 1
        if ($response.StatusCode -eq 200) { break }
    }
    catch { }
    Start-Sleep -Milliseconds 250
}
Start-Process $url
'@
    Write-TextFile -Path $OpenScript -Text $openBody

    $stopBody = @'
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$exe = Join-Path $root "clachan.exe"
Get-CimInstance Win32_Process -Filter "Name = 'clachan.exe'" -ErrorAction SilentlyContinue |
    Where-Object { $_.ExecutablePath -and ([IO.Path]::GetFullPath($_.ExecutablePath) -ieq [IO.Path]::GetFullPath($exe)) } |
    ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
'@
    Write-TextFile -Path $StopScript -Text $stopBody

    if ($PSCommandPath -and ([IO.Path]::GetFullPath($PSCommandPath) -ine [IO.Path]::GetFullPath($InstallScript))) {
        $installerText = Get-Content -LiteralPath $PSCommandPath -Raw
        Write-TextFile -Path $InstallScript -Text $installerText.Replace("SHOWTEX/ClaChan", $Repo)
    }
    elseif (-not (Test-Path -LiteralPath $InstallScript)) {
        try {
            Invoke-WebRequest -UseBasicParsing -Uri "$Base/install.ps1" -OutFile $InstallScript -Headers @{ "User-Agent" = "clachan-installer" }
        }
        catch {
            Write-Host "Warning: could not save the uninstaller; re-run the installer to uninstall." -ForegroundColor Yellow
        }
    }

    Write-Step "Installing $versionText"
    Stop-ClachanApp -ExePath $Exe
    $newExe = "$Exe.new"
    Copy-Item -LiteralPath $download -Destination $newExe -Force
    for ($i = 0; $i -lt 20; $i++) {
        try {
            Move-Item -LiteralPath $newExe -Destination $Exe -Force
            break
        }
        catch {
            if ($i -eq 19) { throw }
            Start-Sleep -Milliseconds 250
        }
    }

    if (-not $NoTask) {
        try {
            Write-Step "Registering start-at-login task"
            $action = New-ScheduledTaskAction -Execute "powershell.exe" -Argument "-NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -File `"$RunScript`""
            $trigger = New-ScheduledTaskTrigger -AtLogOn -User $IdentityName
            $settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable -ExecutionTimeLimit ([TimeSpan]::Zero)
            Register-ScheduledTask -TaskName $TaskName -Action $action -Trigger $trigger -Settings $settings -Description "Clachan local AI chat server" -Force | Out-Null
        }
        catch {
            Write-Host "Warning: could not register the start-at-login task: $_" -ForegroundColor Yellow
        }
    }

    if (-not $NoShortcut) {
        Write-Step "Creating Start Menu and uninstall entries"
        New-Item -ItemType Directory -Path $ShortcutDir -Force | Out-Null
        $shell = New-Object -ComObject WScript.Shell

        $openShortcut = $shell.CreateShortcut((Join-Path $ShortcutDir "Clachan.lnk"))
        $openShortcut.TargetPath = "powershell.exe"
        $openShortcut.Arguments = "-NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -File `"$OpenScript`""
        $openShortcut.WorkingDirectory = $InstallDir
        $openShortcut.IconLocation = "$Exe,0"
        $openShortcut.Description = "Open Clachan"
        $openShortcut.Save()

        $stopShortcut = $shell.CreateShortcut((Join-Path $ShortcutDir "Stop Clachan.lnk"))
        $stopShortcut.TargetPath = "powershell.exe"
        $stopShortcut.Arguments = "-NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -File `"$StopScript`""
        $stopShortcut.WorkingDirectory = $InstallDir
        $stopShortcut.IconLocation = "$Exe,0"
        $stopShortcut.Description = "Stop the Clachan server"
        $stopShortcut.Save()

        $uninstallShortcut = $shell.CreateShortcut((Join-Path $ShortcutDir "Uninstall Clachan.lnk"))
        $uninstallShortcut.TargetPath = "powershell.exe"
        $uninstallShortcut.Arguments = "-NoProfile -ExecutionPolicy Bypass -File `"$InstallScript`" -Uninstall"
        $uninstallShortcut.WorkingDirectory = $env:USERPROFILE
        $uninstallShortcut.IconLocation = "$Exe,0"
        $uninstallShortcut.Description = "Uninstall Clachan"
        $uninstallShortcut.Save()

        New-Item -Path $RegistryKey -Force | Out-Null
        Set-ItemProperty -Path $RegistryKey -Name DisplayName -Value "Clachan"
        Set-ItemProperty -Path $RegistryKey -Name DisplayVersion -Value $appVersion
        Set-ItemProperty -Path $RegistryKey -Name Publisher -Value "Clachan contributors"
        Set-ItemProperty -Path $RegistryKey -Name InstallLocation -Value $InstallDir
        Set-ItemProperty -Path $RegistryKey -Name DisplayIcon -Value "$Exe,0"
        Set-ItemProperty -Path $RegistryKey -Name UninstallString -Value ("powershell.exe -NoProfile -ExecutionPolicy Bypass -File `"$InstallScript`" -Uninstall")
        New-ItemProperty -Path $RegistryKey -Name NoModify -PropertyType DWord -Value 1 -Force | Out-Null
        New-ItemProperty -Path $RegistryKey -Name NoRepair -PropertyType DWord -Value 1 -Force | Out-Null
    }

    if ($OpenFirewall) {
        if (-not $isAdmin) {
            Write-Host "Warning: -OpenFirewall needs an elevated shell; skipping the firewall rule." -ForegroundColor Yellow
        }
        else {
            $firewallPort = Get-InstalledPort -Path $EnvFile
            $ruleName = "Clachan $firewallPort"
            if (-not (Get-NetFirewallRule -DisplayName $ruleName -ErrorAction SilentlyContinue)) {
                New-NetFirewallRule -DisplayName $ruleName -Direction Inbound -Action Allow -Protocol TCP -LocalPort $firewallPort -Profile Private | Out-Null
            }
        }
    }

    $portNumber = Get-InstalledPort -Path $EnvFile
    if (-not $NoStart) {
        Write-Step "Starting Clachan"
        if (-not $NoTask) {
            try {
                Start-ScheduledTask -TaskName $TaskName -ErrorAction Stop
            }
            catch {
                Start-ClachanHidden -ScriptPath $RunScript
            }
        }
        else {
            Start-ClachanHidden -ScriptPath $RunScript
        }

        if (-not (Wait-Health -PortNumber $portNumber)) {
            Write-Host "Clachan did not become healthy. Check:" -ForegroundColor Red
            Write-Host "  $(Join-Path $LogDir 'clachan-error.log')"
            Write-Host "  $(Join-Path $LogDir 'clachan.log')"
            Fail "health check failed"
        }
        Start-Process "http://127.0.0.1:$portNumber/"
    }

    $url = "http://127.0.0.1:$portNumber/"
    Write-Host ""
    Write-Host "Clachan $appVersion is installed." -ForegroundColor Green
    Write-Host "  URL:     $url"
    Write-Host "  Folder:  $InstallDir"
    Write-Host "  Config:  $EnvFile"
    Write-Host "  Data:    $DataDir"
    Write-Host "  Logs:    $LogDir"
    if (-not (Get-EnvValue -Path $EnvFile -Key "OPENROUTER_API_KEY")) {
        Write-Host ""
        Write-Host "Set OPENROUTER_API_KEY in $EnvFile, then restart Clachan from the Start Menu." -ForegroundColor Yellow
    }
    Write-Host ""
    Write-Host "Update: powershell -NoProfile -ExecutionPolicy Bypass -Command `"irm $LatestBase/install.ps1 | iex`""
    Write-Host "Or run the Uninstall Clachan shortcut in the Start Menu."
}
finally {
    Remove-Item -LiteralPath $tempDir -Recurse -Force -ErrorAction SilentlyContinue
}
