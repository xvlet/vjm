# Usage : .\merge_jtl.ps1 -o result.jtl server1.jtl server2.jtl ...

param(
    [string]$o,
    [Parameter(ValueFromRemainingArguments=$true)]
    [string[]]$files
)

if (-not $o -or $files.Count -eq 0) {
    Write-Host "Usage: .\merge_jtl.ps1 -o <output.jtl> <input1.jtl> <input2.jtl> ..."
    exit 1
}

$outputFile = $o
$firstFile = $files[0]

if (-not (Test-Path $firstFile)) {
    Write-Host "Error: First file $firstFile not found."
    exit 1
}

Write-Host "Extracting header from $firstFile..."
# Extract the first line (header) and write to output file
Get-Content $firstFile -TotalCount 1 | Out-File -FilePath $outputFile -Encoding utf8

Write-Host "Merging and sorting data rows from all files..."

# Use a generic List for better performance with large files
$allLines = [System.Collections.Generic.List[string]]::new()

foreach ($file in $files) {
    if (-not (Test-Path $file)) {
        Write-Host "Warning: File $file not found. Skipping."
        continue
    }
   
    # ReadLines is memory efficient and fast
    $lines = [System.IO.File]::ReadLines((Resolve-Path $file).Path)
    $isFirst = $true
   
    foreach ($line in $lines) {
        if ($isFirst) {
            $isFirst = $false
            continue
        }
        if ([string]::IsNullOrWhiteSpace($line)) {
            continue
        }
        $allLines.Add($line)
    }
}

Write-Host "Sorting $($allLines.Count) rows by timestamp..."

# Sort numerically by the first column (timestamp)
$allLines.Sort({
    param($a, $b)
    $tsA = 0L
    $tsB = 0L
   
    $partsA = $a.Split(',')
    if ($partsA.Length -gt 0) { [long]::TryParse($partsA[0], [ref]$tsA) | Out-Null }
   
    $partsB = $b.Split(',')
    if ($partsB.Length -gt 0) { [long]::TryParse($partsB[0], [ref]$tsB) | Out-Null }
   
    return $tsA.CompareTo($tsB)
})

Write-Host "Writing merged data to $outputFile..."
# Append sorted data to the output file
$outPath = $outputFile
if (-not [System.IO.Path]::IsPathRooted($outPath)) {
    $outPath = Join-Path (Get-Location) $outputFile
}
[System.IO.File]::AppendAllLines($outPath, $allLines, [System.Text.Encoding]::UTF8)

Write-Host "Merge successfully completed: $outputFile"
