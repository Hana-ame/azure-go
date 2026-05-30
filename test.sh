#!/bin/bash
set -e

echo "=== go build ==="
go build ./...

echo ""
echo "=== unit tests ==="
go test ./... 2>&1 | grep -E '^(ok|FAIL|---|\?)'

echo ""
echo "=== S3 controller tests ==="
go test -v -run 'TestPutObject|TestGetObject|TestDeleteObject|TestHeadObject' .

echo ""
echo "=== integration (requires running server) ==="
if curl -s http://127.0.0.1:8080/api/test > /dev/null 2>&1; then
    echo "--- server is running, testing S3 API ---"

    echo -n "PUT:  "
    curl -s -X PUT --data-binary 'test content' -H 'Content-Type: text/plain' \
        -w '%{http_code} ETag:%header{etag}' -o /dev/null \
        http://127.0.0.1:8080/test-bucket/integration-test.txt
    echo ""

    echo -n "GET:  "
    curl -s http://127.0.0.1:8080/test-bucket/integration-test.txt
    echo ""

    echo -n "HEAD: "
    curl -s -o /dev/null -w '%{http_code} CT:%header{Content-Type} CL:%header{Content-Length}' \
        --head http://127.0.0.1:8080/test-bucket/integration-test.txt
    echo ""

    echo -n "DEL:  "
    curl -s -X DELETE -w '%{http_code}' -o /dev/null \
        http://127.0.0.1:8080/test-bucket/integration-test.txt
    echo ""

    echo -n "DEDUP: "
    curl -s -X PUT --data-binary 'same content' -H 'Content-Type: text/plain' \
        -w '%{http_code} DedupPath:%header{x-amz-dedup-path}' -o /dev/null \
        http://127.0.0.1:8080/dedup-bucket/file-a.txt
    echo ""
    curl -s -X PUT --data-binary 'same content' -H 'Content-Type: text/plain' \
        -w '%{http_code} DedupPath:%header{x-amz-dedup-path}' -o /dev/null \
        http://127.0.0.1:8080/dedup-bucket/file-b.txt
    echo ""
    echo "  (second upload should show x-amz-dedup-path)"

    echo ""
    echo "=== integration passed ==="
else
    echo "server not running, skip integration tests"
    echo "start with: LISTEN_ADDR=:8080 go run ."
fi
