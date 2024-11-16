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
Remove-Item azure.bin
Copy-Item azure azure.bin

# 使用go live上传文件
# cd ~/azure && ~/download.sh azure.bin azure


# 使用 scp 上传文件
scp azure root@${DST}:~/azure/
# scp azure .env refresh_token root@${DST}:~/azure/

# 运行
# nohup ./azure &
# tail -f nohup.out