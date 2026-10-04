#!/usr/bin/env bash
set -euo pipefail

: "${TEST_LITESTREAM_BINARY:?Set TEST_LITESTREAM_BINARY to Litestream 0.5.17}"
name="woovi-litestream-test-${RANDOM}-$$"
cleanup() { docker rm -f "$name" >/dev/null 2>&1 || true; }
trap cleanup EXIT
docker run -d --name "$name" -p 127.0.0.1::9000 \
  chrislusf/seaweedfs:4.48@sha256:4e61d15fd35994cb1e43e1e553dff106794841fd9a99ade2fc8c8bfce4d7872d \
  server -s3 -s3.port=9000 >/dev/null
port="$(docker port "$name" 9000 | head -n 1)"
export TEST_S3_ENDPOINT="http://${port}"
export TEST_S3_ACCESS_KEY=woovi-test
export TEST_S3_SECRET_KEY=woovi-test-only
# SeaweedFS in this disposable loopback-only test has no authentication.
# A bucket PUT is sufficient; production S3 credentials use the hidden CLI.
ready=false
for _ in $(seq 1 60); do
  if curl --silent --fail -X PUT "${TEST_S3_ENDPOINT}/woovi-test" >/dev/null; then
    ready=true
    break
  fi
  sleep 1
done
if [[ "$ready" != true ]]; then
  echo "S3-compatible test storage did not become ready" >&2
  exit 1
fi
go test -count=2 ./...
go test -race -count=1 ./...
