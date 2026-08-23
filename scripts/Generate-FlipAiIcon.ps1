param(
    [Parameter(Mandatory = $true)]
    [string]$OutputPath
)

$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing
Add-Type @'
using System;
using System.Runtime.InteropServices;
public static class FlipAiNativeIcon {
    [DllImport("user32.dll", SetLastError=true)]
    public static extern bool DestroyIcon(IntPtr hIcon);
}
'@

$size = 64
$bitmap = New-Object System.Drawing.Bitmap($size, $size, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
$graphics = [System.Drawing.Graphics]::FromImage($bitmap)
$graphics.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
$graphics.TextRenderingHint = [System.Drawing.Text.TextRenderingHint]::AntiAliasGridFit
$graphics.Clear([System.Drawing.Color]::Transparent)

$violet = [System.Drawing.Color]::FromArgb(108, 71, 255)
$violetBrush = New-Object System.Drawing.SolidBrush($violet)
$graphics.FillEllipse($violetBrush, 2, 2, 60, 60)

# A tiny speech/bridge accent keeps the icon recognizable even at tray size.
$accentBrush = New-Object System.Drawing.SolidBrush([System.Drawing.Color]::FromArgb(220, 211, 255))
$graphics.FillEllipse($accentBrush, 11, 45, 7, 7)
$graphics.FillEllipse($accentBrush, 20, 49, 5, 5)

$font = New-Object System.Drawing.Font('Segoe UI', 31, [System.Drawing.FontStyle]::Bold, [System.Drawing.GraphicsUnit]::Pixel)
$format = New-Object System.Drawing.StringFormat
$format.Alignment = [System.Drawing.StringAlignment]::Center
$format.LineAlignment = [System.Drawing.StringAlignment]::Center
$rect = New-Object System.Drawing.RectangleF(0, -1, $size, $size)
$graphics.DrawString('F', $font, [System.Drawing.Brushes]::White, $rect, $format)

$dir = Split-Path -Parent $OutputPath
if ($dir) { New-Item -ItemType Directory -Force $dir | Out-Null }
$hIcon = $bitmap.GetHicon()
try {
    $icon = [System.Drawing.Icon]::FromHandle($hIcon)
    $stream = [System.IO.File]::Open($OutputPath, [System.IO.FileMode]::Create)
    try { $icon.Save($stream) } finally { $stream.Dispose(); $icon.Dispose() }
} finally {
    [FlipAiNativeIcon]::DestroyIcon($hIcon) | Out-Null
    $format.Dispose()
    $font.Dispose()
    $accentBrush.Dispose()
    $violetBrush.Dispose()
    $graphics.Dispose()
    $bitmap.Dispose()
}

if (-not (Test-Path $OutputPath) -or (Get-Item $OutputPath).Length -lt 100) {
    throw "FlipAi icon generation failed: $OutputPath"
}
Write-Host "Generated $OutputPath"
