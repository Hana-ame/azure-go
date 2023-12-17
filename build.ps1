$Env:GOOS = "linux"
$Env:GOARCH = "amd64"
$Env:CGO_ENABLED=0
go build -o azure.bin . 