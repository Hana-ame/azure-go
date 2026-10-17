#!/bin/bash
set -e

DST="${1:-vps.moonchan.xyz}"

echo "=== 1. build ==="
export GOOS=linux GOARCH=amd64 CGO_ENABLED=0
go build -o azure .
ls -la azure

echo "=== 2. scp ==="
source ~/script/env.source 2>/dev/null
~/script/scp.sh azure root@"$DST":~/azure/temp/

echo "=== 3. backup + replace + restart ==="
ssh root@"$DST" << 'REMOTE'
cd ~/azure
TS=$(date +%Y%m%d-%H%M%S)
cp -f azure.bin "azure.bin.bak-$TS"
cp -f temp/azure azure.bin
chmod +x azure.bin
echo "  replaced azure.bin (backup: azure.bin.bak-$TS)"
pkill -f './azure.bin' 2>/dev/null || true
sleep 1
nohup ./azure.bin > nohup.out 2>&1 &
sleep 2
echo "=== 4. status ==="
ps aux | grep '[a]zure.bin'
echo "=== log ==="
tail -3 nohup.out
REMOTE

echo "=== done ==="
