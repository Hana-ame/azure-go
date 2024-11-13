# 读取 .env 文件中的内容并转换为哈希表
$envVars = @{}
Get-Content .env | ForEach-Object {
    if ($_ -match '^(?<key>[^=]+)=(?<value>.+)$') {
        $envVars[$matches['key']] = $matches['value']
    }
}

# 获取主机名
$DST = $envVars['HOST']
Write-Output $DST

$Env:GOOS = "linux"
$Env:GOARCH = "amd64"
$Env:CGO_ENABLED=0
go build -o azure . 

# 使用 scp 上传文件
scp azure .env refresh_token root@${DST}:~/azure/
# nohup ./azure &
# tail -f nohup.out