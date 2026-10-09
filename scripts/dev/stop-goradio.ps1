$ErrorActionPreference = "Stop"
$all = @(Get-CimInstance Win32_Process)
$targets = @($all | Where-Object { ($_.Name -ieq "goradio.exe") -and ($_.ExecutablePath -like "*\Goradio\*") })
Write-Output ("Goradio encontrados: " + $targets.Count)
foreach ($p in $targets) {
 Write-Output ("Deteniendo PID " + $p.ProcessId + " " + $p.CommandLine)
 $children = @($all | Where-Object { $_.ParentProcessId -eq $p.ProcessId -and $_.Name -in @("ssh.exe","cloudflared.exe","lt.exe","node.exe") })
 foreach ($c in $children) { Stop-Process -Id $c.ProcessId -Force -ErrorAction SilentlyContinue; Write-Output ("Túnel hijo cerrado " + $c.ProcessId) }
 Stop-Process -Id $p.ProcessId -Force -ErrorAction SilentlyContinue
}
$remaining = @(Get-CimInstance Win32_Process | Where-Object { $_.Name -ieq "goradio.exe" -and $_.ExecutablePath -like "*\Goradio\*" })
Write-Output ("Restantes Goradio: " + $remaining.Count)
