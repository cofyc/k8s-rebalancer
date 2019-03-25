#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

ROOT=$(unset CDPATH && cd $(dirname "${BASH_SOURCE[0]}")/.. && pwd)
cd $ROOT

hack/verify-gofmt.sh
hack/verify-govet.sh
hack/verify-golint.sh
hack/verify-deps.sh
