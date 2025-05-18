#!/bin/bash

# 读取 .env 文件中的内容并转换为哈希表
declare -A envVars
while IFS='=' read -r key value; do
    echo $key;
    echo $value;
    if [[ $key && $value ]]; then
        envVars["$key"]="$value"
    fi
done < .env

# 获取主机名
DST=${envVars['HOST']}
echo "$DST"

# 设置环境变量
export GOOS=linux
export GOARCH=amd64
export CGO_ENABLED=0

# 编译 Go 程序
go build -o azure .

# 移除 azure.bin 文件（如果存在）
rm -f azure.bin

# 复制生成的文件
cp azure azure.bin

# 使用 scp 上传文件
~/script/scp.sh azure.bin root@"$DST":~/azure/
# scp azure .env refresh_token root@"$DST":~/azure/

# 运行
# nohup ./azure &
# tail -f nohup.out