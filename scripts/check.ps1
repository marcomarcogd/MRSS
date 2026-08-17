# scripts/check.ps1 - Run all code quality checks

Write-Host "🔍 Running code quality checks..." -ForegroundColor Green

# Backend checks
Write-Host "📦 Checking Go code..." -ForegroundColor Blue
go vet ./...
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
Write-Host "✅ Go vet passed" -ForegroundColor Green

Write-Host "🎨 Checking Go formatting..." -ForegroundColor Blue
$gofmtOutput = gofmt -d .
if ($gofmtOutput) {
    Write-Host "❌ Go code is not formatted. Run 'make format-backend' to fix." -ForegroundColor Red
    Write-Host $gofmtOutput
    exit 1
}
Write-Host "✅ Go formatting OK" -ForegroundColor Green

# Frontend checks
Write-Host "🎨 Checking frontend code..." -ForegroundColor Blue
Push-Location frontend
npm run lint
if ($LASTEXITCODE -ne 0) { Pop-Location; exit $LASTEXITCODE }

npm run check:windows-icons
if ($LASTEXITCODE -ne 0) { Pop-Location; exit $LASTEXITCODE }

npm test -- --run --reporter=verbose
if ($LASTEXITCODE -ne 0) { Pop-Location; exit $LASTEXITCODE }
Pop-Location
Write-Host "✅ Frontend checks passed" -ForegroundColor Green

# Build check
Write-Host "🔨 Checking build..." -ForegroundColor Blue
go build -v ./...
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
Write-Host "✅ Build successful" -ForegroundColor Green

Write-Host "🎉 All checks passed!" -ForegroundColor Green
