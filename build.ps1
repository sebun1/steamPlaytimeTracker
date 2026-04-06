$env:GOOS = "windows"
$env:GOARCH = "amd64"

go test -failfast -v -run "^TestSteamAPI" ./sptt/...
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

go build -o spt.exe .
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

go build -o createtoken.exe ./cmd/createtoken
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Write-Host "Done."
